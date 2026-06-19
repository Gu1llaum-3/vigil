package migrations

import (
	"encoding/json"
	"fmt"

	"github.com/pocketbase/pocketbase/core"
	m "github.com/pocketbase/pocketbase/migrations"
)

// Adds the load-average and max-disk metric columns (used by the metric-alert
// evaluator and future charts) to both the append-only samples collection and
// the latest-only current cache.
func init() {
	m.Register(func(app core.App) error {
		// idPrefix keeps field ids unique per collection.
		type newField struct{ name string }
		fields := []newField{
			{"load1"},
			{"load5"},
			{"load15"},
			{"disk_max_used_percent"},
		}

		add := func(collectionName, idPrefix string) error {
			collection, err := app.FindCollectionByNameOrId(collectionName)
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
			has := func(name string) bool {
				for _, raw := range existing {
					if f, ok := raw.(map[string]any); ok && f["name"] == name {
						return true
					}
				}
				return false
			}
			for i, f := range fields {
				if has(f.name) {
					continue
				}
				existing = append(existing, map[string]any{
					"hidden":      false,
					"id":          fmt.Sprintf("number%s%02d", idPrefix, i+1),
					"name":        f.name,
					"presentable": false,
					"required":    false,
					"system":      false,
					"type":        "number",
				})
			}
			snapshot["fields"] = existing
			updated, err := json.Marshal(snapshot)
			if err != nil {
				return err
			}
			if err := collection.UnmarshalJSON(updated); err != nil {
				return err
			}
			return app.Save(collection)
		}

		if err := add("host_metric_samples", "7100000"); err != nil {
			return err
		}
		return add("host_metric_current", "7200000")
	}, nil)
}
