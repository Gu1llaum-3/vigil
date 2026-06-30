package hub

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"sort"
	"strconv"
	"time"

	"github.com/Gu1llaum-3/vigil/internal/common"
	"github.com/Gu1llaum-3/vigil/internal/hub/ws"
	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

const (
	hostMetricSamplesCollection   = "host_metric_samples"
	hostMetricCurrentCollection   = "host_metric_current"
	defaultMetricsInterval        = time.Minute
	minMetricsInterval            = 30 * time.Second
	hostMetricsRequestTimeout     = 10 * time.Second
	hostMetricsRetentionDays      = 7
	hostMetricsRetentionCronJobID = "vigilHostMetricRetention"
	hostMetricsRetentionCronExpr  = "15 0 * * *"
)

type HostOverviewRecord struct {
	ID       string   `json:"id"`
	Name     string   `json:"name"`
	Status   string   `json:"status"`
	LastSeen string   `json:"last_seen"`
	Version  string   `json:"version"`
	Tags     []string `json:"tags"`
	common.HostSnapshotResponse
	Metrics *common.HostMetricsResponse `json:"metrics,omitempty"`
}

func parseMetricsInterval() time.Duration {
	return parseDurationEnv("METRICS_INTERVAL", defaultMetricsInterval, minMetricsInterval)
}

func (h *Hub) startMetricsTicker(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			refreshed, failed := h.collectAllHostMetrics(ctx)
			slog.Info("Periodic metrics collection", "refreshed", refreshed, "failed", failed)
		}
	}
}

// agentLogName returns a human-readable label for an agent record (its
// hostname-derived "name"), falling back to the record id when no name is set yet.
func agentLogName(rec *core.Record) string {
	if name := rec.GetString("name"); name != "" {
		return name
	}
	return rec.Id
}

func (h *Hub) collectAllHostMetrics(ctx context.Context) (refreshed, failed int) {
	agents, err := h.FindRecordsByFilter("agents", "status = 'connected'", "", 0, 0)
	if err != nil {
		return 0, 0
	}

	type result struct{ ok bool }
	results := make(chan result, len(agents))
	for _, agent := range agents {
		agentID := agent.Id
		agentName := agentLogName(agent)
		connVal, ok := h.agentConns.Load(agentID)
		if !ok {
			results <- result{}
			continue
		}
		conn, ok := connVal.(*ws.WsConn)
		if !ok {
			results <- result{}
			continue
		}
		go func() {
			if err := h.collectAndPersistHostMetrics(ctx, agentID, conn); err != nil {
				slog.Warn("Metrics collection failed", "agent", agentName, "id", agentID, "err", err)
				results <- result{}
				return
			}
			if err := h.collectAndPersistContainerMetrics(ctx, agentID, conn); err != nil {
				slog.Warn("Container metrics collection failed", "agent", agentName, "id", agentID, "err", err)
				results <- result{}
				return
			}
			results <- result{ok: true}
		}()
	}

	for i := 0; i < cap(results); i++ {
		if (<-results).ok {
			refreshed++
		} else {
			failed++
		}
	}
	return refreshed, failed
}

func (h *Hub) collectAndPersistHostMetrics(ctx context.Context, agentID string, conn *ws.WsConn) error {
	metricsCtx, cancel := context.WithTimeout(ctx, hostMetricsRequestTimeout)
	defer cancel()
	metrics, err := conn.GetHostMetrics(metricsCtx)
	if err != nil {
		return err
	}
	h.persistHostMetrics(agentID, metrics)
	return nil
}

func (h *Hub) persistHostMetrics(agentID string, metrics common.HostMetricsResponse) {
	h.insertHostMetricSample(agentID, metrics)
	// Evaluate metric-threshold alerts before writing the current row, so the resulting
	// edge-trigger state (alert_tiers) is persisted in that same write — surviving a
	// restart without a second DB round-trip or a race on the current row. Direct call
	// (no DB hook on the high-frequency metric collections — see conventions doc).
	h.evaluateMetricAlerts(agentID, metrics)
	h.upsertHostMetricCurrent(agentID, metrics)
}

