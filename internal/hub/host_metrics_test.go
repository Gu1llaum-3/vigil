//go:build testing

package hub

import (
	"testing"

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

// TestBuildFleetMetricSeries locks the fleet-metrics grouping: samples are grouped
// into one series per agent (first-appearance order), the requested field is read
// per point, and the agent id is used when no name is known.
func TestBuildFleetMetricSeries(t *testing.T) {
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
	// collected_at order: a1, a2, a1
	records := []*core.Record{
		mk(a1.Id, 10, "2026-01-01 00:00:00.000Z"),
		mk(a2.Id, 50, "2026-01-01 00:00:30.000Z"),
		mk(a1.Id, 20, "2026-01-01 00:01:00.000Z"),
	}

	series := buildFleetMetricSeries(records, "cpu_percent", map[string]string{a1.Id: "web-1", a2.Id: "web-2"})
	require.Len(t, series, 2)
	require.Equal(t, a1.Id, series[0].ID)
	require.Equal(t, "web-1", series[0].Name)
	require.Len(t, series[0].Points, 2)
	require.Equal(t, 10.0, series[0].Points[0].Value)
	require.Equal(t, 20.0, series[0].Points[1].Value)
	require.Equal(t, a2.Id, series[1].ID)
	require.Len(t, series[1].Points, 1)
	require.Equal(t, 50.0, series[1].Points[0].Value)

	// name falls back to the agent id when unknown
	fallback := buildFleetMetricSeries(records, "cpu_percent", map[string]string{})
	require.Equal(t, a1.Id, fallback[0].Name)
}
