//go:build testing

package hub

import (
	"testing"

	"github.com/Gu1llaum-3/vigil/internal/common"
	"github.com/pocketbase/pocketbase/core"
)

// TestMetricAlertEmptyAgentUpsert guards the "Failed to save metric alert" 400
// seen when toggling a global alert: a PocketBase `agent = ""` filter does not
// match an empty relation, so the upsert must locate the global row by scanning.
// It also checks the full enable→disable cycle does not create a duplicate.
func TestMetricAlertEmptyAgentUpsert(t *testing.T) {
	hub, testApp, err := createTestHub(t)
	if err != nil {
		t.Fatal(err)
	}
	defer cleanupTestHub(hub, testApp)

	col, err := hub.FindCollectionByNameOrId(metricAlertsCollection)
	if err != nil {
		t.Fatalf("collection: %v", err)
	}
	rec := core.NewRecord(col)
	rec.Set("agent", "")
	rec.Set("metric", "cpu")
	rec.Set("enabled", true)
	rec.Set("hysteresis", 5)
	if err := hub.Save(rec); err != nil {
		t.Fatalf("create global alert failed: %v", err)
	}

	// The scan-based lookup must find the empty-agent (global) row.
	found := hub.findMetricAlertRecord("", "cpu")
	if found == nil {
		t.Fatal("findMetricAlertRecord did not find the global row (regression)")
	}
	if found.Id != rec.Id {
		t.Fatalf("found wrong row: %s != %s", found.Id, rec.Id)
	}

	// Disable must update the same row, not insert a duplicate.
	found.Set("enabled", false)
	if err := hub.Save(found); err != nil {
		t.Fatalf("disable update failed: %v", err)
	}
	all, _ := hub.FindAllRecords(metricAlertsCollection)
	if len(all) != 1 {
		t.Fatalf("expected 1 metric_alerts row after enable→disable, got %d", len(all))
	}
}

// TestEdgeStatePersistsAcrossReload is the #5 guard: the fired tier is written into
// host_metric_current.alert_tiers and restored by loadState, so a restart does not
// re-fire an already-active alert.
func TestEdgeStatePersistsAcrossReload(t *testing.T) {
	hub, testApp, err := createTestHub(t)
	if err != nil {
		t.Fatal(err)
	}
	defer cleanupTestHub(hub, testApp)

	const agentID = "agentpersist1"
	th := metricThreshold{enabled: true, warning: 10, hysteresis: 1}

	// Drive the in-memory state to a fired tier, then write the current row (which
	// folds the alert_tiers snapshot into the same write).
	if _, _, changed := hub.metricAlerts.transition(agentID, metricCPU, 50, th); !changed {
		t.Fatal("expected the metric to fire")
	}
	hub.upsertHostMetricCurrent(agentID, common.HostMetricsResponse{CPUPercent: 50})

	// Simulate a restart: a fresh evaluator must restore the tier from the DB.
	fresh := newMetricAlertEvaluator(hub)
	fresh.loadState()
	if got := fresh.snapshotTiers(agentID)["cpu"]; got != int(tierWarning) {
		t.Fatalf("edge state not restored after reload: got tier %d", got)
	}
}
