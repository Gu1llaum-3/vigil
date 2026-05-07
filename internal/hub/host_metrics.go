package hub

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"sort"
	"strconv"
	"time"

	"github.com/Gu1llaum-3/vigil/internal/common"
	"github.com/Gu1llaum-3/vigil/internal/hub/utils"
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
	ID       string `json:"id"`
	Name     string `json:"name"`
	Status   string `json:"status"`
	LastSeen string `json:"last_seen"`
	Version  string `json:"version"`
	common.HostSnapshotResponse
	Metrics *common.HostMetricsResponse `json:"metrics,omitempty"`
}

func parseMetricsInterval() time.Duration {
	raw, ok := utils.GetEnv("METRICS_INTERVAL")
	if !ok || raw == "" {
		return defaultMetricsInterval
	}
	d, err := time.ParseDuration(raw)
	if err != nil || d < minMetricsInterval {
		slog.Warn("Invalid METRICS_INTERVAL, using default", "value", raw, "default", defaultMetricsInterval)
		return defaultMetricsInterval
	}
	return d
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

func (h *Hub) collectAllHostMetrics(ctx context.Context) (refreshed, failed int) {
	agents, err := h.FindRecordsByFilter("agents", "status = 'connected'", "", 0, 0)
	if err != nil {
		return 0, 0
	}

	type result struct{ ok bool }
	results := make(chan result, len(agents))
	for _, agent := range agents {
		agentID := agent.Id
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
				slog.Warn("Metrics collection failed", "agent", agentID, "err", err)
				results <- result{}
				return
			}
			if err := h.collectAndPersistContainerMetrics(ctx, agentID, conn); err != nil {
				slog.Warn("Container metrics collection failed", "agent", agentID, "err", err)
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
	rec, err := h.FindFirstRecordByFilter(hostMetricCurrentCollection, "agent = {:agent}", dbx.Params{"agent": agentID})
	if err != nil {
		col, colErr := h.FindCachedCollectionByNameOrId(hostMetricCurrentCollection)
		if colErr != nil {
			slog.Warn("host_metric_current collection not found", "err", colErr)
			return
		}
		rec = core.NewRecord(col)
	}
	applyHostMetricRecord(rec, agentID, metrics)
	if err := h.SaveNoValidate(rec); err != nil {
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
	rec.Set("network_rx_bps", metrics.NetworkRxBps)
	rec.Set("network_tx_bps", metrics.NetworkTxBps)
	rec.Set("collected_at", collectedAt)
}

func hostMetricsFromRecord(rec *core.Record) common.HostMetricsResponse {
	metrics := common.HostMetricsResponse{
		CPUPercent:        numberAsFloat64(rec.Get("cpu_percent")),
		MemoryTotalBytes:  numberAsUint64(rec.Get("memory_total_bytes")),
		MemoryUsedBytes:   numberAsUint64(rec.Get("memory_used_bytes")),
		MemoryUsedPercent: numberAsFloat64(rec.Get("memory_used_percent")),
		DiskTotalBytes:    numberAsUint64(rec.Get("disk_total_bytes")),
		DiskUsedBytes:     numberAsUint64(rec.Get("disk_used_bytes")),
		DiskUsedPercent:   numberAsFloat64(rec.Get("disk_used_percent")),
		NetworkRxBps:      numberAsUint64(rec.Get("network_rx_bps")),
		NetworkTxBps:      numberAsUint64(rec.Get("network_tx_bps")),
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

	type agentMeta struct {
		name     string
		status   string
		lastSeen string
		version  string
	}
	agentsByID := make(map[string]agentMeta, len(agentRecords))
	for _, agent := range agentRecords {
		agentsByID[agent.Id] = agentMeta{
			name:     agent.GetString("name"),
			status:   agent.GetString("status"),
			lastSeen: agent.GetDateTime("last_seen").String(),
			version:  agent.GetString("version"),
		}
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
		meta := agentsByID[agent.Id]
		host := HostOverviewRecord{
			ID:       agent.Id,
			Name:     meta.name,
			Status:   meta.status,
			LastSeen: meta.lastSeen,
			Version:  meta.version,
		}
		if snapshot, ok := snapshotsByAgent[agent.Id]; ok {
			host.HostSnapshotResponse = snapshot
		}
		if metrics, ok := metricsByAgent[agent.Id]; ok {
			metricsCopy := metrics
			host.Metrics = &metricsCopy
		}
		hosts = append(hosts, host)
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
		return e.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
	}
	return e.JSON(http.StatusOK, hosts)
}

func (h *Hub) getHostDetail(e *core.RequestEvent) error {
	agentID := e.Request.PathValue("id")
	hosts, err := h.loadHostsOverview()
	if err != nil {
		return e.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
	}
	for _, host := range hosts {
		if host.ID == agentID {
			return e.JSON(http.StatusOK, host)
		}
	}
	return e.NotFoundError("Host not found", nil)
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
		return e.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
	}
	history := make([]common.HostMetricsResponse, 0, len(records))
	for _, rec := range records {
		history = append(history, hostMetricsFromRecord(rec))
	}
	return e.JSON(http.StatusOK, history)
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
