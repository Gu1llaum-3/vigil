package hub

import (
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/Gu1llaum-3/vigil/internal/hub/notifications"
	"github.com/Gu1llaum-3/vigil/internal/hub/notifications/providers"
	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// --- Request / response types ---

type notificationChannelInput struct {
	Name    string         `json:"name"`
	Kind    string         `json:"kind"`
	Enabled *bool          `json:"enabled"`
	Config  map[string]any `json:"config"`
}

type notificationChannelResponse struct {
	ID      string         `json:"id"`
	Name    string         `json:"name"`
	Kind    string         `json:"kind"`
	Enabled bool           `json:"enabled"`
	Config  map[string]any `json:"config"`
	Created string         `json:"created"`
	Updated string         `json:"updated"`
}

type notificationRuleInput struct {
	Name            string         `json:"name"`
	Enabled         *bool          `json:"enabled"`
	Events          []string       `json:"events"`
	Filter          map[string]any `json:"filter"`
	Channels        []string       `json:"channels"`
	MinSeverity     string         `json:"min_severity"`
	ThrottleSeconds int            `json:"throttle_seconds"`
}

type notificationRuleResponse struct {
	ID              string         `json:"id"`
	Name            string         `json:"name"`
	Enabled         bool           `json:"enabled"`
	Events          []string       `json:"events"`
	Filter          map[string]any `json:"filter"`
	Channels        []string       `json:"channels"`
	MinSeverity     string         `json:"min_severity"`
	ThrottleSeconds int            `json:"throttle_seconds"`
	Created         string         `json:"created"`
	Updated         string         `json:"updated"`
}

type notificationLogResponse struct {
	ID             string `json:"id"`
	Rule           string `json:"rule"`
	Channel        string `json:"channel"`
	ChannelKind    string `json:"channel_kind,omitempty"`
	CreatedBy      string `json:"created_by,omitempty"`
	EventKind      string `json:"event_kind"`
	ResourceID     string `json:"resource_id"`
	ResourceName   string `json:"resource_name,omitempty"`
	ResourceType   string `json:"resource_type"`
	Status         string `json:"status"`
	Error          string `json:"error,omitempty"`
	PayloadPreview string `json:"payload_preview,omitempty"`
	ReadAt         string `json:"read_at,omitempty"`
	SentAt         string `json:"sent_at"`
}

type notificationLogsPageResponse struct {
	Items   []notificationLogResponse `json:"items"`
	Page    int                       `json:"page"`
	Limit   int                       `json:"limit"`
	HasMore bool                      `json:"has_more"`
}

type notificationUnreadResponse struct {
	Count int                       `json:"count"`
	Items []notificationLogResponse `json:"items"`
}

var notificationCenterEventKinds = []string{
	"monitor.down",
	"monitor.up",
	"agent.offline",
	"agent.online",
	"container_image.update_available",
}

// --- Channel handlers ---

func (h *Hub) getNotificationChannels(e *core.RequestEvent) error {
	records, err := h.FindRecordsByFilter("notification_channels", "", "name", 0, 0)
	if err != nil {
		return err
	}
	result := make([]notificationChannelResponse, 0, len(records))
	for _, rec := range records {
		result = append(result, channelRecordToResponse(rec))
	}
	return e.JSON(http.StatusOK, result)
}

func (h *Hub) createNotificationChannel(e *core.RequestEvent) error {
	var input notificationChannelInput
	if err := e.BindBody(&input); err != nil {
		return e.BadRequestError("Invalid request body", err)
	}
	if err := validateChannelInput(&input); err != nil {
		return e.BadRequestError(err.Error(), nil)
	}

	col, err := h.FindCachedCollectionByNameOrId("notification_channels")
	if err != nil {
		return err
	}
	rec := core.NewRecord(col)
	rec.Set("name", input.Name)
	rec.Set("kind", input.Kind)
	enabled := input.Enabled == nil || *input.Enabled
	rec.Set("enabled", enabled)
	if input.Config != nil {
		rec.Set("config", input.Config)
	}
	rec.Set("created_by", e.Auth.Id)

	if err := h.Save(rec); err != nil {
		return err
	}
	return e.JSON(http.StatusCreated, channelRecordToResponse(rec))
}

func (h *Hub) updateNotificationChannel(e *core.RequestEvent) error {
	id := e.Request.PathValue("id")
	rec, err := h.FindRecordById("notification_channels", id)
	if err != nil {
		return e.NotFoundError("Channel not found", nil)
	}

	var input notificationChannelInput
	if err := e.BindBody(&input); err != nil {
		return e.BadRequestError("Invalid request body", err)
	}

	if input.Name != "" {
		rec.Set("name", input.Name)
	}
	if input.Enabled != nil {
		rec.Set("enabled", *input.Enabled)
	}
	if input.Config != nil {
		merged := mergeChannelConfig(rec.GetString("kind"), rec, input.Config)
		rec.Set("config", merged)
	}

	if err := h.Save(rec); err != nil {
		return err
	}
	return e.JSON(http.StatusOK, channelRecordToResponse(rec))
}

func (h *Hub) deleteNotificationChannel(e *core.RequestEvent) error {
	id := e.Request.PathValue("id")
	rec, err := h.FindRecordById("notification_channels", id)
	if err != nil {
		return e.NotFoundError("Channel not found", nil)
	}
	if err := h.Delete(rec); err != nil {
		return err
	}
	return e.JSON(http.StatusOK, map[string]bool{"ok": true})
}

func (h *Hub) testNotificationChannel(e *core.RequestEvent) error {
	id := e.Request.PathValue("id")
	rec, err := h.FindRecordById("notification_channels", id)
	if err != nil {
		return e.NotFoundError("Channel not found", nil)
	}

	kind := rec.GetString("kind")
	provider, ok := providers.Get(kind)
	if !ok {
		return e.BadRequestError("Unknown provider kind: "+kind, nil)
	}

	var config map[string]any
	_ = rec.UnmarshalJSONField("config", &config)

	ch := providers.Channel{ID: rec.Id, Kind: kind, Config: config}
	msg := providers.Message{
		Title:        "Test notification from Vigil",
		Body:         "This is a test notification. If you receive it, your channel is configured correctly.",
		Severity:     "info",
		EventKind:    "test",
		ResourceName: "Vigil",
	}

	preview, sendErr := provider.Send(e.Request.Context(), ch, msg)
	if sendErr != nil {
		return e.JSON(http.StatusOK, map[string]any{"ok": false, "error": sendErr.Error()})
	}
	return e.JSON(http.StatusOK, map[string]any{"ok": true, "preview": preview})
}

// --- Rule handlers ---

func (h *Hub) getNotificationRules(e *core.RequestEvent) error {
	records, err := h.FindRecordsByFilter("notification_rules", "", "name", 0, 0)
	if err != nil {
		return err
	}
	result := make([]notificationRuleResponse, 0, len(records))
	for _, rec := range records {
		result = append(result, ruleRecordToResponse(rec))
	}
	return e.JSON(http.StatusOK, result)
}

func (h *Hub) createNotificationRule(e *core.RequestEvent) error {
	var input notificationRuleInput
	if err := e.BindBody(&input); err != nil {
		return e.BadRequestError("Invalid request body", err)
	}
	if input.Name == "" {
		return e.BadRequestError("name is required", nil)
	}

	col, err := h.FindCachedCollectionByNameOrId("notification_rules")
	if err != nil {
		return err
	}
	rec := core.NewRecord(col)
	applyRuleFields(rec, &input)
	rec.Set("created_by", e.Auth.Id)

	if err := h.Save(rec); err != nil {
		return err
	}
	return e.JSON(http.StatusCreated, ruleRecordToResponse(rec))
}

func (h *Hub) updateNotificationRule(e *core.RequestEvent) error {
	id := e.Request.PathValue("id")
	rec, err := h.FindRecordById("notification_rules", id)
	if err != nil {
		return e.NotFoundError("Rule not found", nil)
	}

	var input notificationRuleInput
	if err := e.BindBody(&input); err != nil {
		return e.BadRequestError("Invalid request body", err)
	}

	applyRuleFields(rec, &input)

	if err := h.Save(rec); err != nil {
		return err
	}
	return e.JSON(http.StatusOK, ruleRecordToResponse(rec))
}

func (h *Hub) deleteNotificationRule(e *core.RequestEvent) error {
	id := e.Request.PathValue("id")
	rec, err := h.FindRecordById("notification_rules", id)
	if err != nil {
		return e.NotFoundError("Rule not found", nil)
	}
	if err := h.Delete(rec); err != nil {
		return err
	}
	return e.JSON(http.StatusOK, map[string]bool{"ok": true})
}

// --- Log handler ---

func (h *Hub) getNotificationLogs(e *core.RequestEvent) error {
	page := 1
	if p := e.Request.URL.Query().Get("page"); p != "" {
		if n, err := strconv.Atoi(p); err == nil && n > 0 {
			page = n
		} else {
			return e.BadRequestError("Invalid page", err)
		}
	}

	limit := 50
	if l := e.Request.URL.Query().Get("limit"); l != "" {
		if n, err := strconv.Atoi(l); err == nil && n > 0 && n <= 500 {
			limit = n
		} else {
			return e.BadRequestError("Invalid limit", err)
		}
	}

	filter := ""
	params := dbx.Params{}
	addClause := func(clause string, key string, value any) {
		if filter != "" {
			filter += " && "
		}
		filter += clause
		params[key] = value
	}

	if ruleID := e.Request.URL.Query().Get("rule_id"); ruleID != "" {
		addClause("rule = {:rule_id}", "rule_id", ruleID)
	}
	if resourceID := e.Request.URL.Query().Get("resource_id"); resourceID != "" {
		addClause("resource_id = {:resource_id}", "resource_id", resourceID)
	}
	if status := e.Request.URL.Query().Get("status"); status != "" {
		addClause("status = {:status}", "status", status)
	}
	if eventKind := e.Request.URL.Query().Get("event_kind"); eventKind != "" {
		addClause("event_kind = {:event_kind}", "event_kind", eventKind)
	}
	if since := e.Request.URL.Query().Get("since"); since != "" {
		parsed, err := time.Parse(time.RFC3339, since)
		if err != nil {
			return e.BadRequestError("Invalid since timestamp", err)
		}
		addClause("sent_at >= {:since}", "since", parsed.UTC())
	}
	if until := e.Request.URL.Query().Get("until"); until != "" {
		parsed, err := time.Parse(time.RFC3339, until)
		if err != nil {
			return e.BadRequestError("Invalid until timestamp", err)
		}
		addClause("sent_at <= {:until}", "until", parsed.UTC())
	}

	offset := (page - 1) * limit
	records, err := h.FindRecordsByFilter("notification_logs", filter, "-sent_at", limit+1, offset, params)
	if err != nil {
		return err
	}

	hasMore := len(records) > limit
	if hasMore {
		records = records[:limit]
	}

	result := make([]notificationLogResponse, 0, len(records))
	for _, rec := range records {
		entry := notificationLogResponse{
			ID:             rec.Id,
			Rule:           rec.GetString("rule"),
			Channel:        rec.GetString("channel"),
			ChannelKind:    rec.GetString("channel_kind"),
			CreatedBy:      rec.GetString("created_by"),
			EventKind:      rec.GetString("event_kind"),
			ResourceID:     rec.GetString("resource_id"),
			ResourceName:   rec.GetString("resource_name"),
			ResourceType:   rec.GetString("resource_type"),
			Status:         rec.GetString("status"),
			Error:          rec.GetString("error"),
			PayloadPreview: rec.GetString("payload_preview"),
		}
		if !rec.GetDateTime("sent_at").IsZero() {
			entry.SentAt = rec.GetDateTime("sent_at").Time().UTC().Format("2006-01-02T15:04:05Z")
		}
		result = append(result, entry)
	}
	return e.JSON(http.StatusOK, notificationLogsPageResponse{
		Items:   result,
		Page:    page,
		Limit:   limit,
		HasMore: hasMore,
	})
}

func (h *Hub) getUnreadNotificationLogs(e *core.RequestEvent) error {
	limit := 10
	if l := e.Request.URL.Query().Get("limit"); l != "" {
		if n, err := strconv.Atoi(l); err == nil && n > 0 && n <= 100 {
			limit = n
		} else {
			return e.BadRequestError("Invalid limit", err)
		}
	}

	cutoff, err := h.getNotificationCenterLastReadAt(e.Auth.Id)
	if err != nil {
		return err
	}

	params := dbx.Params{"created_by": e.Auth.Id}
	records, err := h.FindRecordsByFilter("notification_logs", "created_by = {:created_by}", "-sent_at", 0, 0, params)
	if err != nil {
		return err
	}

	capLimit := limit
	if len(records) < capLimit {
		capLimit = len(records)
	}
	items := make([]notificationLogResponse, 0, capLimit)
	count := 0
	for _, rec := range records {
		if !isNotificationCenterLog(rec) {
			continue
		}
		sentAt := rec.GetDateTime("sent_at").Time().UTC()
		if !cutoff.IsZero() && !sentAt.After(cutoff) {
			continue
		}
		count++
		if len(items) < limit {
			items = append(items, notificationLogResponse{
				ID:             rec.Id,
				Rule:           rec.GetString("rule"),
				Channel:        rec.GetString("channel"),
				ChannelKind:    rec.GetString("channel_kind"),
				CreatedBy:      rec.GetString("created_by"),
				EventKind:      rec.GetString("event_kind"),
				ResourceID:     rec.GetString("resource_id"),
				ResourceName:   rec.GetString("resource_name"),
				ResourceType:   rec.GetString("resource_type"),
				Status:         rec.GetString("status"),
				Error:          rec.GetString("error"),
				PayloadPreview: rec.GetString("payload_preview"),
				SentAt:         formatRecordDateTime(rec, "sent_at"),
			})
		}
	}

	return e.JSON(http.StatusOK, notificationUnreadResponse{Count: count, Items: items})
}

func (h *Hub) markAllNotificationLogsRead(e *core.RequestEvent) error {
	cutoff, err := h.getNotificationCenterLastReadAt(e.Auth.Id)
	if err != nil {
		return err
	}

	params := dbx.Params{"created_by": e.Auth.Id}
	records, err := h.FindRecordsByFilter("notification_logs", "created_by = {:created_by}", "-sent_at", 0, 0, params)
	if err != nil {
		return err
	}

	updated := 0
	for _, rec := range records {
		if !isNotificationCenterLog(rec) {
			continue
		}
		sentAt := rec.GetDateTime("sent_at").Time().UTC()
		if !cutoff.IsZero() && !sentAt.After(cutoff) {
			continue
		}
		updated++
	}

	if err := h.setNotificationCenterLastReadAt(e.Auth.Id, time.Now().UTC()); err != nil {
		return err
	}

	return e.JSON(http.StatusOK, map[string]any{"ok": true, "updated": updated})
}

// --- Helper functions ---

func channelRecordToResponse(rec *core.Record) notificationChannelResponse {
	var config map[string]any
	_ = rec.UnmarshalJSONField("config", &config)
	kind := rec.GetString("kind")
	return notificationChannelResponse{
		ID:      rec.Id,
		Name:    rec.GetString("name"),
		Kind:    kind,
		Enabled: rec.GetBool("enabled"),
		Config:  notifications.RedactConfig(kind, config),
		Created: rec.GetDateTime("created").String(),
		Updated: rec.GetDateTime("updated").String(),
	}
}

func ruleRecordToResponse(rec *core.Record) notificationRuleResponse {
	var events []string
	_ = rec.UnmarshalJSONField("events", &events)
	var filter map[string]any
	_ = rec.UnmarshalJSONField("filter", &filter)
	channels := rec.GetStringSlice("channels")
	if events == nil {
		events = []string{}
	}
	if channels == nil {
		channels = []string{}
	}
	return notificationRuleResponse{
		ID:              rec.Id,
		Name:            rec.GetString("name"),
		Enabled:         rec.GetBool("enabled"),
		Events:          events,
		Filter:          filter,
		Channels:        channels,
		MinSeverity:     rec.GetString("min_severity"),
		ThrottleSeconds: rec.GetInt("throttle_seconds"),
		Created:         rec.GetDateTime("created").String(),
		Updated:         rec.GetDateTime("updated").String(),
	}
}

func formatRecordDateTime(rec *core.Record, field string) string {
	if rec.GetDateTime(field).IsZero() {
		return ""
	}
	return rec.GetDateTime(field).Time().UTC().Format("2006-01-02T15:04:05Z")
}

func isNotificationCenterLog(rec *core.Record) bool {
	switch rec.GetString("event_kind") {
	case "monitor.down", "monitor.up", "agent.offline", "agent.online", "container_image.update_available":
		return true
	default:
		return false
	}
}

func (h *Hub) getNotificationCenterLastReadAt(userID string) (time.Time, error) {
	if v, ok := h.notificationReadAt.Load(userID); ok {
		if at, ok := v.(time.Time); ok {
			return at.UTC(), nil
		}
	}

	rec, err := h.FindFirstRecordByFilter("user_settings", "user = {:user}", dbx.Params{"user": userID})
	if err != nil {
		return time.Time{}, nil
	}

	var settings map[string]any
	if err := rec.UnmarshalJSONField("settings", &settings); err != nil {
		return time.Time{}, err
	}
	lastRead, _ := settings["notification_last_read_at"].(string)
	if lastRead == "" {
		return time.Time{}, nil
	}
	parsed, err := time.Parse(time.RFC3339, lastRead)
	if err != nil {
		return time.Time{}, nil
	}
	return parsed.UTC(), nil
}

func (h *Hub) setNotificationCenterLastReadAt(userID string, at time.Time) error {
	rec, err := h.FindFirstRecordByFilter("user_settings", "user = {:user}", dbx.Params{"user": userID})
	if err != nil {
		collection, colErr := h.FindCachedCollectionByNameOrId("user_settings")
		if colErr != nil {
			return colErr
		}
		rec = core.NewRecord(collection)
		rec.Set("user", userID)
	}

	var settings map[string]any
	_ = rec.UnmarshalJSONField("settings", &settings)
	if settings == nil {
		settings = map[string]any{}
	}
	settings["notification_last_read_at"] = at.UTC().Format(time.RFC3339)
	rec.Set("settings", settings)
	if err := h.SaveNoValidate(rec); err != nil {
		return err
	}
	h.notificationReadAt.Store(userID, at.UTC())
	return nil
}

func validateChannelInput(input *notificationChannelInput) error {
	if input.Name == "" {
		return fmt.Errorf("name is required")
	}
	validKinds := map[string]bool{
		"email": true, "webhook": true, "slack": true, "teams": true,
		"gchat": true, "ntfy": true, "gotify": true, "in-app": true,
	}
	if !validKinds[input.Kind] {
		return fmt.Errorf("invalid kind %q", input.Kind)
	}
	if input.Config != nil {
		if provider, ok := providers.Get(input.Kind); ok {
			return provider.ValidateConfig(input.Config)
		}
	}
	return nil
}

func applyRuleFields(rec *core.Record, input *notificationRuleInput) {
	if input.Name != "" {
		rec.Set("name", input.Name)
	}
	if input.Enabled != nil {
		rec.Set("enabled", *input.Enabled)
	}
	if input.Events != nil {
		rec.Set("events", input.Events)
	}
	if input.Filter != nil {
		rec.Set("filter", input.Filter)
	}
	if input.Channels != nil {
		rec.Set("channels", input.Channels)
	}
	if input.MinSeverity == "" {
		rec.Set("min_severity", "info")
	} else {
		rec.Set("min_severity", input.MinSeverity)
	}
	rec.Set("throttle_seconds", input.ThrottleSeconds)
}

// mergeChannelConfig preserves existing secrets when the client sends "**REDACTED**" back.
func mergeChannelConfig(kind string, rec *core.Record, newConfig map[string]any) map[string]any {
	var existing map[string]any
	_ = rec.UnmarshalJSONField("config", &existing)
	if existing == nil {
		existing = map[string]any{}
	}

	sensitive := map[string]bool{}
	if provider, ok := providers.Get(kind); ok {
		for _, k := range provider.SensitiveConfigKeys() {
			sensitive[k] = true
		}
	}

	result := make(map[string]any, len(existing))
	for k, v := range existing {
		result[k] = v
	}
	for k, v := range newConfig {
		if sensitive[k] && v == "**REDACTED**" {
			continue // keep existing value
		}
		result[k] = v
	}
	return result
}
