//go:build testing

package hub

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// seedMonitor creates a monitor plus monitor_events (status at the given hour offsets).
func seedMonitor(t *testing.T, hub *Hub, name, monitorType string, offsetsStatuses map[time.Duration]int) string {
	t.Helper()
	mon, err := createTestRecord(hub, "monitors", map[string]any{
		"name":   name,
		"type":   monitorType,
		"active": false, // inactive: the scheduler won't add stray events
	})
	require.NoError(t, err)
	now := time.Now().UTC()
	for offset, status := range offsetsStatuses {
		_, err := createTestRecord(hub, "monitor_events", map[string]any{
			"monitor":    mon.Id,
			"status":     status,
			"latency_ms": int64(42),
			"checked_at": now.Add(offset),
		})
		require.NoError(t, err)
	}
	return mon.Id
}

// findMonitor flattens the grouped response and returns the monitor record by id.
func findMonitor(groups []*MonitorGroupResponse, id string) (MonitorRecord, bool) {
	for _, g := range groups {
		for _, m := range g.Monitors {
			if m.ID == id {
				return m, true
			}
		}
	}
	return MonitorRecord{}, false
}

func TestMonitorStatsCache_ServesAggregates(t *testing.T) {
	hub, testApp, err := createTestHub(t)
	require.NoError(t, err)
	defer cleanupTestHub(hub, testApp)

	id := seedMonitor(t, hub, "m-http", "http", map[time.Duration]int{-1 * time.Hour: 1})

	require.NoError(t, hub.refreshMonitorStats())
	resp, err := hub.buildMonitorsResponse()
	require.NoError(t, err)

	m, ok := findMonitor(resp, id)
	require.True(t, ok)
	require.NotNil(t, m.Uptime24h)
	require.InDelta(t, 100, *m.Uptime24h, 0.01)
	require.NotNil(t, m.Uptime30d)
	require.NotNil(t, m.AvgLatency24hMs)
	require.Len(t, m.RecentChecks, 1, "sparkline served from cache")
}

func TestMonitorStatsCache_ColdCacheComputes(t *testing.T) {
	hub, testApp, err := createTestHub(t)
	require.NoError(t, err)
	defer cleanupTestHub(hub, testApp)

	id := seedMonitor(t, hub, "m-cold", "http", map[time.Duration]int{-2 * time.Hour: 1})

	// No refresh first — buildMonitorsResponse must compute synchronously on the cold path.
	_, _, ok := hub.monitorStatsSnapshot()
	require.False(t, ok, "cache should be cold before first refresh")

	resp, err := hub.buildMonitorsResponse()
	require.NoError(t, err)
	m, found := findMonitor(resp, id)
	require.True(t, found)
	require.NotNil(t, m.Uptime24h, "cold cache should still yield stats")
}

func TestMonitorStatsCache_StatusStaysLive(t *testing.T) {
	hub, testApp, err := createTestHub(t)
	require.NoError(t, err)
	defer cleanupTestHub(hub, testApp)

	id := seedMonitor(t, hub, "m-live", "http", map[time.Duration]int{-1 * time.Hour: 1})
	require.NoError(t, hub.refreshMonitorStats())

	// Flip the monitor's current status WITHOUT refreshing the cache.
	rec, err := hub.FindRecordById("monitors", id)
	require.NoError(t, err)
	rec.Set("status", 0)
	require.NoError(t, hub.SaveNoValidate(rec))

	resp, err := hub.buildMonitorsResponse()
	require.NoError(t, err)
	m, ok := findMonitor(resp, id)
	require.True(t, ok)
	require.Equal(t, 0, m.Status, "status is read live from the record, not the cache")
	require.NotNil(t, m.Uptime24h, "aggregates still served from the (unrefreshed) cache")
}

func TestMonitorStatsCache_PushDoesNotMutateCache(t *testing.T) {
	hub, testApp, err := createTestHub(t)
	require.NoError(t, err)
	defer cleanupTestHub(hub, testApp)

	pushID := seedMonitor(t, hub, "m-push", "push", map[time.Duration]int{-30 * time.Minute: 1})
	httpID := seedMonitor(t, hub, "m-http2", "http", map[time.Duration]int{-30 * time.Minute: 1})
	require.NoError(t, hub.refreshMonitorStats())

	// First response: push latency must be nil, http latency present.
	resp, err := hub.buildMonitorsResponse()
	require.NoError(t, err)
	push, _ := findMonitor(resp, pushID)
	httpM, _ := findMonitor(resp, httpID)
	require.Nil(t, push.AvgLatency24hMs, "push monitors expose no latency")
	require.NotNil(t, httpM.AvgLatency24hMs)

	// The cached push entry must NOT have been mutated to nil by the first call —
	// otherwise a shared-cache data race / corruption. Its latency stays populated.
	metrics, _, ok := hub.monitorStatsSnapshot()
	require.True(t, ok)
	require.NotNil(t, metrics[pushID], "push monitor present in cache")
	require.NotNil(t, metrics[pushID].AvgLatency24hMs, "cache entry must not be mutated by the response builder")

	// Second response still correct (idempotent).
	resp2, err := hub.buildMonitorsResponse()
	require.NoError(t, err)
	push2, _ := findMonitor(resp2, pushID)
	require.Nil(t, push2.AvgLatency24hMs)
}
