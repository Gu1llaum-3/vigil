package hub

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/Gu1llaum-3/vigil/internal/hub/notifications"
	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

const systemNotificationsCollection = "system_notifications"

var systemNotificationCategories = []string{"monitors", "agents", "container_images"}

var systemNotificationEventKinds = []string{
	string(notifications.EventMonitorDown),
	string(notifications.EventMonitorUp),
	string(notifications.EventAgentOffline),
	string(notifications.EventAgentOnline),
	string(notifications.EventContainerImageUpdateAvailable),
}

type systemNotificationResponse struct {
	ID           string         `json:"id"`
	EventKind    string         `json:"event_kind"`
	Category     string         `json:"category"`
	Severity     string         `json:"severity"`
	ResourceType string         `json:"resource_type"`
	ResourceID   string         `json:"resource_id"`
	ResourceName string         `json:"resource_name,omitempty"`
	Title        string         `json:"title"`
	Message      string         `json:"message,omitempty"`
	Payload      map[string]any `json:"payload,omitempty"`
	OccurredAt   string         `json:"occurred_at"`
	Read         bool           `json:"read"`
}

type systemNotificationsPageResponse struct {
	Items   []systemNotificationResponse `json:"items"`
	Page    int                          `json:"page"`
	Limit   int                          `json:"limit"`
	HasMore bool                         `json:"has_more"`
}

type systemNotificationUnreadResponse struct {
	Count int                          `json:"count"`
	Items []systemNotificationResponse `json:"items"`
}

type systemNotificationPreferences struct {
	EnabledCategories map[string]bool `json:"enabled_categories"`
	EnabledEvents     map[string]bool `json:"enabled_events"`
}

func (h *Hub) createSystemNotification(evt notifications.Event) error {
	collection, err := h.FindCachedCollectionByNameOrId(systemNotificationsCollection)
	if err != nil {
		return nil
	}
	if evt.OccurredAt.IsZero() {
		evt.OccurredAt = time.Now().UTC()
	}

	title, message, err := notifications.RenderMessage(evt)
	if err != nil {
		title = string(evt.Kind)
		message = string(evt.Kind)
	}
	if evt.Details != nil {
		if customTitle, ok := evt.Details["title"].(string); ok && customTitle != "" {
			title = customTitle
		}
		if customMessage, ok := evt.Details["message"].(string); ok && customMessage != "" {
			message = customMessage
		}
	}

	rec := core.NewRecord(collection)
	rec.Set("event_kind", string(evt.Kind))
	rec.Set("category", systemNotificationCategory(evt))
	rec.Set("severity", evt.EffectiveSeverity())
	rec.Set("resource_type", evt.Resource.Type)
	rec.Set("resource_id", evt.Resource.ID)
	rec.Set("resource_name", evt.Resource.Name)
	rec.Set("title", title)
	rec.Set("message", message)
	rec.Set("payload", evt.Details)
	rec.Set("occurred_at", evt.OccurredAt.UTC().Format(time.RFC3339))
	return h.SaveNoValidate(rec)
}

func systemNotificationCategory(evt notifications.Event) string {
	switch evt.Resource.Type {
	case "monitor":
		return "monitors"
	case "agent":
		return "agents"
	case "container_image":
		return "container_images"
	default:
		return "monitors"
	}
}

func (h *Hub) getSystemNotifications(e *core.RequestEvent) error {
	page, limit, err := parsePageLimit(e, 25, 100)
	if err != nil {
		return err
	}
	prefs, err := h.systemNotificationPreferencesForUser(e.Auth.Id)
	if err != nil {
		return err
	}

	filter, params := systemNotificationFilter(e, false)
	records, err := h.FindRecordsByFilter(systemNotificationsCollection, filter, "-occurred_at", 0, 0, params)
	if err != nil {
		return err
	}

	status := e.Request.URL.Query().Get("status")
	items := make([]systemNotificationResponse, 0, limit)
	skipped := 0
	start := (page - 1) * limit
	hasMore := false
	for _, rec := range records {
		entry := h.systemNotificationRecordToResponse(rec, prefs)
		if status == "unread" && entry.Read {
			continue
		}
		if skipped < start {
			skipped++
			continue
		}
		if len(items) >= limit {
			hasMore = true
			break
		}
		items = append(items, entry)
	}

	return e.JSON(http.StatusOK, systemNotificationsPageResponse{Items: items, Page: page, Limit: limit, HasMore: hasMore})
}

