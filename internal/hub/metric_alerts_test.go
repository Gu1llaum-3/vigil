//go:build testing

package hub

import (
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

func hostMetrics(t *testing.T, cpu, mem, diskMax, diskRoot float64) common.HostMetricsResponse {
	t.Helper()
	return common.HostMetricsResponse{
		CPUPercent:         cpu,
		MemoryUsedPercent:  mem,
		DiskMaxUsedPercent: diskMax,
		DiskUsedPercent:    diskRoot,
	}
}
