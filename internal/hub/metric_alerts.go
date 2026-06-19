package hub

import (
	"log/slog"
	"math"
	"sync"
	"time"

	"github.com/Gu1llaum-3/vigil/internal/common"
	"github.com/Gu1llaum-3/vigil/internal/hub/notifications"
	"github.com/pocketbase/pocketbase/core"
)

const metricAlertsCollection = "metric_alerts"

// defaultHysteresisFor returns the fallback resolve-margin (dead band) for a metric
// when none is configured. Percent metrics tolerate a wide 5-point band; loadavg is
// a small unitless number (≈ cores), so a 5-point band would swamp typical thresholds
// and make alerts unrecoverable — use a small margin instead.
func defaultHysteresisFor(metric metricKind) float64 {
	if metric == metricLoadAvg {
		return 0.5
	}
	return 5.0
}

// metricKind identifies an alertable host metric.
type metricKind string

const (
	metricCPU     metricKind = "cpu"
	metricMemory  metricKind = "memory"
	metricDisk    metricKind = "disk"
	metricLoadAvg metricKind = "loadavg"
)

var alertableMetrics = []metricKind{metricCPU, metricMemory, metricDisk, metricLoadAvg}

// alertTier is the current breach level for a (agent, metric).
type alertTier int

const (
	tierNone alertTier = iota
	tierWarning
	tierCritical
)

func (t alertTier) String() string {
	switch t {
	case tierWarning:
		return "warning"
	case tierCritical:
		return "critical"
	default:
		return "normal"
	}
}

type metricThreshold struct {
	enabled    bool
	warning    float64
	critical   float64
	hysteresis float64
}

// metricAlertEvaluator holds the threshold cache and the in-memory edge-trigger
// state, and turns freshly collected host metrics into notification events.
type metricAlertEvaluator struct {
	hub *Hub

	mu       sync.RWMutex
	global   map[metricKind]metricThreshold
	perAgent map[string]map[metricKind]metricThreshold

	stateMu sync.Mutex
	state   map[string]map[metricKind]alertTier // agentID → metric → fired tier
}

func newMetricAlertEvaluator(h *Hub) *metricAlertEvaluator {
	return &metricAlertEvaluator{
		hub:      h,
		global:   map[metricKind]metricThreshold{},
		perAgent: map[string]map[metricKind]metricThreshold{},
		state:    map[string]map[metricKind]alertTier{},
	}
}

// load (re)builds the threshold cache from the metric_alerts collection.
func (e *metricAlertEvaluator) load() {
	records, err := e.hub.FindAllRecords(metricAlertsCollection)
	if err != nil {
		slog.Warn("metric alerts: failed to load thresholds", "err", err)
		return
	}
	global := map[metricKind]metricThreshold{}
	perAgent := map[string]map[metricKind]metricThreshold{}
	for _, rec := range records {
		metric := metricKind(rec.GetString("metric"))
		th := metricThreshold{
			enabled:    rec.GetBool("enabled"),
			warning:    numberAsFloat64(rec.Get("warning_value")),
			critical:   numberAsFloat64(rec.Get("critical_value")),
			hysteresis: numberAsFloat64(rec.Get("hysteresis")),
		}
		if th.hysteresis <= 0 {
			th.hysteresis = defaultHysteresisFor(metric)
		}
		if agentID := rec.GetString("agent"); agentID != "" {
			if perAgent[agentID] == nil {
				perAgent[agentID] = map[metricKind]metricThreshold{}
			}
			perAgent[agentID][metric] = th
		} else {
			global[metric] = th
		}
	}
	e.mu.Lock()
	e.global = global
	e.perAgent = perAgent
	e.mu.Unlock()
}

// thresholdFor resolves the effective threshold for (agent, metric): per-agent
// override wins over the global default.
func (e *metricAlertEvaluator) thresholdFor(agentID string, metric metricKind) (metricThreshold, bool) {
	e.mu.RLock()
	defer e.mu.RUnlock()
	if byMetric, ok := e.perAgent[agentID]; ok {
		if th, ok := byMetric[metric]; ok {
			return th, th.enabled
		}
	}
	th, ok := e.global[metric]
	return th, ok && th.enabled
}

