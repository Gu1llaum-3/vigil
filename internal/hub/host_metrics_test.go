//go:build testing

package hub

import (
	"testing"
	"time"

	"github.com/pocketbase/pocketbase/core"
	"github.com/stretchr/testify/require"
)

// TestHostOverviewIncludesTags locks the agent free-text tags round-trip: tags set
// on an agent record surface (verbatim) in the host overview/detail payload, and an
// agent without tags yields an empty slice (stable JSON, never null).
func TestHostOverviewIncludesTags(t *testing.T) {
	hub, testApp, err := createTestHub(t)
	require.NoError(t, err)
	defer cleanupTestHub(hub, testApp)

	tagged, err := createTestRecord(hub, "agents", map[string]any{
		"name":  "web-1",
		"token": "tok-web-1",
		"tags":  []string{"prod", "eu-west"},
	})
	require.NoError(t, err)

	// Re-fetch so we exercise the stored JSON round-trip, not just the in-memory set.
	fetched, err := hub.FindRecordById("agents", tagged.Id)
	require.NoError(t, err)
	require.Equal(t, []string{"prod", "eu-west"}, buildHostOverviewRecord(fetched, nil, nil).Tags)

	bare, err := createTestRecord(hub, "agents", map[string]any{
		"name":  "web-2",
		"token": "tok-web-2",
	})
	require.NoError(t, err)
	bareFetched, err := hub.FindRecordById("agents", bare.Id)
	require.NoError(t, err)
	require.Equal(t, []string{}, buildHostOverviewRecord(bareFetched, nil, nil).Tags)
}

// TestLoadFleetMetricsSeries locks the SQL-bucketed fleet aggregation: samples are grouped
// per (agent, time bucket) into one averaged point per bucket, for every metric, with the
// agent id used as the display name when none is known.
func TestLoadFleetMetricsSeries(t *testing.T) {
	hub, testApp, err := createTestHub(t)
	require.NoError(t, err)
	defer cleanupTestHub(hub, testApp)

	a1, err := createTestRecord(hub, "agents", map[string]any{"name": "web-1", "token": "t1"})
	require.NoError(t, err)
	a2, err := createTestRecord(hub, "agents", map[string]any{"name": "web-2", "token": "t2"})
	require.NoError(t, err)

	mk := func(agentID string, cpu float64, at string) *core.Record {
		rec, rerr := createTestRecord(hub, "host_metric_samples", map[string]any{
			"agent":        agentID,
			"cpu_percent":  cpu,
			"collected_at": at,
		})
		require.NoError(t, rerr)
		return rec
	}
	mk(a1.Id, 10, "2026-01-01 00:00:00.000Z") // a1, minute bucket 00:00
	mk(a2.Id, 50, "2026-01-01 00:00:30.000Z") // a2, minute bucket 00:00
	mk(a2.Id, 70, "2026-01-01 00:00:45.000Z") // a2, same bucket 00:00 → avg(50,70)=60
	mk(a1.Id, 20, "2026-01-01 00:01:00.000Z") // a1, minute bucket 00:01

	since := time.Date(2025, 12, 31, 0, 0, 0, 0, time.UTC)
	// index a metric's series by agent id (SQL orders by agent id, not insertion order).
	byID := func(series []FleetMetricSeries) map[string]FleetMetricSeries {
		m := make(map[string]FleetMetricSeries, len(series))
		for _, s := range series {
			m[s.ID] = s
		}
		return m
	}

	all, err := hub.loadFleetMetricsSeries(since, 60, map[string]string{a1.Id: "web-1", a2.Id: "web-2"})
	require.NoError(t, err)
	require.Len(t, all, 4)
	for _, metric := range []string{"cpu", "memory", "disk", "load"} {
		require.Contains(t, all, metric)
		require.Len(t, all[metric], 2)
	}
	cpu := byID(all["cpu"])
	require.Equal(t, "web-1", cpu[a1.Id].Name)
	require.Len(t, cpu[a1.Id].Points, 2) // two distinct minute buckets
	require.Equal(t, 10.0, cpu[a1.Id].Points[0].Value)
	require.Equal(t, 20.0, cpu[a1.Id].Points[1].Value)
	require.Len(t, cpu[a2.Id].Points, 1)
	require.Equal(t, 60.0, cpu[a2.Id].Points[0].Value) // two samples in one bucket → averaged

	// name falls back to the agent id when unknown
	fallback, err := hub.loadFleetMetricsSeries(since, 60, map[string]string{})
	require.NoError(t, err)
	require.Equal(t, a1.Id, byID(fallback["cpu"])[a1.Id].Name)
}
