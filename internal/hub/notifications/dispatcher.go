package notifications

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Gu1llaum-3/vigil/internal/hub/notifications/providers"
	"github.com/pocketbase/pocketbase/core"
)

const (
	bufferSize  = 1024
	workerCount = 4
	maxRetries  = 3

	// A channel whose last breakerThreshold deliveries failed is skipped for
	// breakerCooldown, then one delivery probes it while the others are still skipped: a
	// dead channel does not hold the workers through every retry of every event.
	breakerThreshold = 3
	breakerCooldown  = time.Minute

	// dropReportInterval is how often dropped events are reported in the delivery history.
	dropReportInterval = time.Minute
)

// sendTimeout bounds a delivery with all its retries.
const sendTimeout = 30 * time.Second

var defaultRetryDelays = []time.Duration{time.Second, 4 * time.Second, 16 * time.Second}

// Dispatcher processes notification events and routes them to configured channels.
type Dispatcher struct {
	app           core.App
	events        chan Event
	throttleCache map[string]time.Time
	mu            sync.Mutex
	providerMap   map[string]providers.Provider
	retryDelays   []time.Duration // before retries 1..maxRetries
	sendTimeout   time.Duration   // a delivery with all its retries
	now           func() time.Time
	breakers      map[string]*channelBreaker // by channel id, under mu
	dropped       atomic.Int64               // events dropped since the last report
}

// channelBreaker counts a channel's consecutive failed deliveries.
type channelBreaker struct {
	failures   int
	since      time.Time // first failure of the streak
	openUntil  time.Time // zero while closed
	skipLogged bool      // the current opening's first skip was logged
}

// New creates a Dispatcher and registers the email and webhook providers.
func New(app core.App) *Dispatcher {
	d := &Dispatcher{
		app:           app,
		events:        make(chan Event, bufferSize),
		throttleCache: make(map[string]time.Time),
		retryDelays:   defaultRetryDelays,
		sendTimeout:   sendTimeout,
		now:           time.Now,
		breakers:      make(map[string]*channelBreaker),
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
	wg.Add(1)
	go func() {
		defer wg.Done()
		ticker := time.NewTicker(dropReportInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				d.reportDropped()
			}
		}
	}()
	wg.Wait()
}

// Dispatch enqueues an event for processing. Non-blocking: drops if buffer is full, and
// counts it for reportDropped.
func (d *Dispatcher) Dispatch(evt Event) {
	select {
	case d.events <- evt:
	default:
		d.dropped.Add(1)
		slog.Warn("notifications: buffer full, dropping event", "kind", evt.Kind, "resource", evt.Resource.ID)
	}
}

// reportDropped writes the events dropped since the last report to the delivery history,
// where admins look for missing notifications.
func (d *Dispatcher) reportDropped() {
	n := d.dropped.Swap(0)
	if n == 0 {
		return
	}
	msg := fmt.Sprintf("notification queue full: %d events dropped (channels too slow or failing)", n)
	slog.Error("notifications: " + msg)
	d.saveLog("", "", "", "", "dispatch", "", "", "", "failed", msg, "")
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
		Severity:     evt.EffectiveSeverity(),
		EventKind:    string(evt.Kind),
		ResourceID:   evt.Resource.ID,
		ResourceName: evt.Resource.Name,
		ResourceType: evt.Resource.Type,
		Previous:     evt.Previous,
		Current:      evt.Current,
		Timestamp:    evt.OccurredAt,
	}

	// Rules, and the channels of each, are served concurrently: a slow or failing channel
	// does not delay the others.
	var wg sync.WaitGroup
	for _, rule := range rules {
		d.goDeliver(&wg, func() { d.processRule(ctx, rule, evt, msg) })
	}
	wg.Wait()
}

// goDeliver runs fn in a goroutine counted by wg; a panic in a provider is logged instead
// of crashing the hub.
func (d *Dispatcher) goDeliver(wg *sync.WaitGroup, fn func()) {
	wg.Add(1)
	go func() {
		defer wg.Done()
		defer func() {
			if r := recover(); r != nil {
				slog.Error("notifications: delivery panicked", "panic", r)
			}
		}()
		fn()
	}()
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
		if severityRank(evt.EffectiveSeverity()) < severityRank(minSev) {
			return
		}
	}

	// Check throttle
	throttleSec := rule.GetInt("throttle_seconds")
	if d.isThrottled(rule.Id, evt.Resource.ID, throttleKind(evt), throttleSec) {
		d.saveLog(rule.Id, rule.GetString("created_by"), "", "", string(evt.Kind), evt.Resource.ID, evt.Resource.Name, evt.Resource.Type, "throttled", "", "")
		return
	}

	var wg sync.WaitGroup
	for _, chID := range rule.GetStringSlice("channels") {
		d.goDeliver(&wg, func() { d.sendToChannel(ctx, chID, rule.Id, rule.GetString("created_by"), msg, evt) })
	}
	wg.Wait()
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

	if skip, logIt, reason := d.breakerCheck(channelID); skip {
		// Logged once per opening: a log per skipped event would flood the history and the
		// failure toasts during an outage.
		if logIt {
			d.saveLog(ruleID, createdBy, channelID, kind, string(evt.Kind), evt.Resource.ID, evt.Resource.Name, evt.Resource.Type, "failed", reason, "")
		}
		return
	}

	var config map[string]any
	_ = chRec.UnmarshalJSONField("config", &config)

	ch := providers.Channel{ID: channelID, Kind: kind, Config: config}

	sendCtx, cancel := context.WithTimeout(ctx, d.sendTimeout)
	defer cancel()

	var lastErr error
	var preview string
	for attempt := 0; attempt <= maxRetries; attempt++ {
		if attempt > 0 {
			select {
			case <-sendCtx.Done():
				if ctx.Err() != nil {
					return // the hub is stopping: not the channel's failure
				}
				d.recordDelivery(channelID, false)
				errMsg := sendCtx.Err().Error()
				if lastErr != nil && !errors.Is(lastErr, sendCtx.Err()) {
					errMsg += " (last error: " + lastErr.Error() + ")"
				}
				d.saveLog(ruleID, createdBy, channelID, kind, string(evt.Kind), evt.Resource.ID, evt.Resource.Name, evt.Resource.Type, "failed", errMsg, preview)
				return
			case <-time.After(d.retryDelays[attempt-1]):
			}
		}
		preview, lastErr = provider.Send(sendCtx, ch, msg)
		if lastErr == nil {
			d.recordDelivery(channelID, true)
			d.saveLog(ruleID, createdBy, channelID, kind, string(evt.Kind), evt.Resource.ID, evt.Resource.Name, evt.Resource.Type, "sent", "", preview)
			return
		}
		slog.Warn("notifications: send attempt failed", "attempt", attempt+1, "channel", channelID, "err", lastErr)
	}

	if ctx.Err() != nil {
		return // the hub is stopping: not the channel's failure
	}
	d.recordDelivery(channelID, false)
	d.saveLog(ruleID, createdBy, channelID, kind, string(evt.Kind), evt.Resource.ID, evt.Resource.Name, evt.Resource.Type, "failed", lastErr.Error(), preview)
}