func (h *Hub) insertHostMetricSample(agentID string, metrics common.HostMetricsResponse) {
	col, err := h.FindCachedCollectionByNameOrId(hostMetricSamplesCollection)
	if err != nil {
		slog.Warn("host_metric_samples collection not found", "err", err)
		return
	}
	rec := core.NewRecord(col)
	applyHostMetricRecord(rec, agentID, metrics)
	if err := h.SaveNoValidate(rec); err != nil {
		slog.Warn("Failed to save host metric sample", "agent", agentID, "err", err)
	}
}

func (h *Hub) upsertHostMetricCurrent(agentID string, metrics common.HostMetricsResponse) {
	// Snapshot the current edge-trigger tiers so they are persisted alongside the
	// metrics in this single write (see persistHostMetrics).
	var tiers map[string]int
	if h.metricAlerts != nil {
		tiers = h.metricAlerts.snapshotTiers(agentID)
	}
	// Concurrent paths (connect-time collection + periodic ticker) can target the same
	// agent, so use the retry-on-conflict helper to keep the unique(agent) upsert safe.
	err := h.upsertByUnique(hostMetricCurrentCollection, "agent = {:agent}", dbx.Params{"agent": agentID}, func(rec *core.Record) {
		applyHostMetricRecord(rec, agentID, metrics)
		if tiers != nil {
			rec.Set("alert_tiers", tiers)
		}
	})
	if err != nil {
		slog.Warn("Failed to save current host metrics", "agent", agentID, "err", err)
	}
}

func applyHostMetricRecord(rec *core.Record, agentID string, metrics common.HostMetricsResponse) {
	collectedAt := metrics.CollectedAt
	if collectedAt == "" {
		collectedAt = time.Now().UTC().Format(time.RFC3339)
	}
	rec.Set("agent", agentID)
	rec.Set("cpu_percent", metrics.CPUPercent)
	rec.Set("memory_total_bytes", metrics.MemoryTotalBytes)
	rec.Set("memory_used_bytes", metrics.MemoryUsedBytes)
	rec.Set("memory_used_percent", metrics.MemoryUsedPercent)
	rec.Set("disk_total_bytes", metrics.DiskTotalBytes)
	rec.Set("disk_used_bytes", metrics.DiskUsedBytes)
	rec.Set("disk_used_percent", metrics.DiskUsedPercent)
	rec.Set("disk_max_used_percent", metrics.DiskMaxUsedPercent)
	rec.Set("network_rx_bps", metrics.NetworkRxBps)
	rec.Set("network_tx_bps", metrics.NetworkTxBps)
	rec.Set("load1", metrics.Load1)
	rec.Set("load5", metrics.Load5)
	rec.Set("load15", metrics.Load15)
	rec.Set("collected_at", collectedAt)
}

func hostMetricsFromRecord(rec *core.Record) common.HostMetricsResponse {
	metrics := common.HostMetricsResponse{
		CPUPercent:         numberAsFloat64(rec.Get("cpu_percent")),
		MemoryTotalBytes:   numberAsUint64(rec.Get("memory_total_bytes")),
		MemoryUsedBytes:    numberAsUint64(rec.Get("memory_used_bytes")),
		MemoryUsedPercent:  numberAsFloat64(rec.Get("memory_used_percent")),
		DiskTotalBytes:     numberAsUint64(rec.Get("disk_total_bytes")),
		DiskUsedBytes:      numberAsUint64(rec.Get("disk_used_bytes")),
		DiskUsedPercent:    numberAsFloat64(rec.Get("disk_used_percent")),
		DiskMaxUsedPercent: numberAsFloat64(rec.Get("disk_max_used_percent")),
		NetworkRxBps:       numberAsUint64(rec.Get("network_rx_bps")),
		NetworkTxBps:       numberAsUint64(rec.Get("network_tx_bps")),
		Load1:              numberAsFloat64(rec.Get("load1")),
		Load5:              numberAsFloat64(rec.Get("load5")),
		Load15:             numberAsFloat64(rec.Get("load15")),
	}
	if !rec.GetDateTime("collected_at").IsZero() {
		metrics.CollectedAt = rec.GetDateTime("collected_at").Time().UTC().Format(time.RFC3339)
	}
	return metrics
}

func numberAsFloat64(value any) float64 {
	switch v := value.(type) {
	case float64:
		return v
	case float32:
		return float64(v)
	case int:
		return float64(v)
	case int64:
		return float64(v)
	case uint64:
		return float64(v)
	case uint:
		return float64(v)
	case string:
		f, _ := strconv.ParseFloat(v, 64)
		return f
	default:
		return 0
	}
}

