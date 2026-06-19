package hub

import (
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
	Created       string  `json:"created"`
	Updated       string  `json:"updated"`
}

func metricAlertResponse(rec *core.Record) metricAlertPayload {
	return metricAlertPayload{
		ID:            rec.Id,
		Agent:         rec.GetString("agent"),
		Metric:        rec.GetString("metric"),
		Enabled:       rec.GetBool("enabled"),
		WarningValue:  numberAsFloat64(rec.Get("warning_value")),
		CriticalValue: numberAsFloat64(rec.Get("critical_value")),
		Hysteresis:    numberAsFloat64(rec.Get("hysteresis")),
		Created:       rec.GetString("created"),
		Updated:       rec.GetString("updated"),
	}
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
	if !isAlertableMetric(body.Metric) {
		return e.BadRequestError("invalid metric", nil)
	}
	if body.WarningValue < 0 || body.CriticalValue < 0 || body.Hysteresis < 0 {
		return e.BadRequestError("thresholds must be non-negative", nil)
	}
	if body.WarningValue > 0 && body.CriticalValue > 0 && body.WarningValue > body.CriticalValue {
		return e.BadRequestError("warning threshold must be ≤ critical threshold", nil)
	}

	// NOTE: do not filter by `agent = ""` — PocketBase relation filtering does not
	// match an empty (unset) relation, so the global row would never be found and a
	// duplicate insert would hit the unique (agent, metric) index. Scan instead
	// (the collection is tiny: a few metrics × global + per-agent overrides).
	existing := h.findMetricAlertRecord(body.Agent, body.Metric)
	if existing == nil {
		collection, err := h.FindCachedCollectionByNameOrId(metricAlertsCollection)
		if err != nil {
			return err
		}
		existing = core.NewRecord(collection)
		existing.Set("agent", body.Agent)
		existing.Set("metric", body.Metric)
	}
	existing.Set("enabled", body.Enabled)
	existing.Set("warning_value", body.WarningValue)
	existing.Set("critical_value", body.CriticalValue)
	existing.Set("hysteresis", body.Hysteresis)

	if err := h.Save(existing); err != nil {
		return e.BadRequestError("Failed to save metric alert", err)
	}
	return e.JSON(http.StatusOK, metricAlertResponse(existing))
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
