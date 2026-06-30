package hub

import (
	"context"
	"log/slog"
	"sync"
	"time"
)

const (
	defaultMonitorStatsInterval = 30 * time.Second
	minMonitorStatsInterval     = 5 * time.Second
)

// monitorStatsCache holds the precomputed, periodically-refreshed aggregates for the
// monitors list: uptime 24h/30d, average 24h latency, and the recent-checks sparkline.
// Serving these from memory turns GET /api/app/monitors from a full 30-day monitor_events
// scan + one seek per monitor (run on every request, including the sidebar's frequent
// down-count refetch) into an O(M) in-memory merge. Live fields (status, last_checked_at,
// last_latency_ms, last_msg) are still read from the monitor record on each request, so the
// up/down state stays instant; only the historical aggregates lag by up to one refresh
// interval — fine for a dashboard.
type monitorStatsCache struct {
	mu           sync.RWMutex
	metrics      map[string]*MonitorMetrics
	recentChecks map[string][]MonitorCheckPoint
	computedAt   time.Time
	// coldMu serializes the synchronous cold-path compute so a burst of requests during
	// the boot window (before the ticker's first refresh) doesn't each run the full scan.
	coldMu sync.Mutex
}

// refreshMonitorStats recomputes the cache (one batched 30-day GROUP BY for every monitor's
// metrics + a recent-checks seek per monitor) and swaps the result in atomically.
func (h *Hub) refreshMonitorStats() error {
	metrics, err := h.loadAllMonitorMetrics()
	if err != nil {
		return err
	}
	monitors, err := h.FindRecordsByFilter("monitors", "", "", 0, 0)
	if err != nil {
		return err
	}
	recent := make(map[string][]MonitorCheckPoint, len(monitors))
	for _, m := range monitors {
		checks, err := h.loadRecentChecks(m, recentChecksLimit)
		if err != nil {
			// One monitor's sparkline failing must not drop the whole refresh.
			continue
		}
		recent[m.Id] = checks
	}

	h.monitorStats.mu.Lock()
	h.monitorStats.metrics = metrics
	h.monitorStats.recentChecks = recent
	h.monitorStats.computedAt = time.Now()
	h.monitorStats.mu.Unlock()
	return nil
}

// monitorStatsSnapshot returns the cached aggregates (read-only — callers must not mutate
// the returned maps or the *MonitorMetrics they point to) and whether the cache has been
// populated at least once.
func (h *Hub) monitorStatsSnapshot() (map[string]*MonitorMetrics, map[string][]MonitorCheckPoint, bool) {
	h.monitorStats.mu.RLock()
	defer h.monitorStats.mu.RUnlock()
	return h.monitorStats.metrics, h.monitorStats.recentChecks, !h.monitorStats.computedAt.IsZero()
}

// ensureMonitorStatsWarm computes the cache once if it is still cold, serializing
// concurrent cold-path callers so only one runs the expensive scan; the rest see the
// freshly-populated cache and return. Used by buildMonitorsResponse before the background
// ticker's first refresh has landed.
func (h *Hub) ensureMonitorStatsWarm() error {
	if _, _, ok := h.monitorStatsSnapshot(); ok {
		return nil
	}
	h.monitorStats.coldMu.Lock()
	defer h.monitorStats.coldMu.Unlock()
	if _, _, ok := h.monitorStatsSnapshot(); ok {
		return nil // another caller warmed it while we waited for the lock
	}
	return h.refreshMonitorStats()
}

// startMonitorStatsTicker keeps the cache warm: it computes once immediately (so the first
// request after boot is already served from cache) then refreshes on the interval.
func (h *Hub) startMonitorStatsTicker(ctx context.Context, interval time.Duration) {
	if err := h.refreshMonitorStats(); err != nil {
		slog.Warn("monitor stats: initial refresh failed", "err", err)
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := h.refreshMonitorStats(); err != nil {
				slog.Warn("monitor stats: refresh failed", "err", err)
			}
		}
	}
}

// parseMonitorStatsInterval reads MONITORS_STATS_INTERVAL from env (default 30s, min 5s).
func parseMonitorStatsInterval() time.Duration {
	return parseDurationEnv("MONITORS_STATS_INTERVAL", defaultMonitorStatsInterval, minMonitorStatsInterval)
}