func numberAsUint64(value any) uint64 {
	switch v := value.(type) {
	case uint64:
		return v
	case uint:
		return uint64(v)
	case int:
		if v > 0 {
			return uint64(v)
		}
	case int64:
		if v > 0 {
			return uint64(v)
		}
	case float64:
		if v > 0 {
			return uint64(v)
		}
	case float32:
		if v > 0 {
			return uint64(v)
		}
	case string:
		n, _ := strconv.ParseUint(v, 10, 64)
		return n
	}
	return 0
}

func (h *Hub) loadCurrentHostMetricsByAgent() (map[string]common.HostMetricsResponse, error) {
	records, err := h.FindAllRecords(hostMetricCurrentCollection)
	if err != nil {
		return nil, err
	}
	result := make(map[string]common.HostMetricsResponse, len(records))
	for _, rec := range records {
		agentID := rec.GetString("agent")
		if agentID == "" {
			continue
		}
		result[agentID] = hostMetricsFromRecord(rec)
	}
	return result, nil
}

// buildHostOverviewRecord assembles a host overview record from an agent record and its
// optional snapshot and current metrics. Shared by the fleet overview and the single-host
// detail endpoint so both produce an identical shape.
func buildHostOverviewRecord(agent *core.Record, snapshot *common.HostSnapshotResponse, metrics *common.HostMetricsResponse) HostOverviewRecord {
	host := HostOverviewRecord{
		ID:       agent.Id,
		Name:     agent.GetString("name"),
		Status:   agent.GetString("status"),
		LastSeen: agent.GetDateTime("last_seen").String(),
		Version:  agent.GetString("version"),
		// GetStringSlice always returns a non-nil slice (empty for missing/empty JSON),
		// so tags serialize as [] not null without an explicit guard.
		Tags: agent.GetStringSlice("tags"),
	}
	if snapshot != nil {
		host.HostSnapshotResponse = *snapshot
	}
	if metrics != nil {
		host.Metrics = metrics
	}
	return host
}

func (h *Hub) loadHostsOverview() ([]HostOverviewRecord, error) {
	agentRecords, err := h.FindAllRecords("agents")
	if err != nil {
		return nil, err
	}
	snapshotRecords, err := h.FindAllRecords("host_snapshots")
	if err != nil {
		return nil, err
	}
	metricsByAgent, err := h.loadCurrentHostMetricsByAgent()
	if err != nil {
		return nil, err
	}

	snapshotsByAgent := make(map[string]common.HostSnapshotResponse, len(snapshotRecords))
	for _, rec := range snapshotRecords {
		agentID := rec.GetString("agent")
		if agentID == "" {
			continue
		}
		var snapshot common.HostSnapshotResponse
		if err := json.Unmarshal([]byte(rec.GetString("data")), &snapshot); err != nil {
			continue
		}
		snapshotsByAgent[agentID] = snapshot
	}

	hosts := make([]HostOverviewRecord, 0, len(agentRecords))
	for _, agent := range agentRecords {
		var snapshotPtr *common.HostSnapshotResponse
		if snapshot, ok := snapshotsByAgent[agent.Id]; ok {
			snapshotCopy := snapshot
			snapshotPtr = &snapshotCopy
		}
		var metricsPtr *common.HostMetricsResponse
		if metrics, ok := metricsByAgent[agent.Id]; ok {
			metricsCopy := metrics
			metricsPtr = &metricsCopy
		}
		hosts = append(hosts, buildHostOverviewRecord(agent, snapshotPtr, metricsPtr))
	}

	sort.SliceStable(hosts, func(i, j int) bool {
		left := hosts[i].Name
		if left == "" {
			left = hosts[i].Hostname
		}
		right := hosts[j].Name
		if right == "" {
			right = hosts[j].Hostname
		}
		return left < right
	})

	return hosts, nil
}

func (h *Hub) getHostsOverview(e *core.RequestEvent) error {
	hosts, err := h.loadHostsOverview()
	if err != nil {
		return e.InternalServerError("Internal server error", err)
	}
	return e.JSON(http.StatusOK, hosts)
}

func (h *Hub) getHostDetail(e *core.RequestEvent) error {
	rec, err := h.buildHostDetail(e.Request.PathValue("id"))
	if err != nil {
		return e.NotFoundError("Host not found", nil)
	}
	return e.JSON(http.StatusOK, rec)
}

