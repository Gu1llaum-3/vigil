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

// TestValidateMetricAlertPayload locks the payload rules, including the new guard that
// an enabled alert must have at least one positive threshold (otherwise it is shown as
// active in the UI yet can never fire — a silent no-op).
func TestValidateMetricAlertPayload(t *testing.T) {
	ok := metricAlertPayload{Metric: "cpu", Enabled: true, WarningValue: 80, CriticalValue: 95, Hysteresis: 5}
	if err := validateMetricAlertPayload(ok); err != nil {
		t.Fatalf("valid payload rejected: %v", err)
	}
	// enabled but no threshold → rejected
	if err := validateMetricAlertPayload(metricAlertPayload{Metric: "cpu", Enabled: true}); err == nil {
		t.Fatal("enabled alert with warning=0 && critical=0 must be rejected")
	}
	// disabled with no threshold is fine (just a placeholder/mute row)
	if err := validateMetricAlertPayload(metricAlertPayload{Metric: "cpu", Enabled: false}); err != nil {
		t.Fatalf("disabled empty payload rejected: %v", err)
	}
	// unknown metric
	if err := validateMetricAlertPayload(metricAlertPayload{Metric: "gpu", Enabled: true, WarningValue: 1}); err == nil {
		t.Fatal("unknown metric must be rejected")
	}
	// warning > critical
	if err := validateMetricAlertPayload(metricAlertPayload{Metric: "cpu", Enabled: true, WarningValue: 95, CriticalValue: 80}); err == nil {
		t.Fatal("warning > critical must be rejected")
	}
	// hysteresis >= threshold
	if err := validateMetricAlertPayload(metricAlertPayload{Metric: "cpu", Enabled: true, WarningValue: 80, Hysteresis: 80}); err == nil {
		t.Fatal("hysteresis >= threshold must be rejected")
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
	if _, _, _, changed := hub.metricAlerts.transition(agentID, metricCPU, 50, th); !changed {
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

// TestEvaluateLoadavgRecoversAtZero drives evaluate() end-to-end and locks the #4
// refinement: loadavg is exempt from the exact-0 "no reading" guard, so a fired load
// alert on a host that goes genuinely idle (load 0.00) actually recovers instead of
// staying stuck. (A regression that reverts zeroIsMissing to skip all zeros would be
// caught here, not just by the unit tests.)
func TestEvaluateLoadavgRecoversAtZero(t *testing.T) {
	hub, testApp, err := createTestHub(t)
	if err != nil {
		t.Fatal(err)
	}
	defer cleanupTestHub(hub, testApp)

	const agentID = "agentload1"
	col, err := hub.FindCollectionByNameOrId(metricAlertsCollection)
	if err != nil {
		t.Fatalf("collection: %v", err)
	}
	rec := core.NewRecord(col)
	rec.Set("metric", "loadavg")
	rec.Set("enabled", true)
	rec.Set("warning_value", 2)
	rec.Set("hysteresis", 0.5)
	if err := hub.Save(rec); err != nil {
		t.Fatalf("create loadavg alert: %v", err)
	}
	hub.metricAlerts.load()

	// Fire the warning tier.
	hub.metricAlerts.evaluate(agentID, common.HostMetricsResponse{Load1: 5})
	if got := hub.metricAlerts.snapshotTiers(agentID)["loadavg"]; got != int(tierWarning) {
		t.Fatalf("loadavg should have fired warning, got tier %d", got)
	}

	// Host goes genuinely idle: load 0.00 must recover, not be treated as "no reading".
	hub.metricAlerts.evaluate(agentID, common.HostMetricsResponse{Load1: 0})
	if _, stuck := hub.metricAlerts.snapshotTiers(agentID)["loadavg"]; stuck {
		t.Fatal("loadavg at 0 on an idle host must recover, not stay fired")
	}
}
