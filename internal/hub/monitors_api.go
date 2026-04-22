package hub

import (
	"net/http"
	"strconv"
	"time"

	"github.com/google/uuid"
	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// MonitorGroupResponse is a group with its monitors, returned by the API.
type MonitorGroupResponse struct {
	ID       string          `json:"id"`
	Name     string          `json:"name"`
	Weight   int             `json:"weight"`
	Monitors []MonitorRecord `json:"monitors"`
}

// MonitorRecord is a monitor with its current status, returned by the API.
type MonitorRecord struct {
	ID                    string              `json:"id"`
	Name                  string              `json:"name"`
	Type                  string              `json:"type"`
	Group                 string              `json:"group"`
	Active                bool                `json:"active"`
	Interval              int                 `json:"interval"`
	Timeout               int                 `json:"timeout"`
	URL                   string              `json:"url,omitempty"`
	HTTPMethod            string              `json:"http_method,omitempty"`
	Keyword               string              `json:"keyword,omitempty"`
	KeywordInvert         bool                `json:"keyword_invert,omitempty"`
	Hostname              string              `json:"hostname,omitempty"`
	Port                  int                 `json:"port,omitempty"`
	DNSHost               string              `json:"dns_host,omitempty"`
	DNSType               string              `json:"dns_type,omitempty"`
	DNSServer             string              `json:"dns_server,omitempty"`
	PushToken             string              `json:"push_token,omitempty"`
	PushURL               string              `json:"push_url,omitempty"`
	PingCount             int                 `json:"ping_count,omitempty"`
	PingPerRequestTimeout int                 `json:"ping_per_request_timeout,omitempty"`
	PingIPFamily          string              `json:"ping_ip_family,omitempty"`
	FailureThreshold      int                 `json:"failure_threshold"`
	Status                int                 `json:"status"`
	LastCheckedAt         string              `json:"last_checked_at"`
	LastLatencyMs         int                 `json:"last_latency_ms"`
	LastMsg               string              `json:"last_msg"`
	AvgLatency24hMs       *float64            `json:"avg_latency_24h_ms,omitempty"`
	Uptime24h             *float64            `json:"uptime_24h,omitempty"`
	Uptime30d             *float64            `json:"uptime_30d,omitempty"`
	RecentChecks          []MonitorCheckPoint `json:"recent_checks,omitempty"`
}

type MonitorCheckPoint struct {
	Status    int    `json:"status"`
	CheckedAt string `json:"checked_at"`
}

type monitorMetrics struct {
	AvgLatency24hMs float64 `db:"avg_latency_24h_ms"`
	Total24h        int     `db:"total_24h"`
	Up24h           int     `db:"up_24h"`
	Total30d        int     `db:"total_30d"`
	Up30d           int     `db:"up_30d"`
}

func (h *Hub) loadMonitorMetrics(m *core.Record) (*MonitorMetrics, error) {
	now := time.Now()

	var row monitorMetrics
	query := `
SELECT
	COUNT(*) AS total_30d,
	COALESCE(SUM(CASE WHEN checked_at >= {:since24} THEN 1 ELSE 0 END), 0) AS total_24h,
	COALESCE(SUM(CASE WHEN checked_at >= {:since24} AND status = 1 THEN 1 ELSE 0 END), 0) AS up_24h,
	COALESCE(SUM(CASE WHEN status = 1 THEN 1 ELSE 0 END), 0) AS up_30d,
	COALESCE(AVG(CASE WHEN checked_at >= {:since24} THEN latency_ms END), 0) AS avg_latency_24h_ms
FROM monitor_events
WHERE monitor = {:id} AND checked_at >= {:since30}`
	if err := h.DB().NewQuery(query).Bind(dbx.Params{
		"id":      m.Id,
		"since24": now.Add(-24 * time.Hour).UTC(),
		"since30": now.Add(-30 * 24 * time.Hour).UTC(),
	}).One(&row); err != nil {
		return nil, err
	}

	metrics := &MonitorMetrics{}
	if row.Total24h > 0 {
		if m.GetString("type") != "push" {
			avg := row.AvgLatency24hMs
			metrics.AvgLatency24hMs = &avg
		}
		uptime24 := float64(row.Up24h) / float64(row.Total24h) * 100
		metrics.Uptime24h = &uptime24
	}
	if row.Total30d > 0 {
		uptime30 := float64(row.Up30d) / float64(row.Total30d) * 100
		metrics.Uptime30d = &uptime30
	}
	return metrics, nil
}

type MonitorMetrics struct {
	AvgLatency24hMs *float64
	Uptime24h       *float64
	Uptime30d       *float64
}

func monitorToRecord(m *core.Record, appURL string, metrics *MonitorMetrics, recentChecks []MonitorCheckPoint) MonitorRecord {
	r := MonitorRecord{
		ID:            m.Id,
		Name:          m.GetString("name"),
		Type:          m.GetString("type"),
		Group:         m.GetString("group"),
		Active:        m.GetBool("active"),
		Interval:      m.GetInt("interval"),
		Timeout:       m.GetInt("timeout"),
		URL:           m.GetString("url"),
		HTTPMethod:    m.GetString("http_method"),
		Keyword:       m.GetString("keyword"),
		KeywordInvert: m.GetBool("keyword_invert"),
		Hostname:      m.GetString("hostname"),
		Port:          m.GetInt("port"),
		DNSHost:       m.GetString("dns_host"),
		DNSType:       m.GetString("dns_type"),
		DNSServer:     m.GetString("dns_server"),
		PushToken:     m.GetString("push_token"),
		PingCount: func() int {
			if raw := m.Get("ping_count"); raw == nil {
				return 1
			}
			return m.GetInt("ping_count")
		}(),
		PingPerRequestTimeout: func() int {
			if raw := m.Get("ping_per_request_timeout"); raw == nil {
				return 2
			}
			return m.GetInt("ping_per_request_timeout")
		}(),
		PingIPFamily: m.GetString("ping_ip_family"),
		FailureThreshold: func() int {
			if raw := m.Get("failure_threshold"); raw == nil {
				return 3
			}
			return m.GetInt("failure_threshold")
		}(),
		Status:        m.GetInt("status"),
		LastLatencyMs: m.GetInt("last_latency_ms"),
		LastMsg:       m.GetString("last_msg"),
	}
	if metrics != nil {
		r.AvgLatency24hMs = metrics.AvgLatency24hMs
		r.Uptime24h = metrics.Uptime24h
		r.Uptime30d = metrics.Uptime30d
	}
	if len(recentChecks) > 0 {
		r.RecentChecks = recentChecks
	}
	if !m.GetDateTime("last_checked_at").IsZero() {
		r.LastCheckedAt = m.GetDateTime("last_checked_at").Time().UTC().Format(time.RFC3339)
	}
	if r.PushToken != "" && appURL != "" {
		r.PushURL = appURL + "/api/app/push/" + r.PushToken
	}
	return r
}

func (h *Hub) loadRecentChecks(m *core.Record, limit int) ([]MonitorCheckPoint, error) {
	events, err := h.FindRecordsByFilter(
		"monitor_events",
		"monitor = {:id}",
		"-checked_at",
		limit,
		0,
		dbx.Params{"id": m.Id},
	)
	if err != nil {
		return nil, err
	}

	checks := make([]MonitorCheckPoint, 0, len(events))
	for i := len(events) - 1; i >= 0; i-- {
		ev := events[i]
		check := MonitorCheckPoint{Status: ev.GetInt("status")}
		if !ev.GetDateTime("checked_at").IsZero() {
			check.CheckedAt = ev.GetDateTime("checked_at").Time().UTC().Format(time.RFC3339)
		}
		checks = append(checks, check)
	}
	return checks, nil
}

// getMonitor returns a single monitor with its current status and metrics.
func (h *Hub) getMonitor(e *core.RequestEvent) error {
	id := e.Request.PathValue("id")
	rec, err := h.FindRecordById("monitors", id)
	if err != nil {
		return e.NotFoundError("Monitor not found", nil)
	}

	metrics, _ := h.loadMonitorMetrics(rec)
	recentChecks, _ := h.loadRecentChecks(rec, 10)
	return e.JSON(http.StatusOK, monitorToRecord(rec, h.Settings().Meta.AppURL, metrics, recentChecks))
}

// getMonitors returns all groups with their monitors.
func (h *Hub) getMonitors(e *core.RequestEvent) error {
	groups, err := h.FindRecordsByFilter("monitor_groups", "", "weight,name", 0, 0)
	if err != nil {
		return err
	}
	monitors, err := h.FindRecordsByFilter("monitors", "", "name", 0, 0)
	if err != nil {
		return err
	}

	appURL := h.Settings().Meta.AppURL

	groupMap := make(map[string]*MonitorGroupResponse, len(groups))
	result := make([]*MonitorGroupResponse, 0, len(groups)+1)

	for _, g := range groups {
		gr := &MonitorGroupResponse{
			ID:       g.Id,
			Name:     g.GetString("name"),
			Weight:   g.GetInt("weight"),
			Monitors: []MonitorRecord{},
		}
		groupMap[g.Id] = gr
		result = append(result, gr)
	}

	ungrouped := &MonitorGroupResponse{
		ID:       "",
		Name:     "",
		Monitors: []MonitorRecord{},
	}

	for _, m := range monitors {
		metrics, _ := h.loadMonitorMetrics(m)
		recentChecks, _ := h.loadRecentChecks(m, 10)
		mr := monitorToRecord(m, appURL, metrics, recentChecks)
		if gr, ok := groupMap[mr.Group]; ok {
			gr.Monitors = append(gr.Monitors, mr)
		} else {
			ungrouped.Monitors = append(ungrouped.Monitors, mr)
		}
	}

	if len(ungrouped.Monitors) > 0 {
		result = append(result, ungrouped)
	}

	return e.JSON(http.StatusOK, result)
}

// createMonitor creates a new monitor.
func (h *Hub) createMonitor(e *core.RequestEvent) error {
	var body map[string]any
	if err := e.BindBody(&body); err != nil {
		return e.BadRequestError("Invalid body", err)
	}

	col, err := h.FindCachedCollectionByNameOrId("monitors")
	if err != nil {
		return err
	}

	rec := core.NewRecord(col)
	applyMonitorFields(rec, body)
	if _, ok := body["failure_threshold"]; !ok {
		rec.Set("failure_threshold", 3)
	}

	if rec.GetString("type") == "push" && rec.GetString("push_token") == "" {
		rec.Set("push_token", uuid.New().String())
	}
	rec.Set("status", monitorStatusUnknown)

	if err := h.Save(rec); err != nil {
		return err
	}
	if rec.GetBool("active") {
		go h.monitorScheduler.startMonitor(rec.Id)
	}
	return e.JSON(http.StatusOK, monitorToRecord(rec, h.Settings().Meta.AppURL, nil, nil))
}

// updateMonitor updates an existing monitor.
func (h *Hub) updateMonitor(e *core.RequestEvent) error {
	id := e.Request.PathValue("id")
	rec, err := h.FindRecordById("monitors", id)
	if err != nil {
		return e.NotFoundError("Monitor not found", nil)
	}

	var body map[string]any
	if err := e.BindBody(&body); err != nil {
		return e.BadRequestError("Invalid body", err)
	}

	applyMonitorFields(rec, body)

	if rec.GetString("type") == "push" && rec.GetString("push_token") == "" {
		rec.Set("push_token", uuid.New().String())
	}

	if err := h.Save(rec); err != nil {
		return err
	}
	// Restart goroutine with updated config (stop first regardless of active state)
	h.monitorScheduler.stopMonitor(rec.Id)
	if rec.GetBool("active") {
		go h.monitorScheduler.startMonitor(rec.Id)
	}
	return e.JSON(http.StatusOK, monitorToRecord(rec, h.Settings().Meta.AppURL, nil, nil))
}

// moveMonitor updates only the monitor group.
func (h *Hub) moveMonitor(e *core.RequestEvent) error {
	id := e.Request.PathValue("id")
	rec, err := h.FindRecordById("monitors", id)
	if err != nil {
		return e.NotFoundError("Monitor not found", nil)
	}

	var body struct {
		Group string `json:"group"`
	}
	if err := e.BindBody(&body); err != nil {
		return e.BadRequestError("Invalid body", err)
	}

	rec.Set("group", body.Group)
	if err := h.SaveNoValidate(rec); err != nil {
		return err
	}

	return e.JSON(http.StatusOK, monitorToRecord(rec, h.Settings().Meta.AppURL, nil, nil))
}

// deleteMonitor deletes a monitor and its events.
func (h *Hub) deleteMonitor(e *core.RequestEvent) error {
	id := e.Request.PathValue("id")
	rec, err := h.FindRecordById("monitors", id)
	if err != nil {
		return e.NotFoundError("Monitor not found", nil)
	}
	if err := h.Delete(rec); err != nil {
		return err
	}
	return e.JSON(http.StatusOK, map[string]bool{"ok": true})
}

// getMonitorEvents returns recent events for a given monitor.
func (h *Hub) getMonitorEvents(e *core.RequestEvent) error {
	id := e.Request.PathValue("id")
	query := e.Request.URL.Query()
	limit := 500
	if rawLimit := query.Get("limit"); rawLimit != "" {
		parsed, err := strconv.Atoi(rawLimit)
		if err != nil || parsed <= 0 {
			return e.BadRequestError("Invalid limit", err)
		}
		if parsed > 5000 {
			parsed = 5000
		}
		limit = parsed
	}

	filter := "monitor = {:id}"
	params := dbx.Params{"id": id}
	if rawSince := query.Get("since"); rawSince != "" {
		since, err := time.Parse(time.RFC3339, rawSince)
		if err != nil {
			return e.BadRequestError("Invalid since timestamp", err)
		}
		filter += " && checked_at >= {:since}"
		params["since"] = since.UTC()
	}
	if rawUntil := query.Get("until"); rawUntil != "" {
		until, err := time.Parse(time.RFC3339, rawUntil)
		if err != nil {
			return e.BadRequestError("Invalid until timestamp", err)
		}
		filter += " && checked_at <= {:until}"
		params["until"] = until.UTC()
	}

	events, err := h.FindRecordsByFilter(
		"monitor_events",
		filter,
		"-checked_at",
		limit,
		0,
		params,
	)
	if err != nil {
		return err
	}

	type EventEntry struct {
		ID        string `json:"id"`
		Status    int    `json:"status"`
		LatencyMs int    `json:"latency_ms"`
		Msg       string `json:"msg"`
		CheckedAt string `json:"checked_at"`
	}

	result := make([]EventEntry, 0, len(events))
	for _, ev := range events {
		entry := EventEntry{
			ID:        ev.Id,
			Status:    ev.GetInt("status"),
			LatencyMs: ev.GetInt("latency_ms"),
			Msg:       ev.GetString("msg"),
		}
		if !ev.GetDateTime("checked_at").IsZero() {
			entry.CheckedAt = ev.GetDateTime("checked_at").Time().UTC().Format(time.RFC3339)
		}
		result = append(result, entry)
	}
	return e.JSON(http.StatusOK, result)
}

// getMonitorGroups returns all monitor groups.
func (h *Hub) getMonitorGroups(e *core.RequestEvent) error {
	groups, err := h.FindRecordsByFilter("monitor_groups", "", "weight,name", 0, 0)
	if err != nil {
		return err
	}
	type GroupEntry struct {
		ID     string `json:"id"`
		Name   string `json:"name"`
		Weight int    `json:"weight"`
	}
	result := make([]GroupEntry, 0, len(groups))
	for _, g := range groups {
		result = append(result, GroupEntry{
			ID:     g.Id,
			Name:   g.GetString("name"),
			Weight: g.GetInt("weight"),
		})
	}
	return e.JSON(http.StatusOK, result)
}

// createMonitorGroup creates a new monitor group.
func (h *Hub) createMonitorGroup(e *core.RequestEvent) error {
	var body struct {
		Name   string `json:"name"`
		Weight int    `json:"weight"`
	}
	if err := e.BindBody(&body); err != nil {
		return e.BadRequestError("Invalid body", err)
	}
	col, err := h.FindCachedCollectionByNameOrId("monitor_groups")
	if err != nil {
		return err
	}
	rec := core.NewRecord(col)
	rec.Set("name", body.Name)
	rec.Set("weight", body.Weight)
	if err := h.Save(rec); err != nil {
		return err
	}
	return e.JSON(http.StatusOK, map[string]any{
		"id":     rec.Id,
		"name":   rec.GetString("name"),
		"weight": rec.GetInt("weight"),
	})
}

// updateMonitorGroup updates a monitor group.
func (h *Hub) updateMonitorGroup(e *core.RequestEvent) error {
	id := e.Request.PathValue("id")
	rec, err := h.FindRecordById("monitor_groups", id)
	if err != nil {
		return e.NotFoundError("Group not found", nil)
	}
	var body struct {
		Name   string `json:"name"`
		Weight int    `json:"weight"`
	}
	if err := e.BindBody(&body); err != nil {
		return e.BadRequestError("Invalid body", err)
	}
	if body.Name != "" {
		rec.Set("name", body.Name)
	}
	rec.Set("weight", body.Weight)
	if err := h.Save(rec); err != nil {
		return err
	}
	return e.JSON(http.StatusOK, map[string]any{
		"id":     rec.Id,
		"name":   rec.GetString("name"),
		"weight": rec.GetInt("weight"),
	})
}

// deleteMonitorGroup deletes a group and ungroups its monitors.
func (h *Hub) deleteMonitorGroup(e *core.RequestEvent) error {
	id := e.Request.PathValue("id")
	rec, err := h.FindRecordById("monitor_groups", id)
	if err != nil {
		return e.NotFoundError("Group not found", nil)
	}
	// Ungroup monitors before deleting
	orphans, _ := h.FindRecordsByFilter("monitors", "group = {:id}", "", 0, 0, dbx.Params{"id": id})
	for _, m := range orphans {
		m.Set("group", "")
		_ = h.SaveNoValidate(m)
	}
	if err := h.Delete(rec); err != nil {
		return err
	}
	return e.JSON(http.StatusOK, map[string]bool{"ok": true})
}

// pushHeartbeat handles incoming push monitor heartbeats (unauthenticated).
func (h *Hub) pushHeartbeat(e *core.RequestEvent) error {
	token := e.Request.PathValue("pushToken")
	monitor, err := h.FindFirstRecordByFilter(
		"monitors",
		"type = 'push' && push_token = {:token} && active = true",
		dbx.Params{"token": token},
	)
	if err != nil {
		// Don't reveal whether the token exists
		return e.JSON(http.StatusOK, map[string]string{"msg": "ok"})
	}
	monitor.Set("last_push_at", time.Now())
	_ = h.SaveNoValidate(monitor)
	return e.JSON(http.StatusOK, map[string]string{"msg": "ok"})
}

func applyMonitorFields(rec *core.Record, body map[string]any) {
	fields := []string{
		"name", "type", "group", "active", "interval", "timeout",
		"url", "http_method", "http_accepted_codes", "keyword", "keyword_invert",
		"hostname", "port", "dns_host", "dns_type", "dns_server", "push_token",
		"ping_count", "ping_per_request_timeout", "ping_ip_family",
		"failure_threshold",
	}
	for _, f := range fields {
		if v, ok := body[f]; ok {
			rec.Set(f, v)
		}
	}
}
