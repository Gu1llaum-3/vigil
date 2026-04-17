package migrations

import (
	"encoding/json"

	"github.com/pocketbase/pocketbase/core"
	m "github.com/pocketbase/pocketbase/migrations"
)

func init() {
	m.Register(func(app core.App) error {
		collection, err := app.FindCollectionByNameOrId("monitors")
		if err != nil {
			return err
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
		addField := func(name string, field map[string]any) {
			for _, raw := range fields {
				if existing, ok := raw.(map[string]any); ok && existing["name"] == name {
					return
				}
			}
			fields = append(fields, field)
		}

		addField("failure_threshold", map[string]any{
			"hidden":      false,
			"id":          "number5200000006",
			"min":         0,
			"name":        "failure_threshold",
			"presentable": false,
			"required":    false,
			"system":      false,
			"type":        "number",
		})
		addField("failure_count", map[string]any{
			"hidden":      true,
			"id":          "number5200000007",
			"min":         0,
			"name":        "failure_count",
			"presentable": false,
			"required":    false,
			"system":      false,
			"type":        "number",
		})

		snapshot["fields"] = fields

		updated, err := json.Marshal(snapshot)
		if err != nil {
			return err
		}
		if err := collection.UnmarshalJSON(updated); err != nil {
			return err
		}
		if err := app.Save(collection); err != nil {
			return err
		}

		_, err = app.DB().NewQuery("UPDATE monitors SET failure_threshold = 3 WHERE failure_threshold IS NULL OR failure_threshold = 0").Execute()
		return err
	}, nil)
}