// buildHostDetail assembles a host's overview record (agent identity + latest snapshot +
// latest metrics). Shared by the /hosts/{id} handler and the MCP get_host tool.
func (h *Hub) buildHostDetail(agentID string) (HostOverviewRecord, error) {
	agent, err := h.FindRecordById("agents", agentID)
	if err != nil {
		return HostOverviewRecord{}, err
	}

	var snapshotPtr *common.HostSnapshotResponse
	if rec, snapErr := h.FindFirstRecordByFilter("host_snapshots", "agent = {:agent}", dbx.Params{"agent": agentID}); snapErr == nil {
		var snapshot common.HostSnapshotResponse
		if json.Unmarshal([]byte(rec.GetString("data")), &snapshot) == nil {
			snapshotPtr = &snapshot
		}
	}

	var metricsPtr *common.HostMetricsResponse
	if rec, metErr := h.FindFirstRecordByFilter(hostMetricCurrentCollection, "agent = {:agent}", dbx.Params{"agent": agentID}); metErr == nil {
		metrics := hostMetricsFromRecord(rec)
		metricsPtr = &metrics
	}

	return buildHostOverviewRecord(agent, snapshotPtr, metricsPtr), nil
}

func (h *Hub) getHostMetricsHistory(e *core.RequestEvent) error {
	agentID := e.Request.PathValue("id")
	if _, err := h.FindRecordById("agents", agentID); err != nil {
		return e.NotFoundError("Host not found", nil)
	}
	since := time.Now().UTC().Add(-parseMetricsHistoryRange(e.Request.URL.Query().Get("range")))
	records, err := h.FindRecordsByFilter(
		hostMetricSamplesCollection,
		"agent = {:agent} && collected_at >= {:since}",
		"collected_at",
		0,
		0,
		dbx.Params{"agent": agentID, "since": since},
	)
	if err != nil {
		return e.InternalServerError("Internal server error", err)
	}
	history := make([]common.HostMetricsResponse, 0, len(records))
	for _, rec := range records {
		history = append(history, hostMetricsFromRecord(rec))
	}
	return e.JSON(http.StatusOK, history)
}

// fleetMetricFields maps a fleet-metrics metric key to the host_metric_samples
// column it reads. cpu/memory/disk are percentages; load is the raw 5-min load.
var fleetMetricFields = map[string]string{
	"cpu":    "cpu_percent",
	"memory": "memory_used_percent",
	"disk":   "disk_used_percent",
	"load":   "load5",
}

// FleetMetricPoint is one (time, value) sample for a host in a fleet-metrics series.
type FleetMetricPoint struct {
	CollectedAt string  `json:"collected_at"`
	Value       float64 `json:"value"`
}

// FleetMetricSeries is one host's time series for the requested metric.
type FleetMetricSeries struct {
	ID     string             `json:"id"`
	Name   string             `json:"name"`
	Points []FleetMetricPoint `json:"points"`
}

// getFleetMetrics returns every fleet metric's per-host history over the range in a single
// response keyed by metric (cpu/memory/disk/load). The aggregation is done **in SQL**
// (GROUP BY agent + time bucket), so a 7d range yields ~seriesTargetPoints points per host
// instead of one row per raw sample (~10k) — keeping the Metrics page fast at scale. The
// bucket width comes from the shared deriveBucketSeconds, so short ranges stay raw.
func (h *Hub) getFleetMetrics(e *core.RequestEvent) error {
	window := parseMetricsHistoryRange(e.Request.URL.Query().Get("range"))
	since := time.Now().UTC().Add(-window)
	bucketSeconds := deriveBucketSeconds(window, seriesTargetPoints)

	names := make(map[string]string)
	if agents, aerr := h.FindAllRecords("agents"); aerr == nil {
		for _, a := range agents {
			names[a.Id] = a.GetString("name")
		}
	} else {
		// Non-fatal: series fall back to the agent id for their display name.
		slog.Warn("fleet metrics: failed to load agent names", "err", aerr)
	}

	out, err := h.loadFleetMetricsSeries(since, bucketSeconds, names)
	if err != nil {
		return e.InternalServerError("Internal server error", err)
	}
	return e.JSON(http.StatusOK, out)
}

