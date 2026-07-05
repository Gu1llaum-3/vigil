package migrations

import (
	"encoding/json"

	"github.com/pocketbase/pocketbase/core"
	m "github.com/pocketbase/pocketbase/migrations"
)

// Adds a `disk_max_mount` text column to host_metric_current: the name of the worst
// *monitored* filesystem (after the per-host include/exclude rule), so the fleet disk bar can
// label the mount it is showing. Current-only (the history chart needs the percent, not the
// name). Depends only on host_metric_current (21_create_host_metrics.go); "37_" sorts after
// "21_" in fresh-DB apply order — same family as 24_/26_/35_.
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
			if f, ok := raw.(map[string]any); ok && f["name"] == "disk_max_mount" {
				return nil // already present
			}
		}
		existing = append(existing, map[string]any{
			"hidden":      false,
			"id":          "text7200000030",
			"name":        "disk_max_mount",
			"presentable": false,
			"required":    false,
			"system":      false,
			"type":        "text",
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
