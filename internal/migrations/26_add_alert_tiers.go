package migrations

import (
	"encoding/json"

	"github.com/pocketbase/pocketbase/core"
	m "github.com/pocketbase/pocketbase/migrations"
)

// Adds the alert_tiers JSON column to host_metric_current. It persists the
// edge-trigger state (metric → fired tier) per agent so a hub restart does not
// re-fire metric alerts that are already active. Latest-only cache, so a tiny map.
func init() {
	m.Register(func(app core.App) error {
		collection, err := app.FindCollectionByNameOrId("host_metric_current")
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
			if f, ok := raw.(map[string]any); ok && f["name"] == "alert_tiers" {
				return nil // already present
			}
		}
		existing = append(existing, map[string]any{
			"hidden":      false,
			"id":          "json7200000010",
			"maxSize":     5000,
			"name":        "alert_tiers",
			"presentable": false,
			"required":    false,
			"system":      false,
			"type":        "json",
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
