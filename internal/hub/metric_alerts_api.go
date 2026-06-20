package hub

import (
	"errors"
	"net/http"
	"strings"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

type metricAlertPayload struct {
	ID            string  `json:"id"`
	Agent         string  `json:"agent"` // "" = global default
	Metric        string  `json:"metric"`
	Enabled       bool    `json:"enabled"`
	WarningValue  float64 `json:"warning_value"`
	CriticalValue float64 `json:"critical_value"`
	Hysteresis    float64 `json:"hysteresis"`
	// DurationSeconds is the "sustained for" delay before a cold-start breach fires
	// (0 = immediate).
	DurationSeconds float64 `json:"duration_seconds"`
	Created         string  `json:"created"`
	Updated         string  `json:"updated"`
}

func metricAlertResponse(rec *core.Record) metricAlertPayload {
	return metricAlertPayload{
		ID:              rec.Id,
		Agent:           rec.GetString("agent"),
		Metric:          rec.GetString("metric"),
		Enabled:         rec.GetBool("enabled"),
		WarningValue:    numberAsFloat64(rec.Get("warning_value")),
		CriticalValue:   numberAsFloat64(rec.Get("critical_value")),
		Hysteresis:      numberAsFloat64(rec.Get("hysteresis")),
		DurationSeconds: numberAsFloat64(rec.Get("duration_seconds")),
		Created:         rec.GetString("created"),
		Updated:         rec.GetString("updated"),
	}
}

// validateMetricAlertPayload checks an upsert payload: known metric, non-negative
// values, warning ≤ critical, an enabled alert has at least one positive threshold
// (otherwise it shows as active in the UI yet can never fire), and the resolve margin
// stays below the lowest active threshold (so a fired alert can always recover).
func validateMetricAlertPayload(body metricAlertPayload) error {
	if !isAlertableMetric(body.Metric) {
		return errors.New("invalid metric")
	}
	if body.WarningValue < 0 || body.CriticalValue < 0 || body.Hysteresis < 0 {
		return errors.New("thresholds must be non-negative")
	}
	if body.WarningValue > 0 && body.CriticalValue > 0 && body.WarningValue > body.CriticalValue {
		return errors.New("warning threshold must be ≤ critical threshold")
	}
	if body.Enabled && body.WarningValue <= 0 && body.CriticalValue <= 0 {
		return errors.New("an enabled alert needs a warning or critical threshold")
	}
	minThreshold := body.WarningValue
	if minThreshold <= 0 {
		minThreshold = body.CriticalValue
	}
	if minThreshold > 0 && body.Hysteresis >= minThreshold {
		return errors.New("resolve margin must be smaller than the threshold")
	}
	if body.DurationSeconds < 0 {
		return errors.New("duration must be non-negative")
	}
	return nil
}

func isAlertableMetric(metric string) bool {
	for _, m := range alertableMetrics {
		if string(m) == metric {
			return true
		}
	}
	return false
}

// findMetricAlertRecord locates the row for (agent, metric) by scanning, which
// correctly matches the global row where agent is empty (a relation `= ""` filter
// does not). Returns nil if none.
func (h *Hub) findMetricAlertRecord(agent, metric string) *core.Record {
	records, err := h.FindAllRecords(metricAlertsCollection)
	if err != nil {
		return nil
	}
	for _, rec := range records {
		if rec.GetString("agent") == agent && rec.GetString("metric") == metric {
			return rec
		}
	}
	return nil
}

func (h *Hub) listMetricAlerts(e *core.RequestEvent) error {
	records, err := h.FindAllRecords(metricAlertsCollection)
	if err != nil {
		return err
	}
	out := make([]metricAlertPayload, 0, len(records))
	for _, rec := range records {
		out = append(out, metricAlertResponse(rec))
	}
	return e.JSON(http.StatusOK, out)
}

// upsertMetricAlert creates or updates the threshold for (agent, metric). An empty
// agent is the global default. Uniqueness is enforced on (agent, metric); the
// record hooks reload the evaluator cache on save.
func (h *Hub) upsertMetricAlert(e *core.RequestEvent) error {
	var body metricAlertPayload
	if err := e.BindBody(&body); err != nil {
		return e.BadRequestError("Invalid request body", err)
	}
	body.Agent = strings.TrimSpace(body.Agent)
	body.Metric = strings.TrimSpace(body.Metric)
	if err := validateMetricAlertPayload(body); err != nil {
		return e.BadRequestError(err.Error(), nil)
	}

	// NOTE: do not filter by `agent = ""` — PocketBase relation filtering does not
	// match an empty (unset) relation, so the global row would never be found and a
	// duplicate insert would hit the unique (agent, metric) index. Scan instead (the
	// collection is tiny). upsertByUnique can't be reused here for the same reason, so
	// retry the scan+save on a unique conflict from a concurrent insert.
	const maxAttempts = 4
	var rec *core.Record
	var saveErr error
	for attempt := 0; attempt < maxAttempts; attempt++ {
		rec = h.findMetricAlertRecord(body.Agent, body.Metric)
		if rec == nil {
			collection, err := h.FindCachedCollectionByNameOrId(metricAlertsCollection)
			if err != nil {
				return err
			}
			rec = core.NewRecord(collection)
			rec.Set("agent", body.Agent)
			rec.Set("metric", body.Metric)
		}
		rec.Set("enabled", body.Enabled)
		rec.Set("warning_value", body.WarningValue)
		rec.Set("critical_value", body.CriticalValue)
		rec.Set("hysteresis", body.Hysteresis)
		rec.Set("duration_seconds", body.DurationSeconds)

		saveErr = h.Save(rec)
		if saveErr == nil {
			return e.JSON(http.StatusOK, metricAlertResponse(rec))
		}
		if !isUniqueConstraintErr(saveErr) {
			return e.BadRequestError("Failed to save metric alert", saveErr)
		}
		// A concurrent writer won the insert; re-scan so this turns into an update.
	}
	return e.BadRequestError("Failed to save metric alert", saveErr)
}

func (h *Hub) deleteMetricAlert(e *core.RequestEvent) error {
	id := e.Request.PathValue("id")
	rec, err := h.FindFirstRecordByFilter(metricAlertsCollection, "id = {:id}", dbx.Params{"id": id})
	if err != nil {
		return e.NotFoundError("Metric alert not found", err)
	}
	if err := h.Delete(rec); err != nil {
		return err
	}
	return e.JSON(http.StatusOK, map[string]any{"ok": true})
}
