package migrations

import (
	"encoding/json"

	"github.com/pocketbase/pocketbase/core"
	m "github.com/pocketbase/pocketbase/migrations"
)

// Adds a `maintenance` boolean to monitor_events. It is set at check time when the check
// falls inside an active maintenance window covering the monitor, and lets the uptime
// aggregates exclude those checks from the up/(up+down) denominator (Uptime-Kuma model:
// planned maintenance does not count against uptime). Existing rows default to false.
func init() {
	m.Register(func(app core.App) error {
		collection, err := app.FindCollectionByNameOrId("monitor_events")
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
		existing, _ := snapshot["fields"].([]any)
		for _, raw := range existing {
			if f, ok := raw.(map[string]any); ok && f["name"] == "maintenance" {
				return nil // already added
			}
		}
		existing = append(existing, map[string]any{
			"hidden":      false,
			"id":          "bool5100000001",
			"name":        "maintenance",
			"presentable": false,
			"required":    false,
			"system":      false,
			"type":        "bool",
		})
		snapshot["fields"] = existing
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
