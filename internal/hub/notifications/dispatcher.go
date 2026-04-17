package notifications

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/Gu1llaum-3/vigil/internal/hub/notifications/providers"
	"github.com/pocketbase/pocketbase/core"
)

const (
	bufferSize  = 256
	workerCount = 2
	maxRetries  = 3
)

var retryDelays = []time.Duration{time.Second, 4 * time.Second, 16 * time.Second}

// Dispatcher processes notification events and routes them to configured channels.
type Dispatcher struct {
	app           core.App
	events        chan Event
	throttleCache map[string]time.Time
	mu            sync.Mutex
	providerMap   map[string]providers.Provider
}

// New creates a Dispatcher and registers the email and webhook providers.
func New(app core.App) *Dispatcher {
	d := &Dispatcher{
		app:           app,
		events:        make(chan Event, bufferSize),
		throttleCache: make(map[string]time.Time),
	}
	d.providerMap = map[string]providers.Provider{
		"email":   &providers.EmailProvider{App: app},
		"webhook": providers.NewWebhookProvider(),
		"slack":   providers.NewSlackProvider(),
		"teams":   providers.NewTeamsProvider(),
		"gchat":   providers.NewGChatProvider(),
		"ntfy":    providers.NewNtfyProvider(),
		"gotify":  providers.NewGotifyProvider(),
		"in-app":  providers.NewInAppProvider(),
	}
	for _, p := range d.providerMap {
		providers.Register(p)
	}
	return d
}

// Start launches the worker goroutines and blocks until ctx is canceled.
func (d *Dispatcher) Start(ctx context.Context) {
	var wg sync.WaitGroup
	for i := 0; i < workerCount; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			d.worker(ctx)
		}()
	}
	wg.Wait()
}

// Dispatch enqueues an event for processing. Non-blocking: drops if buffer is full.
func (d *Dispatcher) Dispatch(evt Event) {
	select {
	case d.events <- evt:
	default:
		slog.Warn("notifications: buffer full, dropping event", "kind", evt.Kind, "resource", evt.Resource.ID)
	}
}

func (d *Dispatcher) worker(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case evt := <-d.events:
			d.process(ctx, evt)
		}
	}
}

func (d *Dispatcher) process(ctx context.Context, evt Event) {
	rules, err := d.app.FindRecordsByFilter("notification_rules", "enabled = true", "", 0, 0)
	if err != nil {
		slog.Warn("notifications: failed to load rules", "err", err)
		return
	}

	title, body, err := RenderMessage(evt)
	if err != nil {
		slog.Warn("notifications: failed to render message", "kind", evt.Kind, "err", err)
		title = string(evt.Kind)
		body = string(evt.Kind)
	}

	msg := providers.Message{
		Title:        title,
		Body:         body,
		Severity:     evt.Kind.Severity(),
		EventKind:    string(evt.Kind),
		ResourceID:   evt.Resource.ID,
		ResourceName: evt.Resource.Name,
		ResourceType: evt.Resource.Type,
		Previous:     evt.Previous,
		Current:      evt.Current,
		Timestamp:    evt.OccurredAt,
	}

	for _, rule := range rules {
		d.processRule(ctx, rule, evt, msg)
	}
}

func (d *Dispatcher) processRule(ctx context.Context, rule *core.Record, evt Event, msg providers.Message) {
	// Check event kind
	var ruleEvents []string
	if err := rule.UnmarshalJSONField("events", &ruleEvents); err != nil || len(ruleEvents) == 0 {
		return
	}
	if !containsString(ruleEvents, string(evt.Kind)) {
		return
	}

	// Check resource filter
	var filter map[string][]string
	_ = rule.UnmarshalJSONField("filter", &filter)
	if !matchesFilter(filter, evt) {
		return
	}

	// Check min_severity
	if minSev := rule.GetString("min_severity"); minSev != "" {
		if severityRank(evt.Kind.Severity()) < severityRank(minSev) {
			return
		}
	}

	// Check throttle
	throttleSec := rule.GetInt("throttle_seconds")
	if d.isThrottled(rule.Id, evt.Resource.ID, string(evt.Kind), throttleSec) {
		d.saveLog(rule.Id, rule.GetString("created_by"), "", "", string(evt.Kind), evt.Resource.ID, evt.Resource.Name, evt.Resource.Type, "throttled", "", "")
		return
	}

	// Load and send to each channel
	channelIDs := rule.GetStringSlice("channels")
	for _, chID := range channelIDs {
		d.sendToChannel(ctx, chID, rule.Id, rule.GetString("created_by"), msg, evt)
	}
}

