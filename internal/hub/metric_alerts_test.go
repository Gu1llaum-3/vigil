//go:build testing

package hub

import (
	"sync"
	"sync/atomic"
	"testing"

	"github.com/Gu1llaum-3/vigil/internal/common"
	"github.com/Gu1llaum-3/vigil/internal/hub/notifications"
)

// TestComputeTierHysteresis locks the anti-flapping behaviour: a value hovering
// at the threshold must not bounce between fired and resolved.
func TestComputeTierHysteresis(t *testing.T) {
	th := metricThreshold{enabled: true, warning: 80, critical: 95, hysteresis: 5}

	cases := []struct {
		name  string
		prev  alertTier
		value float64
		want  alertTier
	}{
		{"below warning stays none", tierNone, 70, tierNone},
		{"cross warning fires warning", tierNone, 80, tierWarning},
		{"81 stays warning", tierWarning, 81, tierWarning},
		// the flapping scenario: 80 → 79 must NOT resolve (still within hysteresis band)
		{"79 holds warning (hysteresis)", tierWarning, 79, tierWarning},
		{"76 holds warning (hysteresis)", tierWarning, 76, tierWarning},
		{"clears only below warning-hysteresis", tierWarning, 74, tierNone},
		{"escalate to critical at 95", tierWarning, 95, tierCritical},
		{"93 holds critical (hysteresis)", tierCritical, 93, tierCritical},
		{"90 holds critical (= crit-hyst)", tierCritical, 90, tierCritical},
		{"89 downgrades to warning", tierCritical, 89, tierWarning},
		{"critical straight to none below warn-hyst", tierCritical, 74, tierNone},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := computeTier(tc.prev, tc.value, th); got != tc.want {
				t.Fatalf("computeTier(%v, %.0f) = %v, want %v", tc.prev, tc.value, got, tc.want)
			}
		})
	}
}

// TestComputeTierWarningOnly covers a threshold with only the warning tier set.
func TestComputeTierWarningOnly(t *testing.T) {
	th := metricThreshold{enabled: true, warning: 90, critical: 0, hysteresis: 5}
	if got := computeTier(tierNone, 92, th); got != tierWarning {
		t.Fatalf("expected warning, got %v", got)
	}
	if got := computeTier(tierWarning, 86, th); got != tierWarning {
		t.Fatalf("expected sticky warning at 86, got %v", got)
	}
	if got := computeTier(tierWarning, 84, th); got != tierNone {
		t.Fatalf("expected none at 84, got %v", got)
	}
}

// TestMetricValueDiskFallback verifies legacy agents (no max-disk field) fall back
// to root disk usage so disk alerts still work before agents are upgraded.
func TestMetricValueDiskFallback(t *testing.T) {
	// new agent: max field populated → used
	v, _ := metricValue(metricDisk, hostMetrics(t, 0, 0, 90, 50))
	if v != 90 {
		t.Fatalf("expected max disk 90, got %v", v)
	}
	// legacy agent: max field 0 → fall back to root
	v, _ = metricValue(metricDisk, hostMetrics(t, 0, 0, 0, 72))
	if v != 72 {
		t.Fatalf("expected fallback to root disk 72, got %v", v)
	}
}

// TestComputeTierNeverTrapsAlert is the #1 regression: when the stored hysteresis is
// ≥ the threshold (e.g. a loadavg row at warning 2 / hysteresis 5), the dead band must
// still be clamped above 0 so a fired alert can recover when the metric drops.
func TestComputeTierNeverTrapsAlert(t *testing.T) {
	th := metricThreshold{enabled: true, warning: 2, critical: 0, hysteresis: 5}
	if got := computeTier(tierNone, 3, th); got != tierWarning {
		t.Fatalf("expected warning at 3, got %v", got)
	}
	if got := computeTier(tierWarning, 0.1, th); got != tierNone {
		t.Fatalf("expected recovery near idle (0.1), got %v", got)
	}
}

