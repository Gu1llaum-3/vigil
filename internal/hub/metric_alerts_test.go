//go:build testing

package hub

import (
	"sync"
	"sync/atomic"
	"testing"

	"github.com/Gu1llaum-3/vigil/internal/common"
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

// TestEvaluateIgnoresZeroReading is the #4 guard: an exact-0 reading (failed/partial
// collection) must not be treated as a recovery; the current tier is kept.
func TestEvaluateIgnoresZeroReading(t *testing.T) {
	e := &metricAlertEvaluator{
		global:   map[metricKind]metricThreshold{metricLoadAvg: {enabled: true, warning: 2, hysteresis: 0.5}},
		perAgent: map[string]map[metricKind]metricThreshold{},
		state:    map[string]map[metricKind]alertTier{"a1": {metricLoadAvg: tierWarning}},
	}
	e.evaluate("a1", common.HostMetricsResponse{Load1: 0})
	if got := e.state["a1"][metricLoadAvg]; got != tierWarning {
		t.Fatalf("zero reading must keep the fired tier, got %v", got)
	}
}

// TestTransitionAtomic is the #7 race guard: concurrent transitions for the same
// (agent, metric) must report the escalation exactly once.
func TestTransitionAtomic(t *testing.T) {
	e := &metricAlertEvaluator{state: map[string]map[metricKind]alertTier{}}
	th := metricThreshold{enabled: true, warning: 80, hysteresis: 5}
	var changes int64
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, _, changed := e.transition("a1", metricCPU, 90, th); changed {
				atomic.AddInt64(&changes, 1)
			}
		}()
	}
	wg.Wait()
	if changes != 1 {
		t.Fatalf("expected exactly 1 reported transition, got %d", changes)
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
