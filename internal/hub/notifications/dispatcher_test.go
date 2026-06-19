//go:build testing

package notifications

import "testing"

// TestThrottleKind locks the per-metric throttle discriminator: two host metric
// events for the same agent but different metrics must NOT share a throttle key,
// otherwise one metric's breach would suppress another's notification.
func TestThrottleKind(t *testing.T) {
	cpuWarn := Event{Kind: EventHostMetricExceeded, Details: map[string]any{"metric": "cpu", "tier": "warning"}}
	cpuCrit := Event{Kind: EventHostMetricExceeded, Details: map[string]any{"metric": "cpu", "tier": "critical"}}
	disk := Event{Kind: EventHostMetricExceeded, Details: map[string]any{"metric": "disk", "tier": "warning"}}
	mon := Event{Kind: EventMonitorDown}

	// Different metrics on the same agent must not share a throttle key.
	if throttleKind(cpuWarn) == throttleKind(disk) {
		t.Fatalf("cpu and disk breaches must have distinct throttle keys, both = %q", throttleKind(cpuWarn))
	}
	// A warning→critical escalation of the same metric must not be throttled away.
	if throttleKind(cpuWarn) == throttleKind(cpuCrit) {
		t.Fatalf("warning and critical escalations must have distinct throttle keys, both = %q", throttleKind(cpuWarn))
	}
	if got, want := throttleKind(cpuWarn), "host.metric_exceeded:cpu:warning"; got != want {
		t.Fatalf("throttleKind(cpu/warning) = %q, want %q", got, want)
	}
	if got, want := throttleKind(mon), "monitor.down"; got != want {
		t.Fatalf("throttleKind(non-metric) = %q, want %q", got, want)
	}
}
