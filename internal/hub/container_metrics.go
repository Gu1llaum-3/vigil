package hub

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/Gu1llaum-3/vigil/internal/common"
	"github.com/Gu1llaum-3/vigil/internal/hub/ws"
	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

const (
	containerMetricSamplesCollection   = "container_metric_samples"
	containerMetricsRequestTimeout     = 10 * time.Second
	containerMetricsRetentionDays      = 7
	containerMetricsRetentionCronJobID = "vigilContainerMetricRetention"
	containerMetricsRetentionCronExpr  = "20 0 * * *"
)

type ContainerMetricHistoryPoint struct {
	CollectedAt string                         `json:"collected_at"`
	Containers  []common.ContainerMetricsPoint `json:"containers"`
}

type ContainerMetricSeriesPoint struct {
	CollectedAt      string  `json:"collected_at"`
	CPUPercent       float64 `json:"cpu_percent"`
	MemoryUsedBytes  uint64  `json:"memory_used_bytes"`
	MemoryLimitBytes uint64  `json:"memory_limit_bytes"`
	NetworkRxBps     uint64  `json:"network_rx_bps"`
	NetworkTxBps     uint64  `json:"network_tx_bps"`
}

func matchContainerByName(points []common.ContainerMetricsPoint, name string) *common.ContainerMetricsPoint {
	if name == "" {
		return nil
	}
	trimmed := strings.TrimPrefix(name, "/")
	for i := range points {
		candidate := strings.TrimPrefix(points[i].Name, "/")
		if candidate == trimmed {
			return &points[i]
		}
	}
	return nil
}

func containerSeriesFromPoint(collectedAt string, point common.ContainerMetricsPoint) ContainerMetricSeriesPoint {
	return ContainerMetricSeriesPoint{
		CollectedAt:      collectedAt,
		CPUPercent:       point.CPUPercent,
		MemoryUsedBytes:  point.MemoryUsedBytes,
		MemoryLimitBytes: point.MemoryLimitBytes,
		NetworkRxBps:     point.NetworkRxBps,
		NetworkTxBps:     point.NetworkTxBps,
	}
}

func (h *Hub) collectAndPersistContainerMetrics(ctx context.Context, agentID string, conn *ws.WsConn) error {
	metricsCtx, cancel := context.WithTimeout(ctx, containerMetricsRequestTimeout)
	defer cancel()
	metrics, err := conn.GetContainerMetrics(metricsCtx)
	if err != nil {
		return err
	}
	h.insertContainerMetricSample(agentID, metrics)
	return nil
}

func (h *Hub) insertContainerMetricSample(agentID string, metrics common.ContainerMetricsSnapshotResponse) {
	col, err := h.FindCachedCollectionByNameOrId(containerMetricSamplesCollection)
	if err != nil {
		slog.Warn("container_metric_samples collection not found", "err", err)
		return
	}
	dataBytes, err := json.Marshal(metrics.Containers)
	if err != nil {
		slog.Warn("Failed to marshal container metrics", "agent", agentID, "err", err)
		return
	}
	collectedAt := metrics.CollectedAt
	if collectedAt == "" {
		collectedAt = time.Now().UTC().Format(time.RFC3339)
	}
	rec := core.NewRecord(col)
	rec.Set("agent", agentID)
	rec.Set("data", string(dataBytes))
	rec.Set("collected_at", collectedAt)
	if err := h.SaveNoValidate(rec); err != nil {
		slog.Warn("Failed to save container metric sample", "agent", agentID, "err", err)
	}
}

func containerMetricHistoryPointFromRecord(rec *core.Record) ContainerMetricHistoryPoint {
	point := ContainerMetricHistoryPoint{}
	if !rec.GetDateTime("collected_at").IsZero() {
		point.CollectedAt = rec.GetDateTime("collected_at").Time().UTC().Format(time.RFC3339)
	}
	if raw := rec.GetString("data"); raw != "" {
		_ = json.Unmarshal([]byte(raw), &point.Containers)
	}
	return point
}

