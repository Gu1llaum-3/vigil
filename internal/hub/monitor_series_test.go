//go:build testing

package hub

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func seedMonitorWithEvents(t *testing.T, hub *Hub, events []struct {
	at      time.Time
	status  int
	latency int
}) string {
	t.Helper()
	mon, err := createTestRecord(hub, "monitors", map[string]any{"name": "m", "type": "http", "active": false})
	require.NoError(t, err)
	for _, ev := range events {
		_, err := createTestRecord(hub, "monitor_events", map[string]any{
			"monitor":    mon.Id,
			"status":     ev.status,
			"latency_ms": int64(ev.latency),
			"checked_at": ev.at.UTC(),
		})
		require.NoError(t, err)
	}
	return mon.Id
}

func TestMonitorEventsWindowSince(t *testing.T) {
	now := time.Date(2026, 1, 8, 12, 0, 0, 0, time.UTC)

	// range derives since server-side and takes precedence over a client since.
	got, err := monitorEventsWindowSince(now, "7d", "2020-01-01T00:00:00Z")
	require.NoError(t, err)
	require.NotNil(t, got)
	require.Equal(t, now.Add(-7*24*time.Hour), *got)

	// no range → client since is parsed and honored.
	got, err = monitorEventsWindowSince(now, "", "2026-01-01T00:00:00Z")
	require.NoError(t, err)
	require.NotNil(t, got)
	require.Equal(t, time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), *got)

	// neither → unbounded (nil).
	got, err = monitorEventsWindowSince(now, "", "")
	require.NoError(t, err)
	require.Nil(t, got)

	// invalid client since → error.
	_, err = monitorEventsWindowSince(now, "", "not-a-timestamp")
	require.Error(t, err)

	// unknown non-empty range → error (not a silent 24h default that would truncate a
	// caller's since-based query).
	_, err = monitorEventsWindowSince(now, "monthly", "2026-01-01T00:00:00Z")
	require.Error(t, err)
}

func TestLoadMonitorSeries_Buckets(t *testing.T) {
	hub, testApp, err := createTestHub(t)
	require.NoError(t, err)
	defer cleanupTestHub(hub, testApp)

	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	id := seedMonitorWithEvents(t, hub, []struct {
		at      time.Time
		status  int
		latency int
	}{
		{base, 1, 100},                       // bucket 00:00 (up)
		{base.Add(10 * time.Second), 1, 200}, // bucket 00:00 (up) → avg 150
		{base.Add(70 * time.Second), 0, 0},   // bucket 00:01 (down)
		{base.Add(130 * time.Second), 1, 50}, // bucket 00:02 (up)
	})

	// 60s buckets, window covering all three minutes.
	points, err := hub.loadMonitorSeries(id, base.Add(-time.Second), 60)
	require.NoError(t, err)
	require.Len(t, points, 3)
	require.Equal(t, 1, points[0].Status)
	require.Equal(t, 150, points[0].LatencyMs) // averaged over the two up checks
	require.Equal(t, 0, points[1].Status)      // bucket with a down check
	require.Equal(t, 1, points[2].Status)
	require.Equal(t, 50, points[2].LatencyMs)
}

func TestLoadMonitorMetricsExcludesPendingFromUptime(t *testing.T) {
	hub, testApp, err := createTestHub(t)
	require.NoError(t, err)
	defer cleanupTestHub(hub, testApp)

	now := time.Now().UTC()
	at := func(minAgo int) time.Time { return now.Add(-time.Duration(minAgo) * time.Minute) }
	// In the last 24h: 7 up, 2 down, 5 pending. Pending must NOT count toward the
	// denominator, so uptime = 7 / (7 + 2) = 77.78%, not 7 / 14.
	events := []struct {
		at      time.Time
		status  int
		latency int
	}{}
	for i := 0; i < 7; i++ {
		events = append(events, struct {
			at      time.Time
			status  int
			latency int
		}{at(i + 1), monitorStatusUp, 100})
	}
	for i := 0; i < 2; i++ {
		events = append(events, struct {
			at      time.Time
			status  int
			latency int
		}{at(i + 20), monitorStatusDown, 0})
	}
	for i := 0; i < 5; i++ {
		events = append(events, struct {
			at      time.Time
			status  int
			latency int
		}{at(i + 40), monitorStatusPending, 0})
	}
	id := seedMonitorWithEvents(t, hub, events)

	mon, err := hub.FindRecordById("monitors", id)
	require.NoError(t, err)
	metrics, err := hub.loadMonitorMetrics(mon)
	require.NoError(t, err)
	require.NotNil(t, metrics.Uptime24h)
	require.InDelta(t, 100.0*7.0/9.0, *metrics.Uptime24h, 0.01)
}

