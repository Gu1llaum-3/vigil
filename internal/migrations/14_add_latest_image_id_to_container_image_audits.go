package migrations

import (
	"encoding/json"

	"github.com/pocketbase/pocketbase/core"
	m "github.com/pocketbase/pocketbase/migrations"
)

func init() {
	m.Register(func(app core.App) error {
		collection, err := app.FindCollectionByNameOrId("container_image_audits")
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
		for _, raw := range fields {
			if existing, ok := raw.(map[string]any); ok && existing["name"] == "latest_image_id" {
				snapshot["fields"] = fields
				updated, err := json.Marshal(snapshot)
				if err != nil {
					return err
				}
				if err := collection.UnmarshalJSON(updated); err != nil {
					return err
				}
				return app.Save(collection)
			}
		}

		fields = append(fields, map[string]any{
			"hidden":      false,
			"id":          "text6600000013",
			"name":        "latest_image_id",
			"presentable": false,
			"required":    false,
			"system":      false,
			"type":        "text",
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
