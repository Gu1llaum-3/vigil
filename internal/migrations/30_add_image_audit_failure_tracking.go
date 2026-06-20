package migrations

import (
	"encoding/json"

	"github.com/pocketbase/pocketbase/core"
	m "github.com/pocketbase/pocketbase/migrations"
)

// Adds transient-failure tracking columns to container_image_audits so a one-off
// registry timeout/network error no longer overwrites the last good result with
// check_failed. consecutive_failures counts transient failures since the last success;
// last_check_error / last_check_error_at record the most recent soft error while the
// previous good status is preserved (until the failure grace is exhausted).
func init() {
	m.Register(func(app core.App) error {
		collection, err := app.FindCollectionByNameOrId("container_image_audits")
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
		has := func(name string) bool {
			for _, raw := range fields {
				if f, ok := raw.(map[string]any); ok && f["name"] == name {
					return true
				}
			}
			return false
		}
		if !has("consecutive_failures") {
			fields = append(fields, map[string]any{
				"hidden": false, "id": "number6600000020", "name": "consecutive_failures",
				"presentable": false, "required": false, "system": false, "type": "number", "min": float64(0),
			})
		}
		if !has("last_check_error") {
			fields = append(fields, map[string]any{
				"hidden": false, "id": "text6600000021", "name": "last_check_error",
				"presentable": false, "required": false, "system": false, "type": "text",
			})
		}
		if !has("last_check_error_at") {
			fields = append(fields, map[string]any{
				"hidden": false, "id": "text6600000022", "name": "last_check_error_at",
				"presentable": false, "required": false, "system": false, "type": "text",
			})
		}
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