// evaluate compares each metric against its threshold and dispatches edge-triggered
// notifications. Safe to call from the per-agent collection goroutines: the
// read-compute-write of the tier is done atomically in transition() so two
// concurrent polls for the same agent cannot both dispatch the same escalation.
func (e *metricAlertEvaluator) evaluate(agentID string, metrics common.HostMetricsResponse) {
	for _, metric := range alertableMetrics {
		th, ok := e.thresholdFor(agentID, metric)
		if !ok {
			e.clearState(agentID, metric)
			continue
		}
		value, unit := metricValue(metric, metrics)
		// A failed or partial collection leaves a metric at exactly 0. On a live host
		// that reports metrics, none of these (memory %, root disk %, CPU %, load) is
		// ever genuinely 0, so treat an exact 0 as "no reading": keep the current tier
		// instead of emitting a spurious recovery. A truly idle metric resolves on the
		// next poll, where the reading is a tiny non-zero value below the clear bound.
		if value == 0 {
			continue
		}
		prev, next, changed := e.transition(agentID, metric, value, th)
		if !changed {
			continue
		}
		e.dispatch(agentID, metric, value, unit, prev, next, th)
	}
}

func (e *metricAlertEvaluator) dispatch(agentID string, metric metricKind, value float64, unit string, prev, next alertTier, th metricThreshold) {
	var evt notifications.Event
	switch {
	case next > prev: // escalation (→warning, →critical)
		threshold := th.warning
		severity := "warning"
		if next == tierCritical {
			threshold = th.critical
			severity = "critical"
		}
		evt = notifications.Event{
			Kind:     notifications.EventHostMetricExceeded,
			Severity: severity,
			Previous: prev.String(),
			Current:  next.String(),
			Details: map[string]any{
				"metric":    string(metric),
				"value":     value,
				"threshold": threshold,
				"tier":      next.String(),
				"unit":      unit,
			},
		}
	case next == tierNone: // full recovery
		evt = notifications.Event{
			Kind:     notifications.EventHostMetricRecovered,
			Previous: prev.String(),
			Current:  next.String(),
			Details: map[string]any{
				"metric": string(metric),
				"value":  value,
				"unit":   unit,
			},
		}
	default: // downgrade critical→warning: stay in alert, stay silent in v1
		return
	}

	evt.OccurredAt = time.Now()
	evt.Resource = notifications.ResourceRef{ID: agentID, Name: e.agentName(agentID), Type: "agent"}

	if err := e.hub.createSystemNotification(evt); err != nil {
		slog.Warn("metric alerts: failed to create system notification", "agent", agentID, "metric", metric, "err", err)
	}
	e.hub.notifier.Dispatch(evt)
}

func (e *metricAlertEvaluator) agentName(agentID string) string {
	if rec, err := e.hub.FindRecordById("agents", agentID); err == nil {
		if name := rec.GetString("name"); name != "" {
			return name
		}
	}
	return agentID
}

// --- edge-trigger state ---

// transition atomically computes the next tier for (agent, metric) from value and,
// if it differs from the current tier, stores it and reports the previous tier. The
// whole read-modify-write happens under one lock so concurrent polls for the same
// agent cannot both observe the same prev and dispatch twice.
func (e *metricAlertEvaluator) transition(agentID string, metric metricKind, value float64, th metricThreshold) (prev, next alertTier, changed bool) {
	e.stateMu.Lock()
	defer e.stateMu.Unlock()
	prev = e.state[agentID][metric]
	next = computeTier(prev, value, th)
	if next == prev {
		return prev, next, false
	}
	if e.state[agentID] == nil {
		e.state[agentID] = map[metricKind]alertTier{}
	}
	e.state[agentID][metric] = next
	return prev, next, true
}

// clearState forgets the breach state for (agent, metric) when no threshold is
// configured (a config removal is not a metric "recovery", so no event is sent).
func (e *metricAlertEvaluator) clearState(agentID string, metric metricKind) {
	e.stateMu.Lock()
	defer e.stateMu.Unlock()
	if byMetric, ok := e.state[agentID]; ok {
		delete(byMetric, metric)
	}
}

// snapshotTiers returns the active (non-normal) breach tiers for an agent as a
// JSON-serializable map, for persistence into host_metric_current.alert_tiers.
func (e *metricAlertEvaluator) snapshotTiers(agentID string) map[string]int {
	e.stateMu.Lock()
	defer e.stateMu.Unlock()
	byMetric := e.state[agentID]
	out := make(map[string]int, len(byMetric))
	for m, t := range byMetric {
		if t != tierNone {
			out[string(m)] = int(t)
		}
	}
	return out
}

