package hub

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/Gu1llaum-3/vigil/internal/hub/notifications"
	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/types"
)

const systemNotificationsCollection = "system_notifications"

var systemNotificationCategories = []string{"monitors", "agents", "container_images", "host_metrics"}

var systemNotificationEventKinds = []string{
	string(notifications.EventMonitorDown),
	string(notifications.EventMonitorUp),
	string(notifications.EventAgentOffline),
	string(notifications.EventAgentOnline),
	string(notifications.EventContainerImageUpdateAvailable),
	string(notifications.EventHostMetricExceeded),
	string(notifications.EventHostMetricRecovered),
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
	switch evt.Kind {
	case notifications.EventHostMetricExceeded, notifications.EventHostMetricRecovered:
		return "host_metrics"
	}
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

	where := systemNotificationFilter(e)
	if e.Request.URL.Query().Get("status") == "unread" {
		unread := unreadSystemNotificationsExpr(prefs, systemNotificationCategories)
		if where == nil {
			where = unread
		} else {
			where = dbx.And(where, unread)
		}
	}
	// One more than the page, to tell whether another page follows.
	records, err := h.querySystemNotifications(where, limit+1, (page-1)*limit)
	if err != nil {
		return err
	}
	hasMore := len(records) > limit
	if hasMore {
		records = records[:limit]
	}
	items := make([]systemNotificationResponse, 0, len(records))
	for _, rec := range records {
		items = append(items, h.systemNotificationRecordToResponse(rec, prefs))
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

	var categories []string
	var events []any
	for _, cat := range systemNotificationCategories {
		if prefs.EnabledCategories[cat] {
			categories = append(categories, cat)
		}
	}
	for _, kind := range systemNotificationEventKinds {
		if prefs.EnabledEvents[kind] {
			events = append(events, kind)
		}
	}
	response := systemNotificationUnreadResponse{Items: []systemNotificationResponse{}}
	if len(categories) == 0 || len(events) == 0 {
		return e.JSON(http.StatusOK, response)
	}
	where := dbx.And(dbx.In("event_kind", events...), unreadSystemNotificationsExpr(prefs, categories))

	if err := h.DB().Select("COUNT(*)").From(systemNotificationsCollection).Where(where).Row(&response.Count); err != nil {
		return err
	}
	records, err := h.querySystemNotifications(where, limit, 0)
	if err != nil {
		return err
	}
	for _, rec := range records {
		response.Items = append(response.Items, h.systemNotificationRecordToResponse(rec, prefs))
	}
	return e.JSON(http.StatusOK, response)
}

// querySystemNotifications returns a page of the feed matching where, newest first; the
// filtering and paging happen in SQL (indexes on occurred_at and (category, occurred_at)).
func (h *Hub) querySystemNotifications(where dbx.Expression, limit, offset int) ([]*core.Record, error) {
	query := h.RecordQuery(systemNotificationsCollection)
	if where != nil { // dbx renders a nil condition as "()"
		query = query.AndWhere(where)
	}
	var records []*core.Record
	err := query.OrderBy("occurred_at DESC", "id DESC").Limit(int64(limit)).Offset(int64(offset)).All(&records)
	return records, err
}

// unreadSystemNotificationsExpr matches the entries of categories newer than the user's read
// cursor for their category (all of them for a category never read). It must agree with the
// Read flag computed by systemNotificationRecordToResponse.
func unreadSystemNotificationsExpr(prefs systemNotificationUserPreferences, categories []string) dbx.Expression {
	clauses := make([]dbx.Expression, 0, len(categories))
	for _, cat := range categories {
		inCategory := dbx.HashExp{"category": cat}
		lastRead, err := time.Parse(time.RFC3339, prefs.LastReadAtByCategory[cat])
		if err != nil {
			clauses = append(clauses, inCategory)
			continue
		}
		// occurred_at is stored at millisecond precision, so comparing with the cursor
		// truncated to the millisecond gives the same answer as comparing with the cursor.
		cursor, _ := types.ParseDateTime(lastRead.UTC())
		clauses = append(clauses, dbx.And(inCategory, dbx.NewExp("occurred_at > {:cursor_"+cat+"}", dbx.Params{"cursor_" + cat: cursor.String()})))
	}
	if len(clauses) == 0 {
		return dbx.NewExp("0")
	}
	return dbx.Or(clauses...)
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

var likeEscaper = strings.NewReplacer(`\`, `\\`, "%", `\%`, "_", `\_`)

// systemNotificationFilter builds the history filters from the query string (nil: none).
func systemNotificationFilter(e *core.RequestEvent) dbx.Expression {
	query := e.Request.URL.Query()
	clauses := []dbx.Expression{}
	for _, field := range []string{"category", "severity", "event_kind"} {
		if value := query.Get(field); value != "" {
			clauses = append(clauses, dbx.HashExp{field: value})
		}
	}
	if q := strings.TrimSpace(query.Get("q")); q != "" {
		// dbx.Like escapes the wildcards but adds no ESCAPE clause, which SQLite needs.
		pattern := "%" + likeEscaper.Replace(q) + "%"
		clauses = append(clauses, dbx.NewExp(
			`(resource_name LIKE {:q} ESCAPE '\' OR title LIKE {:q} ESCAPE '\' OR message LIKE {:q} ESCAPE '\')`,
			dbx.Params{"q": pattern}))
	}
	if len(clauses) == 0 {
		return nil
	}
	return dbx.And(clauses...)
}