// TestEvaluateIgnoresZeroReading is the #4 guard: for a metric where 0 is never a real
// reading (cpu/memory/disk), an exact-0 sample (failed/partial collection) must not be
// treated as a recovery; the current tier is kept. (loadavg is exempt — see
// TestZeroIsMissing — so it is not used here.)
func TestEvaluateIgnoresZeroReading(t *testing.T) {
	e := &metricAlertEvaluator{
		global:   map[metricKind]metricThreshold{metricCPU: {enabled: true, warning: 80, hysteresis: 5}},
		perAgent: map[string]map[metricKind]metricThreshold{},
		state:    map[string]map[metricKind]alertTier{"a1": {metricCPU: tierWarning}},
	}
	e.evaluate("a1", common.HostMetricsResponse{CPUPercent: 0})
	if got := e.state["a1"][metricCPU]; got != tierWarning {
		t.Fatalf("zero reading must keep the fired tier, got %v", got)
	}
}

// TestTransitionAtomic is the #7 race guard: concurrent transitions for the same
// (agent, metric) must report the escalation exactly once.
func TestTransitionAtomic(t *testing.T) {
	e := &metricAlertEvaluator{
		state: map[string]map[metricKind]alertTier{},
		peak:  map[string]map[metricKind]alertTier{},
	}
	th := metricThreshold{enabled: true, warning: 80, hysteresis: 5}
	var changes int64
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, _, _, changed := e.transition("a1", metricCPU, 90, th); changed {
				atomic.AddInt64(&changes, 1)
			}
		}()
	}
	wg.Wait()
	if changes != 1 {
		t.Fatalf("expected exactly 1 reported transition, got %d", changes)
	}
}

// TestMetricAlertEventSeverity locks the recovery-severity fix: a recovery event must
// carry the severity of the tier it clears (not the default "info"), so a rule with
// min_severity=warning still delivers the matching "back to normal". It also checks
// escalation severity, the silent critical→warning downgrade, and the disk mount label.
func TestMetricAlertEventSeverity(t *testing.T) {
	th := metricThreshold{enabled: true, warning: 80, critical: 95, hysteresis: 5}
	m := common.HostMetricsResponse{}

	// escalation none→warning
	if evt, ok := metricAlertEvent("a", "host", metricCPU, 82, "%", tierNone, tierWarning, tierWarning, th, 0, m); !ok ||
		evt.Kind != notifications.EventHostMetricExceeded || evt.Severity != "warning" {
		t.Fatalf("escalation→warning: ok=%v kind=%v sev=%q", ok, evt.Kind, evt.Severity)
	}
	// escalation warning→critical
	if evt, ok := metricAlertEvent("a", "host", metricCPU, 97, "%", tierWarning, tierCritical, tierCritical, th, 0, m); !ok || evt.Severity != "critical" {
		t.Fatalf("escalation→critical: ok=%v sev=%q", ok, evt.Severity)
	}
	// recovery from warning carries "warning"
	if evt, ok := metricAlertEvent("a", "host", metricCPU, 10, "%", tierWarning, tierNone, tierWarning, th, 0, m); !ok ||
		evt.Kind != notifications.EventHostMetricRecovered || evt.Severity != "warning" {
		t.Fatalf("recovery-from-warning: ok=%v kind=%v sev=%q", ok, evt.Kind, evt.Severity)
	}
	// recovery whose episode peaked at critical carries "critical" (even if the immediate
	// previous tier had been downgraded to warning)
	if evt, ok := metricAlertEvent("a", "host", metricCPU, 10, "%", tierWarning, tierNone, tierCritical, th, 0, m); !ok || evt.Severity != "critical" {
		t.Fatalf("recovery-peaked-critical: ok=%v sev=%q", ok, evt.Severity)
	}
	// downgrade critical→warning stays silent
	if _, ok := metricAlertEvent("a", "host", metricCPU, 88, "%", tierCritical, tierWarning, tierCritical, th, 0, m); ok {
		t.Fatal("critical→warning downgrade must not emit an event")
	}
	// disk escalation names the busiest mount
	dm := common.HostMetricsResponse{DiskMaxUsedPercent: 92, DiskMaxMount: "/data"}
	if evt, ok := metricAlertEvent("a", "host", metricDisk, 92, "%", tierNone, tierWarning, tierWarning, th, 0, dm); !ok || evt.Details["mount"] != "/data" {
		t.Fatalf("disk mount label: ok=%v mount=%v", ok, evt.Details["mount"])
	}
}

