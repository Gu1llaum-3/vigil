package hub

import (
	"database/sql"
	"fmt"
	"math"
	"net/http"
	"strconv"
	"time"

	"github.com/google/uuid"
	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/types"
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
	Inverted              bool                `json:"inverted,omitempty"`
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
	Events24h       int     `db:"events_24h"`
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
	COALESCE(SUM(CASE WHEN status IN (0, 1) THEN 1 ELSE 0 END), 0) AS total_30d,
	COALESCE(SUM(CASE WHEN checked_at >= {:since24} THEN 1 ELSE 0 END), 0) AS events_24h,
	COALESCE(SUM(CASE WHEN checked_at >= {:since24} AND status IN (0, 1) THEN 1 ELSE 0 END), 0) AS total_24h,
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
	// Latency presence is gated on *any* events in the window, independent of the uptime
	// denominator (which excludes pending/unknown), so a monitor doesn't lose its latency
	// reading just because its only recent checks were pending.
	if row.Events24h > 0 && m.GetString("type") != "push" {
		avg := row.AvgLatency24hMs
		metrics.AvgLatency24hMs = &avg
	}
	if row.Total24h > 0 {
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

type monitorMetricsRow struct {
	Monitor         string  `db:"monitor"`
	AvgLatency24hMs float64 `db:"avg_latency_24h_ms"`
	Events24h       int     `db:"events_24h"`
	Total24h        int     `db:"total_24h"`
	Up24h           int     `db:"up_24h"`
	Total30d        int     `db:"total_30d"`
	Up30d           int     `db:"up_30d"`
}

// loadAllMonitorMetrics computes the 24h/30d metrics for every monitor in a single
// GROUP BY aggregate instead of one query per monitor, so the monitors list cost no
// longer scales with the number of monitors. The push-type latency guard is applied by
// the caller (the type lives on the monitor record, not the events). Latency is left as
// set here; callers nil it for push monitors.
func (h *Hub) loadAllMonitorMetrics() (map[string]*MonitorMetrics, error) {
	now := time.Now()
	var rows []monitorMetricsRow
	query := `
SELECT
	monitor,
	COALESCE(SUM(CASE WHEN status IN (0, 1) THEN 1 ELSE 0 END), 0) AS total_30d,
	COALESCE(SUM(CASE WHEN checked_at >= {:since24} THEN 1 ELSE 0 END), 0) AS events_24h,
	COALESCE(SUM(CASE WHEN checked_at >= {:since24} AND status IN (0, 1) THEN 1 ELSE 0 END), 0) AS total_24h,
	COALESCE(SUM(CASE WHEN checked_at >= {:since24} AND status = 1 THEN 1 ELSE 0 END), 0) AS up_24h,
	COALESCE(SUM(CASE WHEN status = 1 THEN 1 ELSE 0 END), 0) AS up_30d,
	COALESCE(AVG(CASE WHEN checked_at >= {:since24} THEN latency_ms END), 0) AS avg_latency_24h_ms
FROM monitor_events
WHERE checked_at >= {:since30}
GROUP BY monitor`
	if err := h.DB().NewQuery(query).Bind(dbx.Params{
		"since24": now.Add(-24 * time.Hour).UTC(),
		"since30": now.Add(-30 * 24 * time.Hour).UTC(),
	}).All(&rows); err != nil {
		return nil, err
	}

	result := make(map[string]*MonitorMetrics, len(rows))
	for _, row := range rows {
		metrics := &MonitorMetrics{}
		// Latency presence is gated on any events in the window (independent of the uptime
		// denominator, which excludes pending/unknown). The caller nils latency for push monitors.
		if row.Events24h > 0 {
			avg := row.AvgLatency24hMs
			metrics.AvgLatency24hMs = &avg
		}
		if row.Total24h > 0 {
			uptime24 := float64(row.Up24h) / float64(row.Total24h) * 100
			metrics.Uptime24h = &uptime24
		}
		if row.Total30d > 0 {
			uptime30 := float64(row.Up30d) / float64(row.Total30d) * 100
			metrics.Uptime30d = &uptime30
		}
		result[row.Monitor] = metrics
	}
	return result, nil
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
		Inverted:      m.GetBool("inverted"),
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

// recentChecksLimit is how many recent check points each monitor exposes (sparkline depth).
const recentChecksLimit = 10

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
	rec, err := h.buildMonitorDetail(e.Request.PathValue("id"))
	if err != nil {
		return e.NotFoundError("Monitor not found", nil)
	}
	return e.JSON(http.StatusOK, rec)
}

// buildMonitorDetail returns a single monitor with its current status, metrics and recent
// checks. Shared by the /monitors/{id} handler and the MCP get_monitor tool.
func (h *Hub) buildMonitorDetail(id string) (MonitorRecord, error) {
	rec, err := h.FindRecordById("monitors", id)
	if err != nil {
		return MonitorRecord{}, err
	}
	metrics, _ := h.loadMonitorMetrics(rec)
	recentChecks, _ := h.loadRecentChecks(rec, 10)
	return monitorToRecord(rec, h.Settings().Meta.AppURL, metrics, recentChecks), nil
}

// getMonitors returns all groups with their monitors.
func (h *Hub) getMonitors(e *core.RequestEvent) error {
	result, err := h.buildMonitorsResponse()
	if err != nil {
		return err
	}
	return e.JSON(http.StatusOK, result)
}

// buildMonitorsResponse builds all groups with their monitors and aggregated metrics.
// Shared by the /monitors handler and the MCP list_monitors tool.
func (h *Hub) buildMonitorsResponse() ([]*MonitorGroupResponse, error) {
	groups, err := h.FindRecordsByFilter("monitor_groups", "", "weight,name", 0, 0)
	if err != nil {
		return nil, err
	}
	monitors, err := h.FindRecordsByFilter("monitors", "", "name", 0, 0)
	if err != nil {
		return nil, err
	}

	// Aggregated metrics + recent-checks come from the in-memory cache (refreshed in the
	// background), so this endpoint no longer scans monitor_events on every request. On a
	// cold cache (e.g. right after boot, before the first refresh), compute once synchronously
	// so the first response still carries stats.
	metricsByMonitor, recentByMonitor, ok := h.monitorStatsSnapshot()
	if !ok {
		if err := h.ensureMonitorStatsWarm(); err != nil {
			return nil, err
		}
		metricsByMonitor, recentByMonitor, _ = h.monitorStatsSnapshot()
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
		metrics := metricsByMonitor[m.Id]
		if metrics != nil && m.GetString("type") == "push" {
			// Copy before nil-ing latency: the cached *MonitorMetrics is shared (read-only),
			// mutating it in place would corrupt the cache and race other readers.
			cp := *metrics
			cp.AvgLatency24hMs = nil
			metrics = &cp
		}
		// recent_checks (sparkline) comes from the cache too — no per-monitor seek here.
		recentChecks := recentByMonitor[m.Id]
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

	return result, nil
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

// MonitorEventEntry is a single monitor check result in the event history.
type MonitorEventEntry struct {
	ID        string `json:"id"`
	Status    int    `json:"status"`
	LatencyMs int    `json:"latency_ms"`
	Msg       string `json:"msg"`
	CheckedAt string `json:"checked_at"`
}

// loadMonitorEvents returns up to limit recent events for a monitor, newest first,
// optionally bounded by since/until. Shared by the /monitors/{id}/events handler and the
// MCP monitor_events tool.
func (h *Hub) loadMonitorEvents(id string, limit int, since, until *time.Time) ([]MonitorEventEntry, error) {
	filter := "monitor = {:id}"
	params := dbx.Params{"id": id}
	if since != nil {
		filter += " && checked_at >= {:since}"
		params["since"] = since.UTC()
	}
	if until != nil {
		filter += " && checked_at <= {:until}"
		params["until"] = until.UTC()
	}
	events, err := h.FindRecordsByFilter("monitor_events", filter, "-checked_at", limit, 0, params)
	if err != nil {
		return nil, err
	}
	result := make([]MonitorEventEntry, 0, len(events))
	for _, ev := range events {
		entry := MonitorEventEntry{
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
	return result, nil
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

	sincePtr, err := monitorEventsWindowSince(time.Now().UTC(), query.Get("range"), query.Get("since"))
	if err != nil {
		return e.BadRequestError("Invalid time window", err)
	}
	var untilPtr *time.Time
	if rawUntil := query.Get("until"); rawUntil != "" {
		until, err := time.Parse(time.RFC3339, rawUntil)
		if err != nil {
			return e.BadRequestError("Invalid until timestamp", err)
		}
		untilPtr = &until
	}

	// transitions_only returns just the status-change events (Uptime-Kuma-style incident
	// history), computed server-side so the payload stays small over long windows.
	if query.Get("transitions_only") == "true" {
		transitions, err := h.loadMonitorTransitions(id, sincePtr, limit)
		if err != nil {
			return err
		}
		return e.JSON(http.StatusOK, transitions)
	}

	result, err := h.loadMonitorEvents(id, limit, sincePtr, untilPtr)
	if err != nil {
		return err
	}
	return e.JSON(http.StatusOK, result)
}

// monitorEventsWindowSince resolves the `since` lower bound for the monitor events /
// transitions endpoint. A `range` (1h/3h/6h/24h/7d) makes the SERVER the single clock
// authority for the window and takes precedence over a client-supplied `since` — so the
// detail chart and the transitions list cover exactly the same period (the series endpoint
// derives `since` the same way) instead of drifting by client/server clock skew. An unknown
// non-empty `range` is rejected (rather than silently coerced to 24h, which would truncate
// an external caller's `since`-based query without warning). Returns nil when neither is
// given (unbounded history, capped by limit).
func monitorEventsWindowSince(now time.Time, rangeParam, sinceParam string) (*time.Time, error) {
	if rangeParam != "" {
		window, ok := parseMonitorRange(rangeParam)
		if !ok {
			return nil, fmt.Errorf("invalid range %q", rangeParam)
		}
		since := now.Add(-window).UTC()
		return &since, nil
	}
	if sinceParam != "" {
		since, err := time.Parse(time.RFC3339, sinceParam)
		if err != nil {
			return nil, err
		}
		since = since.UTC()
		return &since, nil
	}
	return nil, nil
}

// parseMonitorRange maps a range key to its window, reporting whether the key is known.
// ok=false carries the 24h default so callers that want lenient behavior (the series
// endpoint, whose range always comes from our own UI) can ignore ok, while the events
// endpoint can reject an unknown range.
func parseMonitorRange(raw string) (time.Duration, bool) {
	switch raw {
	case "1h":
		return time.Hour, true
	case "3h":
		return 3 * time.Hour, true
	case "6h":
		return 6 * time.Hour, true
	case "24h":
		return 24 * time.Hour, true
	case "7d":
		return 7 * 24 * time.Hour, true
	default:
		return 24 * time.Hour, false
	}
}

func parseMonitorRangeWindow(raw string) time.Duration {
	window, _ := parseMonitorRange(raw)
	return window
}

// seriesTargetPoints is the approximate number of points a downsampled chart series aims
// for; the bucket width is derived from the range to hit it. Shared by the monitor latency
// series and the fleet-metrics endpoint.
const seriesTargetPoints = 500

// deriveBucketSeconds picks a bucket width so a range yields ~targetPoints buckets. The
// floor is 1s (not 60s): for short ranges window/target is smaller than the sample interval,
// so each bucket holds ≤1 sample and the series is effectively raw — no spike-smoothing. Only
// long ranges (where window/target exceeds the interval) actually aggregate.
func deriveBucketSeconds(window time.Duration, targetPoints int) int {
	if targetPoints < 1 {
		targetPoints = 1
	}
	bucketSeconds := int(window.Seconds()) / targetPoints
	if bucketSeconds < 1 {
		bucketSeconds = 1
	}
	return bucketSeconds
}

// getMonitorSeries returns a downsampled latency series for the monitor over `range`,
// bucketed to ~seriesTargetPoints points — used by the detail chart for long ranges
// (e.g. 7d) where returning every raw check would be too many points to fetch and render.
func (h *Hub) getMonitorSeries(e *core.RequestEvent) error {
	id := e.Request.PathValue("id")
	window := parseMonitorRangeWindow(e.Request.URL.Query().Get("range"))
	since := time.Now().UTC().Add(-window)
	result, err := h.loadMonitorSeries(id, since, deriveBucketSeconds(window, seriesTargetPoints))
	if err != nil {
		return err
	}
	return e.JSON(http.StatusOK, result)
}

type monitorSeriesRow struct {
	BucketStart  int64           `db:"bucket_start"`
	AvgLatency   sql.NullFloat64 `db:"avg_latency"`
	DownCount    int             `db:"down_count"`
	PendingCount int             `db:"pending_count"`
}

// loadMonitorSeries returns a downsampled series for one monitor: events in [since, now]
// grouped into fixed buckets of bucketSeconds, aggregated **in SQL** so only one row per
// bucket (~monitorSeriesTargetPoints) is materialized, not every raw check. Each bucket
// yields one MonitorEventEntry the frontend's buildSeries consumes unchanged — latency =
// average over the bucket's up checks; bucket status follows the worst check in the bucket
// (down if ANY check was down → red band, else pending if ANY was pending → amber band,
// else up), so incidents stay visible on the downsampled chart.
func (h *Hub) loadMonitorSeries(id string, since time.Time, bucketSeconds int) ([]MonitorEventEntry, error) {
	if bucketSeconds < 1 {
		bucketSeconds = 60
	}
	var rows []monitorSeriesRow
	err := h.DB().
		NewQuery(`SELECT
			(CAST(strftime('%s', checked_at) AS INTEGER) / {:bucket}) * {:bucket} AS bucket_start,
			AVG(CASE WHEN status = 1 THEN latency_ms END) AS avg_latency,
			SUM(CASE WHEN status = 0 THEN 1 ELSE 0 END) AS down_count,
			SUM(CASE WHEN status = 2 THEN 1 ELSE 0 END) AS pending_count
		FROM monitor_events
		WHERE monitor = {:id} AND checked_at >= {:since}
		GROUP BY bucket_start
		ORDER BY bucket_start`).
		Bind(dbx.Params{"id": id, "since": since.UTC(), "bucket": bucketSeconds}).
		All(&rows)
	if err != nil {
		return nil, err
	}

	out := make([]MonitorEventEntry, 0, len(rows))
	for _, r := range rows {
		status, latency := 1, 0
		if r.DownCount > 0 {
			status = monitorStatusDown
		} else if r.PendingCount > 0 {
			status = monitorStatusPending
		}
		if r.AvgLatency.Valid {
			latency = int(math.Round(r.AvgLatency.Float64))
		}
		out = append(out, MonitorEventEntry{
			Status:    status,
			LatencyMs: latency,
			CheckedAt: time.Unix(r.BucketStart, 0).UTC().Format(time.RFC3339),
		})
	}
	return out, nil
}

type monitorTransitionRow struct {
	ID        string         `db:"id"`
	CheckedAt types.DateTime `db:"checked_at"`
	Status    int            `db:"status"`
	LatencyMs int            `db:"latency_ms"`
	Msg       string         `db:"msg"`
}

// loadMonitorTransitions returns only the real up↔down status-change events for a monitor in
// [since, now], newest first (capped at limit; limit<=0 = no cap). Change detection runs **in
// SQL** via a LAG window function, so only transition rows are materialized in Go — bounded
// regardless of how many raw checks fall in the window. Pending (sub-threshold) rows are
// excluded *before* the LAG so they neither create up→pending→up churn nor push real outages
// out of the capped incident list — pending is surfaced on the sparkline, not the incident log.
// The WHERE clause is built from constants only (the id/since/limit/pending are bound params),
// so it is injection-safe.
func (h *Hub) loadMonitorTransitions(id string, since *time.Time, limit int) ([]MonitorEventEntry, error) {
	where := "monitor = {:id} AND status != {:pending}"
	params := dbx.Params{"id": id, "pending": monitorStatusPending}
	if since != nil {
		where += " AND checked_at >= {:since}"
		params["since"] = since.UTC()
	}
	if limit <= 0 {
		limit = -1 // SQLite: LIMIT -1 means unbounded
	}
	params["limit"] = limit

	var rows []monitorTransitionRow
	err := h.DB().
		NewQuery(`SELECT id, status, latency_ms, msg, checked_at FROM (
			SELECT id, status, latency_ms, msg, checked_at,
			       LAG(status) OVER (ORDER BY checked_at) AS prev_status
			FROM monitor_events
			WHERE ` + where + `
		) WHERE prev_status IS NULL OR status != prev_status
		ORDER BY checked_at DESC
		LIMIT {:limit}`).
		Bind(params).
		All(&rows)
	if err != nil {
		return nil, err
	}

	out := make([]MonitorEventEntry, 0, len(rows))
	for _, r := range rows {
		out = append(out, MonitorEventEntry{
			ID:        r.ID,
			Status:    r.Status,
			LatencyMs: r.LatencyMs,
			Msg:       r.Msg,
			CheckedAt: r.CheckedAt.Time().UTC().Format(time.RFC3339),
		})
	}
	return out, nil
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
		"failure_threshold", "inverted",
	}
	for _, f := range fields {
		if v, ok := body[f]; ok {
			rec.Set(f, v)
		}
	}
}