func TestLoadMonitorSeriesPendingBuckets(t *testing.T) {
	hub, testApp, err := createTestHub(t)
	require.NoError(t, err)
	defer cleanupTestHub(hub, testApp)

	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	id := seedMonitorWithEvents(t, hub, []struct {
		at      time.Time
		status  int
		latency int
	}{
		{base, monitorStatusUp, 100},                            // bucket 00:00 → up
		{base.Add(70 * time.Second), monitorStatusPending, 0},   // bucket 00:01 → pending only
		{base.Add(130 * time.Second), monitorStatusPending, 0},  // bucket 00:02 (pending) ...
		{base.Add(140 * time.Second), monitorStatusDown, 0},     // ... + down → down wins
	})

	points, err := hub.loadMonitorSeries(id, base.Add(-time.Second), 60)
	require.NoError(t, err)
	require.Len(t, points, 3)
	require.Equal(t, monitorStatusUp, points[0].Status)
	require.Equal(t, monitorStatusPending, points[1].Status, "a pending-only bucket is pending (amber)")
	require.Equal(t, monitorStatusDown, points[2].Status, "down wins over pending in the same bucket")
}

func TestLoadMonitorTransitions(t *testing.T) {
	hub, testApp, err := createTestHub(t)
	require.NoError(t, err)
	defer cleanupTestHub(hub, testApp)

	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	min := func(n int) time.Time { return base.Add(time.Duration(n) * time.Minute) }
	id := seedMonitorWithEvents(t, hub, []struct {
		at      time.Time
		status  int
		latency int
	}{
		{min(0), 1, 10}, // up   (baseline → emitted)
		{min(1), 1, 12}, // up   (no change)
		{min(2), 0, 0},  // down (transition)
		{min(3), 0, 0},  // down (no change)
		{min(4), 1, 11}, // up   (transition)
	})

	since := base.Add(-time.Second)
	transitions, err := hub.loadMonitorTransitions(id, &since, 0)
	require.NoError(t, err)
	// newest first: up@4, down@2, up@0
	require.Len(t, transitions, 3)
	require.Equal(t, 1, transitions[0].Status)
	require.Equal(t, 0, transitions[1].Status)
	require.Equal(t, 1, transitions[2].Status)

	// limit caps to the newest N
	limited, err := hub.loadMonitorTransitions(id, &since, 2)
	require.NoError(t, err)
	require.Len(t, limited, 2)
	require.Equal(t, 1, limited[0].Status)
	require.Equal(t, 0, limited[1].Status)
}

// TestLoadMonitorTransitionsExcludesPending locks that pending (sub-threshold) rows do not
// appear in — and do not create spurious transitions in — the up↔down incident list.
func TestLoadMonitorTransitionsExcludesPending(t *testing.T) {
	hub, testApp, err := createTestHub(t)
	require.NoError(t, err)
	defer cleanupTestHub(hub, testApp)

	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	min := func(n int) time.Time { return base.Add(time.Duration(n) * time.Minute) }
	id := seedMonitorWithEvents(t, hub, []struct {
		at      time.Time
		status  int
		latency int
	}{
		{min(0), monitorStatusUp, 10},      // up (baseline)
		{min(1), monitorStatusPending, 0},  // pending (must be ignored)
		{min(2), monitorStatusPending, 0},  // pending (must be ignored)
		{min(3), monitorStatusDown, 0},     // down (transition up→down)
		{min(4), monitorStatusUp, 11},      // up (transition down→up)
	})

	since := base.Add(-time.Second)
	transitions, err := hub.loadMonitorTransitions(id, &since, 0)
	require.NoError(t, err)
	// Only real up↔down changes, pending elided: newest first up@4, down@3, up@0.
	require.Len(t, transitions, 3)
	require.Equal(t, monitorStatusUp, transitions[0].Status)
	require.Equal(t, monitorStatusDown, transitions[1].Status)
	require.Equal(t, monitorStatusUp, transitions[2].Status)
	for _, tr := range transitions {
		require.NotEqual(t, monitorStatusPending, tr.Status)
	}
}
