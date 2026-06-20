package hub

import (
	"encoding/json"
	"log/slog"
	"math"
	"sync"
	"time"

	"github.com/Gu1llaum-3/vigil/internal/common"
	"github.com/Gu1llaum-3/vigil/internal/hub/notifications"
	"github.com/pocketbase/dbx"
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
	state   map[string]map[metricKind]alertTier // agentID → metric → current fired tier
	// peak holds the highest tier reached during the current alert episode, so a
	// recovery carries the peak severity even after a silent critical→warning downgrade
	// (otherwise a min_severity=critical rule would miss the recovery). In-memory only;
	// a restart falls back to the restored current tier (see transition).
	peak map[string]map[metricKind]alertTier

	// cores caches each agent's CPU core count (from its snapshot) so the loadavg metric
	// can be normalized to load-per-core — a single global threshold then means the same
	// thing on a 1-core and a 64-core host. Populated on snapshot upsert and lazily from
	// the stored snapshot on a cache miss.
	coresMu sync.RWMutex
	cores   map[string]int
}

func newMetricAlertEvaluator(h *Hub) *metricAlertEvaluator {
	return &metricAlertEvaluator{
		hub:      h,
		global:   map[metricKind]metricThreshold{},
		perAgent: map[string]map[metricKind]metricThreshold{},
		state:    map[string]map[metricKind]alertTier{},
		peak:     map[string]map[metricKind]alertTier{},
		cores:    map[string]int{},
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

// thresholdFor resolves the effective threshold for (agent, metric). A per-agent
// override always wins over the global default, including when it is disabled: a
// disabled override means "mute this metric for this host" and does NOT fall back to
// an enabled global. To re-inherit the global, the override row is deleted (the UI's
// "Reset to global"), not disabled.
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
		// loadavg is normalized to load-per-core so a single global threshold means the
		// same thing on every host (1.0 = fully utilized). If the core count is unknown
		// (no snapshot yet), skip rather than alert on a raw, host-size-dependent value.
		cores := 0
		if metric == metricLoadAvg {
			cores = e.coresFor(agentID)
			perCore, ok := loadPerCore(value, cores)
			if !ok {
				continue
			}
			value, unit = perCore, "/core"
		}
		// A failed or partial collection leaves a metric at exactly 0. For metrics that
		// are never genuinely 0 on a live host (memory %, root disk %, and CPU %, which
		// also reads 0 on the first post-connect sample before it has a baseline), treat
		// an exact 0 as "no reading" and keep the current tier instead of emitting a
		// spurious recovery. loadavg legitimately reads 0.00 on an idle host, so its 0 is
		// honored (otherwise a fired load alert could never recover on an idle box).
		if value == 0 && zeroIsMissing(metric) {
			continue
		}
		prev, next, peak, changed := e.transition(agentID, metric, value, th)
		if !changed {
			continue
		}
		evt, ok := metricAlertEvent(agentID, e.agentName(agentID), metric, value, unit, prev, next, peak, th, cores, metrics)
		if !ok {
			continue
		}
		e.dispatch(evt, agentID, metric)
	}
}

// zeroIsMissing reports whether an exact-0 reading for a metric should be treated as a
// non-reading (failed/partial collection or no baseline) rather than a real value.
func zeroIsMissing(metric metricKind) bool {
	return metric != metricLoadAvg
}

// loadPerCore normalizes a raw load average by the CPU core count so a single global
// threshold expressed as load-per-core (1.0 == fully utilized) is meaningful across
// heterogeneous hosts. ok is false when the core count is unknown (no snapshot yet) so
// the caller can skip the metric rather than alert on an unnormalized value.
func loadPerCore(load float64, cores int) (float64, bool) {
	if cores <= 0 {
		return 0, false
	}
	return load / float64(cores), true
}

// coresFor returns an agent's CPU core count, cached. On a miss it reads the agent's
// stored snapshot once and caches the result (0 if unknown).
func (e *metricAlertEvaluator) coresFor(agentID string) int {
	e.coresMu.RLock()
	c, ok := e.cores[agentID]
	e.coresMu.RUnlock()
	if ok {
		return c
	}
	c = e.loadCoresFromSnapshot(agentID)
	e.coresMu.Lock()
	e.cores[agentID] = c
	e.coresMu.Unlock()
	return c
}

// setCores updates the cached core count for an agent (called when a fresh snapshot is
// stored). A non-positive value is ignored so a partial snapshot cannot wipe a known count.
func (e *metricAlertEvaluator) setCores(agentID string, cores int) {
	if cores <= 0 {
		return
	}
	e.coresMu.Lock()
	e.cores[agentID] = cores
	e.coresMu.Unlock()
}

func (e *metricAlertEvaluator) loadCoresFromSnapshot(agentID string) int {
	rec, err := e.hub.FindFirstRecordByFilter("host_snapshots", "agent = {:agent}", dbx.Params{"agent": agentID})
	if err != nil {
		return 0
	}
	var snap common.HostSnapshotResponse
	if json.Unmarshal([]byte(rec.GetString("data")), &snap) != nil {
		return 0
	}
	return snap.Resources.CPUCores
}

