package migrations

import (
	"encoding/json"

	"github.com/pocketbase/pocketbase/core"
	m "github.com/pocketbase/pocketbase/migrations"
)

func init() {
	m.Register(func(app core.App) error {
		collection, err := app.FindCollectionByNameOrId("notification_logs")
		if err != nil {
			return nil
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
		fieldExists := func(name string) bool {
			for _, raw := range fields {
				if existing, ok := raw.(map[string]any); ok && existing["name"] == name {
					return true
				}
			}
			return false
		}

		if !fieldExists("read_at") {
			fields = append(fields, map[string]any{
				"hidden":      false,
				"id":          "text6300000008",
				"name":        "read_at",
				"presentable": false,
				"required":    false,
				"system":      false,
				"type":        "text",
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