// loadState restores the edge-trigger state from host_metric_current.alert_tiers so
// a hub restart does not re-fire alerts that were already active.
func (e *metricAlertEvaluator) loadState() {
	records, err := e.hub.FindAllRecords(hostMetricCurrentCollection)
	if err != nil {
		slog.Warn("metric alerts: failed to restore edge state", "err", err)
		return
	}
	state := map[string]map[metricKind]alertTier{}
	for _, rec := range records {
		agentID := rec.GetString("agent")
		if agentID == "" {
			continue
		}
		var tiers map[string]int
		if err := rec.UnmarshalJSONField("alert_tiers", &tiers); err != nil || len(tiers) == 0 {
			continue
		}
		byMetric := map[metricKind]alertTier{}
		for k, v := range tiers {
			if v != int(tierNone) {
				byMetric[metricKind(k)] = alertTier(v)
			}
		}
		if len(byMetric) > 0 {
			state[agentID] = byMetric
		}
	}
	e.stateMu.Lock()
	e.state = state
	e.stateMu.Unlock()
}

// metricValue extracts the comparable value (and its unit) for a metric, with a
// graceful fallback to root disk usage for agents older than the max-disk field.
func metricValue(metric metricKind, m common.HostMetricsResponse) (float64, string) {
	switch metric {
	case metricCPU:
		return m.CPUPercent, "%"
	case metricMemory:
		return m.MemoryUsedPercent, "%"
	case metricDisk:
		if m.DiskMaxUsedPercent > 0 {
			return m.DiskMaxUsedPercent, "%"
		}
		return m.DiskUsedPercent, "%"
	case metricLoadAvg:
		return m.Load1, ""
	default:
		return 0, ""
	}
}

// computeTier returns the breach tier for value, applying hysteresis on the way
// down: a tier is only left once value falls below (tierThreshold - hysteresis).
//
// The dead band is clamped to 90% of the threshold (clearBound) so the recovery
// point stays strictly positive even if a stored hysteresis is ≥ the threshold
// (e.g. a legacy loadavg row with threshold 2 and hysteresis 5). Without this, the
// band would extend below 0 and a fired alert could never recover. The API also
// rejects hysteresis ≥ threshold at write time; this is the defensive floor.
func computeTier(prev alertTier, value float64, th metricThreshold) alertTier {
	if th.critical > 0 {
		if value >= th.critical {
			return tierCritical
		}
		if prev == tierCritical && value >= clearBound(th.critical, th.hysteresis) {
			return tierCritical
		}
	}
	if th.warning > 0 {
		if value >= th.warning {
			return tierWarning
		}
		if (prev == tierWarning || prev == tierCritical) && value >= clearBound(th.warning, th.hysteresis) {
			return tierWarning
		}
	}
	return tierNone
}

// clearBound is the value a metric must fall below to leave a breach tier. The
// hysteresis is capped at 90% of the threshold so the bound is always > 0 and the
// alert remains recoverable regardless of the configured/stored hysteresis.
func clearBound(threshold, hysteresis float64) float64 {
	return threshold - math.Min(hysteresis, threshold*0.9)
}

// evaluateMetricAlerts is the Hub entry point called from persistHostMetrics.
func (h *Hub) evaluateMetricAlerts(agentID string, metrics common.HostMetricsResponse) {
	if h.metricAlerts == nil {
		return
	}
	h.metricAlerts.evaluate(agentID, metrics)
}

// registerMetricAlertHooks keeps the threshold cache in sync. metric_alerts is a
// low-frequency, admin-edited collection, so update hooks are safe here (unlike
// the high-frequency monitor/metric collections — see conventions doc).
func (h *Hub) registerMetricAlertHooks() {
	reload := func(e *core.RecordEvent) error {
		h.metricAlerts.load()
		return e.Next()
	}
	h.App.OnRecordAfterCreateSuccess(metricAlertsCollection).BindFunc(reload)
	h.App.OnRecordAfterUpdateSuccess(metricAlertsCollection).BindFunc(reload)
	h.App.OnRecordAfterDeleteSuccess(metricAlertsCollection).BindFunc(reload)
}