func (h *Hub) getHostContainerMetricsHistory(e *core.RequestEvent) error {
	agentID := e.Request.PathValue("id")
	if _, err := h.FindRecordById("agents", agentID); err != nil {
		return e.NotFoundError("Host not found", nil)
	}
	since := time.Now().UTC().Add(-parseMetricsHistoryRange(e.Request.URL.Query().Get("range")))
	records, err := h.FindRecordsByFilter(
		containerMetricSamplesCollection,
		"agent = {:agent} && collected_at >= {:since}",
		"collected_at",
		0,
		0,
		dbx.Params{"agent": agentID, "since": since},
	)
	if err != nil {
		return e.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
	}
	history := make([]ContainerMetricHistoryPoint, 0, len(records))
	for _, rec := range records {
		history = append(history, containerMetricHistoryPointFromRecord(rec))
	}
	return e.JSON(http.StatusOK, history)
}

func (h *Hub) getContainerMetricsHistoryByName(e *core.RequestEvent) error {
	agentID := e.Request.PathValue("id")
	name := e.Request.PathValue("name")
	if _, err := h.FindRecordById("agents", agentID); err != nil {
		return e.NotFoundError("Host not found", nil)
	}
	since := time.Now().UTC().Add(-parseMetricsHistoryRange(e.Request.URL.Query().Get("range")))
	records, err := h.FindRecordsByFilter(
		containerMetricSamplesCollection,
		"agent = {:agent} && collected_at >= {:since}",
		"collected_at",
		0,
		0,
		dbx.Params{"agent": agentID, "since": since},
	)
	if err != nil {
		return e.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
	}
	series := make([]ContainerMetricSeriesPoint, 0, len(records))
	for _, rec := range records {
		point := containerMetricHistoryPointFromRecord(rec)
		if match := matchContainerByName(point.Containers, name); match != nil {
			series = append(series, containerSeriesFromPoint(point.CollectedAt, *match))
		}
	}
	return e.JSON(http.StatusOK, series)
}

func (h *Hub) getContainerMetricsLatestByName(e *core.RequestEvent) error {
	agentID := e.Request.PathValue("id")
	name := e.Request.PathValue("name")
	if _, err := h.FindRecordById("agents", agentID); err != nil {
		return e.NotFoundError("Host not found", nil)
	}
	records, err := h.FindRecordsByFilter(
		containerMetricSamplesCollection,
		"agent = {:agent}",
		"-collected_at",
		1,
		0,
		dbx.Params{"agent": agentID},
	)
	if err != nil {
		return e.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
	}
	if len(records) == 0 {
		return e.JSON(http.StatusOK, nil)
	}
	point := containerMetricHistoryPointFromRecord(records[0])
	match := matchContainerByName(point.Containers, name)
	if match == nil {
		return e.JSON(http.StatusOK, nil)
	}
	return e.JSON(http.StatusOK, containerSeriesFromPoint(point.CollectedAt, *match))
}

func (h *Hub) getHostContainerMetricsLatest(e *core.RequestEvent) error {
	agentID := e.Request.PathValue("id")
	if _, err := h.FindRecordById("agents", agentID); err != nil {
		return e.NotFoundError("Host not found", nil)
	}
	records, err := h.FindRecordsByFilter(
		containerMetricSamplesCollection,
		"agent = {:agent}",
		"-collected_at",
		1,
		0,
		dbx.Params{"agent": agentID},
	)
	if err != nil {
		return e.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
	}
	if len(records) == 0 {
		return e.JSON(http.StatusOK, ContainerMetricHistoryPoint{Containers: []common.ContainerMetricsPoint{}})
	}
	return e.JSON(http.StatusOK, containerMetricHistoryPointFromRecord(records[0]))
}

func (h *Hub) purgeContainerMetricSamplesOlderThan(days int) (int, error) {
	if days <= 0 {
		return 0, fmt.Errorf("days must be greater than 0")
	}
	cutoff := time.Now().UTC().AddDate(0, 0, -days)
	params := dbx.Params{"cutoff": cutoff}
	count, err := countRows(h, "SELECT COUNT(*) AS count FROM container_metric_samples WHERE collected_at < {:cutoff}", params)
	if err != nil || count == 0 {
		return count, err
	}
	return count, deleteRows(h, "DELETE FROM container_metric_samples WHERE collected_at < {:cutoff}", params)
}

func sortContainerMetricPoints(points []common.ContainerMetricsPoint) {
	sort.SliceStable(points, func(i, j int) bool {
		left := points[i].Name
		if left == "" {
			left = points[i].ID
		}
		right := points[j].Name
		if right == "" {
			right = points[j].ID
		}
		return left < right
	})
}