func (d *Dispatcher) sendToChannel(ctx context.Context, channelID, ruleID, createdBy string, msg providers.Message, evt Event) {
	chRec, err := d.app.FindRecordById("notification_channels", channelID)
	if err != nil {
		slog.Warn("notifications: channel not found", "channel", channelID, "err", err)
		return
	}
	if !chRec.GetBool("enabled") {
		return
	}

	kind := chRec.GetString("kind")
	provider, ok := d.providerMap[kind]
	if !ok {
		slog.Warn("notifications: unregistered provider kind", "kind", kind, "channel", channelID)
		return
	}

	var config map[string]any
	_ = chRec.UnmarshalJSONField("config", &config)

	ch := providers.Channel{ID: channelID, Kind: kind, Config: config}

	sendCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	var lastErr error
	var preview string
	for attempt := 0; attempt <= maxRetries; attempt++ {
		if attempt > 0 {
			select {
			case <-sendCtx.Done():
				d.saveLog(ruleID, createdBy, channelID, kind, string(evt.Kind), evt.Resource.ID, evt.Resource.Name, evt.Resource.Type, "failed", "context canceled", preview)
				return
			case <-time.After(retryDelays[attempt-1]):
			}
		}
		preview, lastErr = provider.Send(sendCtx, ch, msg)
		if lastErr == nil {
			d.saveLog(ruleID, createdBy, channelID, kind, string(evt.Kind), evt.Resource.ID, evt.Resource.Name, evt.Resource.Type, "sent", "", preview)
			return
		}
		slog.Warn("notifications: send attempt failed", "attempt", attempt+1, "channel", channelID, "err", lastErr)
	}

	d.saveLog(ruleID, createdBy, channelID, kind, string(evt.Kind), evt.Resource.ID, evt.Resource.Name, evt.Resource.Type, "failed", lastErr.Error(), preview)
}

func (d *Dispatcher) isThrottled(ruleID, resourceID, kind string, throttleSec int) bool {
	if throttleSec <= 0 {
		return false
	}
	key := ruleID + "|" + resourceID + "|" + kind
	d.mu.Lock()
	defer d.mu.Unlock()
	if t, ok := d.throttleCache[key]; ok && time.Since(t) < time.Duration(throttleSec)*time.Second {
		return true
	}
	d.throttleCache[key] = time.Now()
	return false
}

func (d *Dispatcher) saveLog(ruleID, createdBy, channelID, channelKind, eventKind, resourceID, resourceName, resourceType, status, errMsg, payloadPreview string) {
	col, err := d.app.FindCachedCollectionByNameOrId("notification_logs")
	if err != nil {
		slog.Warn("notifications: notification_logs collection not found", "err", err)
		return
	}
	rec := core.NewRecord(col)
	if ruleID != "" {
		rec.Set("rule", ruleID)
	}
	if channelID != "" {
		rec.Set("channel", channelID)
	}
	if createdBy != "" {
		rec.Set("created_by", createdBy)
	}
	if channelKind != "" {
		rec.Set("channel_kind", channelKind)
	}
	rec.Set("event_kind", eventKind)
	rec.Set("resource_id", resourceID)
	rec.Set("resource_name", resourceName)
	rec.Set("resource_type", resourceType)
	rec.Set("status", status)
	rec.Set("sent_at", time.Now())
	if errMsg != "" {
		rec.Set("error", errMsg)
	}
	rec.Set("payload_preview", payloadPreview)
	if err := d.app.SaveNoValidate(rec); err != nil {
		slog.Warn("notifications: failed to save log", "err", err)
	}
}

// RedactConfig returns a copy of the config with sensitive keys replaced by "**REDACTED**".
func RedactConfig(kind string, config map[string]any) map[string]any {
	if config == nil {
		return nil
	}
	provider, ok := providers.Get(kind)
	if !ok {
		return config
	}
	sensitiveKeys := provider.SensitiveConfigKeys()
	if len(sensitiveKeys) == 0 {
		return config
	}
	sensitive := make(map[string]bool, len(sensitiveKeys))
	for _, k := range sensitiveKeys {
		sensitive[k] = true
	}
	redacted := make(map[string]any, len(config))
	for k, v := range config {
		if sensitive[k] && v != nil && v != "" {
			redacted[k] = "**REDACTED**"
		} else {
			redacted[k] = v
		}
	}
	return redacted
}

// Helper functions

func containsString(slice []string, s string) bool {
	for _, v := range slice {
		if v == s {
			return true
		}
	}
	return false
}

func matchesFilter(filter map[string][]string, evt Event) bool {
	if len(filter) == 0 {
		return true
	}
	var ids []string
	switch evt.Resource.Type {
	case "monitor":
		ids = filter["monitor_ids"]
	case "agent":
		ids = filter["agent_ids"]
	}
	if len(ids) == 0 {
		return true
	}
	return containsString(ids, evt.Resource.ID)
}

func severityRank(s string) int {
	switch s {
	case "warning":
		return 1
	case "critical":
		return 2
	default:
		return 0
	}
}