// TestTransitionTracksPeak locks option (a): after critical→warning (silent downgrade),
// the episode peak stays critical, so the eventual recovery is reported at critical and
// reaches a min_severity=critical rule.
func TestTransitionTracksPeak(t *testing.T) {
	e := &metricAlertEvaluator{
		state: map[string]map[metricKind]alertTier{},
		peak:  map[string]map[metricKind]alertTier{},
	}
	th := metricThreshold{enabled: true, warning: 80, critical: 95, hysteresis: 5}

	if _, next, _, _ := e.transition("a", metricCPU, 96, th); next != tierCritical {
		t.Fatalf("expected critical, got %v", next)
	}
	// drop into the warning band (below critical-hysteresis, above warning-hysteresis):
	// a silent downgrade to warning, but the episode peak must remain critical.
	if _, next, peak, _ := e.transition("a", metricCPU, 88, th); next != tierWarning || peak != tierCritical {
		t.Fatalf("downgrade: next=%v peak=%v (want warning/critical)", next, peak)
	}
	// full recovery reports the peak (critical), not the downgraded warning tier.
	prev, next, peak, changed := e.transition("a", metricCPU, 10, th)
	if !changed || next != tierNone || peak != tierCritical {
		t.Fatalf("recovery: changed=%v next=%v peak=%v (want true/normal/critical)", changed, next, peak)
	}
	evt, ok := metricAlertEvent("a", "host", metricCPU, 10, "%", prev, next, peak, th, 0, common.HostMetricsResponse{})
	if !ok || evt.Severity != "critical" {
		t.Fatalf("recovery event: ok=%v sev=%q (want critical)", ok, evt.Severity)
	}
	// the peak must be cleared after recovery so the next episode starts fresh.
	if _, _, peak, _ := e.transition("a", metricCPU, 82, th); peak != tierWarning {
		t.Fatalf("new episode peak=%v, want warning (peak not reset)", peak)
	}
}

// TestLoadPerCore locks the loadavg normalization: the same per-core threshold means
// the same thing on a 1-core and a 16-core host, and an unknown core count is reported.
func TestLoadPerCore(t *testing.T) {
	if v, ok := loadPerCore(8, 8); !ok || v != 1.0 {
		t.Fatalf("8 load / 8 cores = 1.0/core: got %v ok=%v", v, ok)
	}
	if v, ok := loadPerCore(1.5, 1); !ok || v != 1.5 {
		t.Fatalf("1.5 load / 1 core = 1.5/core: got %v ok=%v", v, ok)
	}
	if v, ok := loadPerCore(24, 16); !ok || v != 1.5 {
		t.Fatalf("24 load / 16 cores = 1.5/core: got %v ok=%v", v, ok)
	}
	if _, ok := loadPerCore(5, 0); ok {
		t.Fatal("unknown core count must report ok=false")
	}
}

// TestZeroIsMissing locks the #4 refinement: an exact-0 reading is treated as "not
// collected" for cpu/memory/disk (never genuinely 0 on a live host, and CPU reads 0
// on the first post-connect sample), but loadavg legitimately reads 0.00 when idle,
// so a real 0 there must be honored (otherwise a fired load alert never recovers).
func TestZeroIsMissing(t *testing.T) {
	for _, m := range []metricKind{metricCPU, metricMemory, metricDisk} {
		if !zeroIsMissing(m) {
			t.Fatalf("%s: exact 0 should be treated as missing", m)
		}
	}
	if zeroIsMissing(metricLoadAvg) {
		t.Fatal("loadavg: an exact 0 is a valid idle reading, must not be skipped")
	}
}

// TestThresholdForDisabledOverrideMutes is the #8 semantics: a disabled per-agent
// override mutes the metric for that host even when the global default is enabled.
func TestThresholdForDisabledOverrideMutes(t *testing.T) {
	e := &metricAlertEvaluator{
		global:   map[metricKind]metricThreshold{metricCPU: {enabled: true, warning: 80}},
		perAgent: map[string]map[metricKind]metricThreshold{"a1": {metricCPU: {enabled: false, warning: 80}}},
	}
	if _, ok := e.thresholdFor("a1", metricCPU); ok {
		t.Fatal("disabled per-agent override must mute the metric even when global is enabled")
	}
	if _, ok := e.thresholdFor("a2", metricCPU); !ok {
		t.Fatal("agent without override must inherit the enabled global default")
	}
}

func hostMetrics(t *testing.T, cpu, mem, diskMax, diskRoot float64) common.HostMetricsResponse {
	t.Helper()
	return common.HostMetricsResponse{
		CPUPercent:         cpu,
		MemoryUsedPercent:  mem,
		DiskMaxUsedPercent: diskMax,
		DiskUsedPercent:    diskRoot,
	}
}