// breakerCheck reports whether a delivery to channelID is skipped, whether to log this skip
// (the first of the opening) and why. When the cooldown ends, this delivery probes the
// channel and the others stay skipped until its outcome is recorded.
func (d *Dispatcher) breakerCheck(channelID string) (skip, logIt bool, reason string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	b := d.breakers[channelID]
	if b == nil || b.openUntil.IsZero() {
		return false, false, ""
	}
	now := d.now()
	if !now.Before(b.openUntil) {
		b.openUntil = now.Add(breakerCooldown) // until the probe's outcome
		return false, false, ""
	}
	logIt = !b.skipLogged
	b.skipLogged = true
	return true, logIt, fmt.Sprintf("skipped: the channel's last %d deliveries since %s failed; its deliveries are skipped (without a log) until %s",
		b.failures, b.since.UTC().Format(time.RFC3339), b.openUntil.UTC().Format(time.RFC3339))
}

// recordDelivery updates channelID's breaker with the outcome of a delivery.
func (d *Dispatcher) recordDelivery(channelID string, ok bool) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if ok {
		delete(d.breakers, channelID)
		return
	}
	b := d.breakers[channelID]
	if b == nil {
		b = &channelBreaker{since: d.now()}
		d.breakers[channelID] = b
	}
	b.failures++
	if b.failures >= breakerThreshold {
		b.openUntil = d.now().Add(breakerCooldown)
		b.skipLogged = false
	}
}

// ResetChannel forgets channelID's failures, after it was edited or tested successfully.
func (d *Dispatcher) ResetChannel(channelID string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	delete(d.breakers, channelID)
}

// throttleKind is the throttle discriminator for an event. Throttling is per
// (rule, resource, kind); host-metric events additionally carry the metric and the
// breach tier so that (a) a CPU breach does not suppress a concurrent disk/memory/load
// breach on the same agent, and (b) a warning→critical escalation of the same metric is
// not throttled away by the earlier warning notification (different tier → different key).
func throttleKind(evt Event) string {
	k := string(evt.Kind)
	if m, ok := evt.Details["metric"].(string); ok && m != "" {
		k += ":" + m
		if tier, ok := evt.Details["tier"].(string); ok && tier != "" {
			k += ":" + tier
		}
	}
	return k
}

func (d *Dispatcher) isThrottled(ruleID, resourceID, kind string, throttleSec int) bool {
	if throttleSec <= 0 {
		return false
	}
	key := ruleID + "|" + resourceID + "|" + kind
	d.mu.Lock()
	defer d.mu.Unlock()
	now := d.now()
	if t, ok := d.throttleCache[key]; ok && now.Sub(t) < time.Duration(throttleSec)*time.Second {
		return true
	}
	d.throttleCache[key] = now
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

// matchesFilter applies a rule's resource filter, with the semantics of a maintenance
// window's scope: no ids at all = every resource; otherwise only the listed monitors, the
// listed hosts and the container images of those hosts. A resource type the filter cannot
// name is not covered.
func matchesFilter(filter map[string][]string, evt Event) bool {
	monitors, agents := filter["monitor_ids"], filter["agent_ids"]
	if len(monitors) == 0 && len(agents) == 0 {
		return true
	}
	switch evt.Resource.Type {
	case "monitor":
		return containsString(monitors, evt.Resource.ID)
	case "agent":
		return containsString(agents, evt.Resource.ID)
	case "container_image":
		return containsString(agents, ContainerHost(evt))
	}
	return false
}

// ContainerHost returns the host of a container_image event: Details["agent_id"], else the
// "<agent>|<container>" head of its resource id.
func ContainerHost(evt Event) string {
	if id, _ := evt.Details["agent_id"].(string); id != "" {
		return id
	}
	id, _, _ := strings.Cut(evt.Resource.ID, "|")
	return id
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