type fleetMetricsBucketRow struct {
	Agent       string          `db:"agent"`
	BucketStart int64           `db:"bucket_start"`
	AvgCPU      sql.NullFloat64 `db:"avg_cpu"`
	AvgMemory   sql.NullFloat64 `db:"avg_memory"`
	AvgDisk     sql.NullFloat64 `db:"avg_disk"`
	AvgLoad     sql.NullFloat64 `db:"avg_load"`
}

// loadFleetMetricsSeries aggregates host_metric_samples in SQL into per-(agent, time-bucket)
// averages for every metric, returning one series per host per metric (agents in
// first-appearance order). Only ~one row per bucket is materialized, not every raw sample.
func (h *Hub) loadFleetMetricsSeries(since time.Time, bucketSeconds int, names map[string]string) (map[string][]FleetMetricSeries, error) {
	if bucketSeconds < 1 {
		bucketSeconds = 60
	}
	var rows []fleetMetricsBucketRow
	err := h.DB().
		NewQuery(`SELECT
			agent,
			(CAST(strftime('%s', collected_at) AS INTEGER) / {:bucket}) * {:bucket} AS bucket_start,
			AVG(cpu_percent) AS avg_cpu,
			AVG(memory_used_percent) AS avg_memory,
			AVG(disk_used_percent) AS avg_disk,
			AVG(load5) AS avg_load
		FROM host_metric_samples
		WHERE collected_at >= {:since}
		GROUP BY agent, bucket_start
		ORDER BY agent, bucket_start`).
		Bind(dbx.Params{"since": since.UTC(), "bucket": bucketSeconds}).
		All(&rows)
	if err != nil {
		return nil, err
	}

	order := make([]string, 0)
	byAgent := make(map[string]map[string]*FleetMetricSeries)
	for _, r := range rows {
		if r.Agent == "" {
			continue
		}
		seriesByMetric, seen := byAgent[r.Agent]
		if !seen {
			name := names[r.Agent]
			if name == "" {
				name = r.Agent
			}
			seriesByMetric = make(map[string]*FleetMetricSeries, len(fleetMetricFields))
			for metric := range fleetMetricFields {
				seriesByMetric[metric] = &FleetMetricSeries{ID: r.Agent, Name: name, Points: []FleetMetricPoint{}}
			}
			byAgent[r.Agent] = seriesByMetric
			order = append(order, r.Agent)
		}
		at := time.Unix(r.BucketStart, 0).UTC().Format(time.RFC3339)
		appendPoint := func(metric string, v sql.NullFloat64) {
			// Skip a bucket whose metric is NULL (no samples carried that column) rather than
			// plotting a misleading 0 — the chart shows a gap instead of a false dip to zero.
			if !v.Valid {
				return
			}
			seriesByMetric[metric].Points = append(seriesByMetric[metric].Points, FleetMetricPoint{CollectedAt: at, Value: v.Float64})
		}
		appendPoint("cpu", r.AvgCPU)
		appendPoint("memory", r.AvgMemory)
		appendPoint("disk", r.AvgDisk)
		appendPoint("load", r.AvgLoad)
	}

	out := make(map[string][]FleetMetricSeries, len(fleetMetricFields))
	for metric := range fleetMetricFields {
		series := make([]FleetMetricSeries, 0, len(order))
		for _, agentID := range order {
			series = append(series, *byAgent[agentID][metric])
		}
		out[metric] = series
	}
	return out, nil
}

func parseMetricsHistoryRange(raw string) time.Duration {
	switch raw {
	case "1h":
		return time.Hour
	case "24h":
		return 24 * time.Hour
	case "7d":
		return 7 * 24 * time.Hour
	default:
		return 6 * time.Hour
	}
}

func (h *Hub) purgeHostMetricSamplesOlderThan(days int) (int, error) {
	if days <= 0 {
		return 0, fmt.Errorf("days must be greater than 0")
	}
	cutoff := time.Now().UTC().AddDate(0, 0, -days)
	params := dbx.Params{"cutoff": cutoff}
	count, err := countRows(h, "SELECT COUNT(*) AS count FROM host_metric_samples WHERE collected_at < {:cutoff}", params)
	if err != nil || count == 0 {
		return count, err
	}
	return count, deleteRows(h, "DELETE FROM host_metric_samples WHERE collected_at < {:cutoff}", params)
}
