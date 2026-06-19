//go:build testing

package hub

import (
	"testing"

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
