package migrations

import (
	"encoding/json"

	"github.com/pocketbase/pocketbase/core"
	m "github.com/pocketbase/pocketbase/migrations"
)

// Adds the disk_mounts JSON column to host_metric_current: the latest per-mount used% for
// every real local filesystem the agent reported. It backs the host-detail per-filesystem
// breakdown and the hub-side "worst monitored mount" computation (per-host include/exclude).
// Latest-only cache, so a small array. This migration only depends on host_metric_current
// (created by 21_create_host_metrics.go); "35_" sorts after "21_" in the lexical apply order
// used on a fresh DB, which is all that matters here — same family/precedent as
// 24_add_metric_alert_columns.go and 26_add_alert_tiers.go.
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
			if f, ok := raw.(map[string]any); ok && f["name"] == "disk_mounts" {
				return nil // already present
			}
		}
		existing = append(existing, map[string]any{
			"hidden":      false,
			"id":          "json7200000020",
			"maxSize":     20000,
			"name":        "disk_mounts",
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