// metricAlertEvent builds the notification event for a tier transition, or returns
// ok=false when the transition is silent (a critical→warning downgrade). It is pure so
// the severity/details logic can be unit-tested without a hub. OccurredAt is stamped by
// the caller (dispatch).
func metricAlertEvent(agentID, agentName string, metric metricKind, value float64, unit string, prev, next, peak alertTier, th metricThreshold, cores int, metrics common.HostMetricsResponse) (notifications.Event, bool) {
	// addContext augments a details map with metric-specific fields used by the message
	// templates and the in-app feed.
	addContext := func(d map[string]any) {
		// Disk alerts fire on the highest-used filesystem, which may not be root; name it
		// so the notification matches the breached partition, not the root usage.
		if metric == metricDisk {
			d["mount"] = diskMountLabel(metrics)
		}
		// loadavg is reported per-core; carry the raw 5-minute load and core count so the
		// message can show the absolute figure too ("load 12.4 across 8 cores").
		if metric == metricLoadAvg {
			d["load_raw"] = metrics.Load5
			d["cores"] = cores
		}
	}

	var evt notifications.Event
	switch {
	case next > prev: // escalation (→warning, →critical)
		threshold := th.warning
		severity := "warning"
		if next == tierCritical {
			threshold = th.critical
			severity = "critical"
		}
		details := map[string]any{
			"metric":    string(metric),
			"value":     value,
			"threshold": threshold,
			"tier":      next.String(),
			"unit":      unit,
		}
		addContext(details)
		evt = notifications.Event{
			Kind:     notifications.EventHostMetricExceeded,
			Severity: severity,
			Previous: prev.String(),
			Current:  next.String(),
			Details:  details,
		}
	case next == tierNone: // full recovery
		// Carry the peak severity reached during the episode (warning/critical), not the
		// default "info" nor the possibly-downgraded immediate tier, so a rule with
		// min_severity≥warning (incl. =critical after a critical→warning decline) still
		// delivers the matching recovery instead of leaving the alert stuck "active".
		details := map[string]any{
			"metric": string(metric),
			"value":  value,
			"unit":   unit,
		}
		addContext(details)
		evt = notifications.Event{
			Kind:     notifications.EventHostMetricRecovered,
			Severity: peak.String(),
			Previous: prev.String(),
			Current:  next.String(),
			Details:  details,
		}
	default: // downgrade critical→warning: stay in alert, stay silent in v1
		return notifications.Event{}, false
	}

	evt.Resource = notifications.ResourceRef{ID: agentID, Name: agentName, Type: "agent"}
	return evt, true
}

func (e *metricAlertEvaluator) dispatch(evt notifications.Event, agentID string, metric metricKind) {
	evt.OccurredAt = time.Now()
	if err := e.hub.createSystemNotification(evt); err != nil {
		slog.Warn("metric alerts: failed to create system notification", "agent", agentID, "metric", metric, "err", err)
	}
	e.hub.notifier.Dispatch(evt)
}

func (e *metricAlertEvaluator) agentName(agentID string) string {
	rec, err := e.hub.FindRecordById("agents", agentID)
	if err != nil {
		return agentID
	}
	return agentLogName(rec)
}

// --- edge-trigger state ---

// transition atomically computes the next tier for (agent, metric) from value and,
// if it differs from the current tier, stores it and reports the previous tier plus the
// peak tier reached during this alert episode. The whole read-modify-write happens under
// one lock so concurrent polls for the same agent cannot both observe the same prev and
// dispatch twice. peak is what a recovery should be notified at: it survives a silent
// critical→warning downgrade and is reset once the metric returns to normal.
func (e *metricAlertEvaluator) transition(agentID string, metric metricKind, value float64, th metricThreshold) (prev, next, peak alertTier, changed bool) {
	e.stateMu.Lock()
	defer e.stateMu.Unlock()
	prev = e.state[agentID][metric]
	next = computeTier(prev, value, th)
	// The peak is at least the current tier (covers a restart that restored the tier
	// but not the peak).
	peak = e.peak[agentID][metric]
	if peak < prev {
		peak = prev
	}
	if next == prev {
		return prev, next, peak, false
	}
	if next > peak {
		peak = next
	}
	if next == tierNone {
		// Recovered: report the episode peak, then forget it.
		if byMetric, ok := e.peak[agentID]; ok {
			delete(byMetric, metric)
		}
	} else {
		if e.peak[agentID] == nil {
			e.peak[agentID] = map[metricKind]alertTier{}
		}
		e.peak[agentID][metric] = peak
	}
	if e.state[agentID] == nil {
		e.state[agentID] = map[metricKind]alertTier{}
	}
	e.state[agentID][metric] = next
	return prev, next, peak, true
}

// clearState forgets the breach state for (agent, metric) when no threshold is
// configured (a config removal is not a metric "recovery", so no event is sent).
func (e *metricAlertEvaluator) clearState(agentID string, metric metricKind) {
	e.stateMu.Lock()
	defer e.stateMu.Unlock()
	if byMetric, ok := e.state[agentID]; ok {
		delete(byMetric, metric)
	}
	if byMetric, ok := e.peak[agentID]; ok {
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
		// Use the 5-minute load (sustained load) for alerting, not the 1-minute, which
		// is too spiky for alerts — the standard window used by Datadog/Nagios/Zabbix.
		return m.Load5, ""
	default:
		return 0, ""
	}
}

// diskMountLabel returns the filesystem the disk alert refers to: the busiest mount
// reported by the agent, or root for legacy agents / the root-usage fallback.
func diskMountLabel(m common.HostMetricsResponse) string {
	if m.DiskMaxUsedPercent > 0 && m.DiskMaxMount != "" {
		return m.DiskMaxMount
	}
	return "/"
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
