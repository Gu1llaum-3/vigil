package migrations

import (
	"encoding/json"

	"github.com/pocketbase/pocketbase/core"
	m "github.com/pocketbase/pocketbase/migrations"
)

// Adds the duration_seconds column to metric_alerts. It is the "sustained for" delay: a
// cold-start breach (none → warning/critical) only fires once the value has stayed over
// the threshold continuously for this long (0 = fire immediately). Reduces false alarms
// from transient spikes, complementing the hysteresis (which governs the exit side).
func init() {
	m.Register(func(app core.App) error {
		collection, err := app.FindCollectionByNameOrId("metric_alerts")
		if err != nil {
			return nil // collection missing → nothing to migrate
		}
		data, err := collection.MarshalJSON()
		if err != nil {
			return err
		}
		var snapshot map[string]any
		if err := json.Unmarshal(data, &snapshot); err != nil {
			return err
		}
		fields, _ := snapshot["fields"].([]any)
		for _, raw := range fields {
			if f, ok := raw.(map[string]any); ok && f["name"] == "duration_seconds" {
				return nil // already present
			}
		}
		fields = append(fields, map[string]any{
			"hidden":      false,
			"id":          "number7400000010",
			"name":        "duration_seconds",
			"presentable": false,
			"required":    false,
			"system":      false,
			"type":        "number",
			"min":         float64(0),
		})
		snapshot["fields"] = fields
		updated, err := json.Marshal(snapshot)
		if err != nil {
			return err
		}
		if err := collection.UnmarshalJSON(updated); err != nil {
			return err
		}
		return app.Save(collection)
	}, nil)
}