func (h *Hub) getUnreadSystemNotifications(e *core.RequestEvent) error {
	limit := 8
	if l := e.Request.URL.Query().Get("limit"); l != "" {
		parsed, err := strconv.Atoi(l)
		if err != nil || parsed <= 0 || parsed > 100 {
			return e.BadRequestError("Invalid limit", err)
		}
		limit = parsed
	}
	prefs, err := h.systemNotificationPreferencesForUser(e.Auth.Id)
	if err != nil {
		return err
	}

	records, err := h.FindRecordsByFilter(systemNotificationsCollection, "", "-occurred_at", 0, 0)
	if err != nil {
		return err
	}

	items := make([]systemNotificationResponse, 0, limit)
	count := 0
	for _, rec := range records {
		category := rec.GetString("category")
		if !prefs.EnabledCategories[category] {
			continue
		}
		if !prefs.EnabledEvents[rec.GetString("event_kind")] {
			continue
		}
		entry := h.systemNotificationRecordToResponse(rec, prefs)
		if entry.Read {
			continue
		}
		count++
		if len(items) < limit {
			items = append(items, entry)
		}
	}

	return e.JSON(http.StatusOK, systemNotificationUnreadResponse{Count: count, Items: items})
}

func (h *Hub) markSystemNotificationsRead(e *core.RequestEvent) error {
	prefs, err := h.systemNotificationPreferencesForUser(e.Auth.Id)
	if err != nil {
		return err
	}
	category := e.Request.URL.Query().Get("category")
	categories := systemNotificationCategories
	if category != "" {
		categories = []string{category}
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	for _, cat := range categories {
		prefs.LastReadAtByCategory[cat] = now
	}
	if err := h.saveSystemNotificationPreferences(e.Auth.Id, prefs); err != nil {
		return err
	}
	return e.JSON(http.StatusOK, map[string]any{"ok": true})
}

func (h *Hub) getSystemNotificationPreferences(e *core.RequestEvent) error {
	prefs, err := h.systemNotificationPreferencesForUser(e.Auth.Id)
	if err != nil {
		return err
	}
	return e.JSON(http.StatusOK, systemNotificationPreferences{EnabledCategories: prefs.EnabledCategories, EnabledEvents: prefs.EnabledEvents})
}

func (h *Hub) updateSystemNotificationPreferences(e *core.RequestEvent) error {
	var input systemNotificationPreferences
	if err := e.BindBody(&input); err != nil {
		return e.BadRequestError("Invalid request body", err)
	}
	prefs, err := h.systemNotificationPreferencesForUser(e.Auth.Id)
	if err != nil {
		return err
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	for _, cat := range systemNotificationCategories {
		if enabled, ok := input.EnabledCategories[cat]; ok {
			previous := prefs.EnabledCategories[cat]
			prefs.EnabledCategories[cat] = enabled
			if enabled && !previous {
				prefs.LastReadAtByCategory[cat] = now
			}
		}
	}
	for _, eventKind := range systemNotificationEventKinds {
		if enabled, ok := input.EnabledEvents[eventKind]; ok {
			prefs.EnabledEvents[eventKind] = enabled
		}
	}
	if err := h.saveSystemNotificationPreferences(e.Auth.Id, prefs); err != nil {
		return err
	}
	return e.JSON(http.StatusOK, systemNotificationPreferences{EnabledCategories: prefs.EnabledCategories, EnabledEvents: prefs.EnabledEvents})
}

type systemNotificationUserPreferences struct {
	EnabledCategories    map[string]bool
	EnabledEvents        map[string]bool
	LastReadAtByCategory map[string]string
}

func (h *Hub) systemNotificationPreferencesForUser(userID string) (systemNotificationUserPreferences, error) {
	prefs := defaultSystemNotificationPreferences()
	rec, err := h.FindFirstRecordByFilter("user_settings", "user = {:user}", dbx.Params{"user": userID})
	if err != nil {
		return prefs, nil
	}
	var settings map[string]any
	if err := rec.UnmarshalJSONField("settings", &settings); err != nil {
		return prefs, err
	}
	if raw, ok := settings["system_notifications_enabled_categories"].(map[string]any); ok {
		for _, cat := range systemNotificationCategories {
			if enabled, ok := raw[cat].(bool); ok {
				prefs.EnabledCategories[cat] = enabled
			}
		}
	}
	if raw, ok := settings["system_notifications_enabled_events"].(map[string]any); ok {
		for _, eventKind := range systemNotificationEventKinds {
			if enabled, ok := raw[eventKind].(bool); ok {
				prefs.EnabledEvents[eventKind] = enabled
			}
		}
	}
	if raw, ok := settings["system_notifications_last_read_at_by_category"].(map[string]any); ok {
		for _, cat := range systemNotificationCategories {
			if value, ok := raw[cat].(string); ok {
				prefs.LastReadAtByCategory[cat] = value
			}
		}
	}
	if raw, ok := h.systemNotificationReadAt.Load(userID); ok {
		if cached, ok := raw.(map[string]string); ok {
			for _, cat := range systemNotificationCategories {
				if value := cached[cat]; value != "" {
					prefs.LastReadAtByCategory[cat] = value
				}
			}
		}
	}
	return prefs, nil
}

func defaultSystemNotificationPreferences() systemNotificationUserPreferences {
	prefs := systemNotificationUserPreferences{
		EnabledCategories:    map[string]bool{},
		EnabledEvents:        map[string]bool{},
		LastReadAtByCategory: map[string]string{},
	}
	for _, cat := range systemNotificationCategories {
		prefs.EnabledCategories[cat] = true
	}
	for _, eventKind := range systemNotificationEventKinds {
		prefs.EnabledEvents[eventKind] = true
	}
	return prefs
}

func (h *Hub) saveSystemNotificationPreferences(userID string, prefs systemNotificationUserPreferences) error {
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
	settings["system_notifications_enabled_categories"] = prefs.EnabledCategories
	settings["system_notifications_enabled_events"] = prefs.EnabledEvents
	settings["system_notifications_last_read_at_by_category"] = prefs.LastReadAtByCategory
	rec.Set("settings", settings)
	h.systemNotificationReadAt.Store(userID, prefs.LastReadAtByCategory)
	return h.SaveNoValidate(rec)
}

func (h *Hub) systemNotificationRecordToResponse(rec *core.Record, prefs systemNotificationUserPreferences) systemNotificationResponse {
	var payload map[string]any
	_ = rec.UnmarshalJSONField("payload", &payload)
	category := rec.GetString("category")
	occurred := rec.GetDateTime("occurred_at").Time().UTC()
	read := false
	if lastRead := prefs.LastReadAtByCategory[category]; lastRead != "" {
		if parsed, err := time.Parse(time.RFC3339, lastRead); err == nil && !occurred.After(parsed.UTC()) {
			read = true
		}
	}
	return systemNotificationResponse{
		ID:           rec.Id,
		EventKind:    rec.GetString("event_kind"),
		Category:     category,
		Severity:     rec.GetString("severity"),
		ResourceType: rec.GetString("resource_type"),
		ResourceID:   rec.GetString("resource_id"),
		ResourceName: rec.GetString("resource_name"),
		Title:        rec.GetString("title"),
		Message:      rec.GetString("message"),
		Payload:      payload,
		OccurredAt:   formatRecordDateTime(rec, "occurred_at"),
		Read:         read,
	}
}

func parsePageLimit(e *core.RequestEvent, defaultLimit, maxLimit int) (int, int, error) {
	page := 1
	if p := e.Request.URL.Query().Get("page"); p != "" {
		parsed, err := strconv.Atoi(p)
		if err != nil || parsed <= 0 {
			return 0, 0, e.BadRequestError("Invalid page", err)
		}
		page = parsed
	}
	limit := defaultLimit
	if l := e.Request.URL.Query().Get("limit"); l != "" {
		parsed, err := strconv.Atoi(l)
		if err != nil || parsed <= 0 || parsed > maxLimit {
			return 0, 0, e.BadRequestError("Invalid limit", err)
		}
		limit = parsed
	}
	return page, limit, nil
}

func systemNotificationFilter(e *core.RequestEvent, enabledOnly bool) (string, dbx.Params) {
	filter := ""
	params := dbx.Params{}
	addClause := func(clause, key string, value any) {
		if filter != "" {
			filter += " && "
		}
		filter += clause
		params[key] = value
	}
	if category := e.Request.URL.Query().Get("category"); category != "" {
		addClause("category = {:category}", "category", category)
	}
	if severity := e.Request.URL.Query().Get("severity"); severity != "" {
		addClause("severity = {:severity}", "severity", severity)
	}
	if eventKind := e.Request.URL.Query().Get("event_kind"); eventKind != "" {
		addClause("event_kind = {:event_kind}", "event_kind", eventKind)
	}
	if q := strings.TrimSpace(e.Request.URL.Query().Get("q")); q != "" {
		addClause("(resource_name ~ {:q} || title ~ {:q} || message ~ {:q})", "q", q)
	}
	_ = enabledOnly
	return filter, params
}
