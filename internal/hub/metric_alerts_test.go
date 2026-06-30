//go:build testing

package hub

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"

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

// TestMetricValueLoadUsesLoad5 locks that loadavg alerting uses the 5-minute load (the
// industry-standard window for sustained-load alerts; less flappy than the 1-minute),
// not Load1.
func TestMetricValueLoadUsesLoad5(t *testing.T) {
	m := common.HostMetricsResponse{Load1: 9, Load5: 3, Load15: 1}
	if v, _ := metricValue(metricLoadAvg, m); v != 3 {
		t.Fatalf("loadavg metric must use Load5 (3), got %v", v)
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
			if _, _, _, changed := e.transition("a1", metricCPU, 90, th, time.Now()); changed {
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

	if _, next, _, _ := e.transition("a", metricCPU, 96, th, time.Now()); next != tierCritical {
		t.Fatalf("expected critical, got %v", next)
	}
	// drop into the warning band (below critical-hysteresis, above warning-hysteresis):
	// a silent downgrade to warning, but the episode peak must remain critical.
	if _, next, peak, _ := e.transition("a", metricCPU, 88, th, time.Now()); next != tierWarning || peak != tierCritical {
		t.Fatalf("downgrade: next=%v peak=%v (want warning/critical)", next, peak)
	}
	// full recovery reports the peak (critical), not the downgraded warning tier.
	prev, next, peak, changed := e.transition("a", metricCPU, 10, th, time.Now())
	if !changed || next != tierNone || peak != tierCritical {
		t.Fatalf("recovery: changed=%v next=%v peak=%v (want true/normal/critical)", changed, next, peak)
	}
	evt, ok := metricAlertEvent("a", "host", metricCPU, 10, "%", prev, next, peak, th, 0, common.HostMetricsResponse{})
	if !ok || evt.Severity != "critical" {
		t.Fatalf("recovery event: ok=%v sev=%q (want critical)", ok, evt.Severity)
	}
	// the peak must be cleared after recovery so the next episode starts fresh.
	if _, _, peak, _ := e.transition("a", metricCPU, 82, th, time.Now()); peak != tierWarning {
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

// TestGateEntry locks the sustained-"for" gating logic: only a cold-start breach (none →
// breach) is delayed; escalation, downgrade and recovery commit immediately; and a gap in
// observations larger than maxGap restarts the streak.
func TestGateEntry(t *testing.T) {
	base := time.Unix(1_700_000_000, 0)
	dur := 2 * time.Minute
	gap := 3 * time.Minute
	streak := func(since, last time.Time) pendingStreak { return pendingStreak{since: since, lastSeen: last} }

	// cold-start, no streak yet → start streak (since=lastSeen=now), do not fire
	if commit, s := gateEntry(tierNone, tierWarning, pendingStreak{}, base, dur, gap); commit != tierNone || s.since != base || s.lastSeen != base {
		t.Fatalf("cold-start begin: commit=%v streak=%+v", commit, s)
	}
	// cold-start, within the window → keep waiting, preserve since, advance lastSeen
	if commit, s := gateEntry(tierNone, tierWarning, streak(base, base), base.Add(time.Minute), dur, gap); commit != tierNone || s.since != base || s.lastSeen != base.Add(time.Minute) {
		t.Fatalf("within window: commit=%v streak=%+v", commit, s)
	}
	// cold-start, sustained past the window → fire, clear streak
	if commit, s := gateEntry(tierNone, tierWarning, streak(base, base.Add(time.Minute)), base.Add(dur), dur, gap); commit != tierWarning || !s.since.IsZero() {
		t.Fatalf("sustained: commit=%v streak=%+v", commit, s)
	}
	// gap larger than maxGap since lastSeen → restart streak (do NOT fire on a single late
	// sample, even though now-since would exceed the duration)
	if commit, s := gateEntry(tierNone, tierWarning, streak(base, base), base.Add(2*time.Hour), dur, gap); commit != tierNone || s.since != base.Add(2*time.Hour) {
		t.Fatalf("gap must restart streak: commit=%v streak=%+v", commit, s)
	}
	// breach cleared before the window → target none commits immediately, streak cleared
	if commit, s := gateEntry(tierNone, tierNone, streak(base, base), base.Add(time.Minute), dur, gap); commit != tierNone || !s.since.IsZero() {
		t.Fatalf("cleared early: commit=%v streak=%+v", commit, s)
	}
	// duration 0 → fire immediately (legacy behavior)
	if commit, _ := gateEntry(tierNone, tierWarning, pendingStreak{}, base, 0, gap); commit != tierWarning {
		t.Fatalf("no duration must fire immediately, got %v", commit)
	}
	// already firing → escalation is immediate (not gated)
	if commit, _ := gateEntry(tierWarning, tierCritical, pendingStreak{}, base, dur, gap); commit != tierCritical {
		t.Fatalf("escalation must be immediate, got %v", commit)
	}
	// already firing → recovery is immediate
	if commit, _ := gateEntry(tierWarning, tierNone, pendingStreak{}, base, dur, gap); commit != tierNone {
		t.Fatalf("recovery must be immediate, got %v", commit)
	}
}

// TestTransitionSustainedFor drives transition() over time with a "for" duration: a breach
// must persist for the duration before firing, a transient spike that clears must NOT
// fire, and an agent that goes offline mid-window must NOT fire on a single sample when it
// returns (the streak restarts after a gap).
func TestTransitionSustainedFor(t *testing.T) {
	th := metricThreshold{enabled: true, warning: 80, critical: 95, hysteresis: 5, duration: 2 * time.Minute}
	base := time.Unix(1_700_000_000, 0)

	newEval := func() *metricAlertEvaluator {
		return &metricAlertEvaluator{
			state:        map[string]map[metricKind]alertTier{},
			peak:         map[string]map[metricKind]alertTier{},
			pending:      map[string]map[metricKind]pendingStreak{},
			pollInterval: time.Minute, // maxStreakGap = 3m
		}
	}

	// Sustained breach fires only after the window.
	e := newEval()
	if _, _, _, changed := e.transition("a", metricCPU, 90, th, base); changed {
		t.Fatal("first breach poll must not fire (pending)")
	}
	if _, _, _, changed := e.transition("a", metricCPU, 90, th, base.Add(time.Minute)); changed {
		t.Fatal("still within the for-window must not fire")
	}
	_, next, _, changed := e.transition("a", metricCPU, 90, th, base.Add(2*time.Minute))
	if !changed || next != tierWarning {
		t.Fatalf("sustained breach must fire warning: changed=%v next=%v", changed, next)
	}

	// Transient spike that clears before the window never fires.
	e2 := newEval()
	if _, _, _, changed := e2.transition("b", metricCPU, 99, th, base); changed {
		t.Fatal("spike start must not fire")
	}
	if _, _, _, changed := e2.transition("b", metricCPU, 10, th, base.Add(30*time.Second)); changed {
		t.Fatal("spike cleared before window must not fire")
	}
	// after the original window elapses, still nothing (streak was reset)
	if _, _, _, changed := e2.transition("b", metricCPU, 10, th, base.Add(3*time.Minute)); changed {
		t.Fatal("no alert should remain after a transient spike")
	}

	// Agent offline mid-window then returns: the gap (>> maxStreakGap) restarts the streak,
	// so a single breaching sample on return must NOT fire.
	e3 := newEval()
	if _, _, _, changed := e3.transition("c", metricCPU, 90, th, base); changed {
		t.Fatal("breach start must not fire")
	}
	// returns 2h later, still breaching → streak restarts, no immediate fire
	if _, _, _, changed := e3.transition("c", metricCPU, 90, th, base.Add(2*time.Hour)); changed {
		t.Fatal("single sample after an offline gap must not fire (streak restarted)")
	}
	// but it now fires once sustained for the full window from the restart
	if _, next, _, changed := e3.transition("c", metricCPU, 90, th, base.Add(2*time.Hour+2*time.Minute)); !changed || next != tierWarning {
		t.Fatalf("must fire after a fresh sustained window post-gap: changed=%v next=%v", changed, next)
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

// TestInstantSeverity locks the hosts-overview bar coloring: severity reflects the
// instantaneous value against the resolved thresholds, IGNORING enabled/hysteresis/
// duration/mute (the bar shows the machine, not the alert state). Resolution is
// per-agent override > global > built-in default (80/90 for cpu/memory/disk).
func TestInstantSeverity(t *testing.T) {
	cases := []struct {
		name    string
		eval    *metricAlertEvaluator
		agent   string
		metric  metricKind
		value   float64
		want    alertTier
	}{
		{
			name:   "vanilla install uses built-in default: 99.6%% CPU is critical",
			eval:   &metricAlertEvaluator{},
			agent:  "a1",
			metric: metricCPU,
			value:  99.6,
			want:   tierCritical,
		},
		{
			name:   "built-in default warning band",
			eval:   &metricAlertEvaluator{},
			agent:  "a1",
			metric: metricMemory,
			value:  85,
			want:   tierWarning,
		},
		{
			name:   "below built-in default is normal",
			eval:   &metricAlertEvaluator{},
			agent:  "a1",
			metric: metricDisk,
			value:  50,
			want:   tierNone,
		},
		{
			name: "disabled global row still colors the bar (enabled ignored)",
			eval: &metricAlertEvaluator{
				global: map[metricKind]metricThreshold{metricCPU: {enabled: false, warning: 60, critical: 70}},
			},
			agent:  "a1",
			metric: metricCPU,
			value:  65,
			want:   tierWarning,
		},
		{
			name: "disabled per-agent override (mute) still colors and wins over global",
			eval: &metricAlertEvaluator{
				global:   map[metricKind]metricThreshold{metricCPU: {enabled: true, warning: 80, critical: 90}},
				perAgent: map[string]map[metricKind]metricThreshold{"a1": {metricCPU: {enabled: false, warning: 50, critical: 60}}},
			},
			agent:  "a1",
			metric: metricCPU,
			value:  55,
			want:   tierWarning,
		},
		{
			name:   "metric with no configured row and no built-in default is normal",
			eval:   &metricAlertEvaluator{},
			agent:  "a1",
			metric: metricLoadAvg,
			value:  99,
			want:   tierNone,
		},
		{
			// A "mute" row (disabled, both bands left at 0) must NOT leave the bar uncolored:
			// each band falls through to the built-in default so a saturated host still reds.
			name: "zeroed mute override falls through to built-in default",
			eval: &metricAlertEvaluator{
				perAgent: map[string]map[metricKind]metricThreshold{"a1": {metricCPU: {enabled: false}}},
			},
			agent:  "a1",
			metric: metricCPU,
			value:  100,
			want:   tierCritical,
		},
		{
			// A global row that sets only critical must let the warning band fall through to
			// the default (80), not silently lose the intermediate warning color.
			name: "global with only critical set keeps default warning band",
			eval: &metricAlertEvaluator{
				global: map[metricKind]metricThreshold{metricCPU: {enabled: true, critical: 95}},
			},
			agent:  "a1",
			metric: metricCPU,
			value:  82,
			want:   tierWarning,
		},
		{
			// A partial override (warning only) keeps its own warning but inherits the default
			// critical so the upper band is not lost.
			name: "partial override inherits default critical",
			eval: &metricAlertEvaluator{
				perAgent: map[string]map[metricKind]metricThreshold{"a1": {metricCPU: {enabled: true, warning: 50}}},
			},
			agent:  "a1",
			metric: metricCPU,
			value:  95,
			want:   tierCritical,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.eval.instantSeverity(tc.agent, tc.metric, tc.value); got != tc.want {
				t.Fatalf("instantSeverity(%s, %v) = %v, want %v", tc.agent, tc.value, got, tc.want)
			}
		})
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
