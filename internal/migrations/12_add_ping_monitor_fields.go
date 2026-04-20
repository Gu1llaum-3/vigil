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
		addField := func(name string, field map[string]any) {
			for _, raw := range fields {
				if existing, ok := raw.(map[string]any); ok && existing["name"] == name {
					return
				}
			}
			fields = append(fields, field)
		}

		addField("ping_count", map[string]any{
			"hidden":      false,
			"id":          "number5200000008",
			"min":         1,
			"name":        "ping_count",
			"presentable": false,
			"required":    false,
			"system":      false,
			"type":        "number",
		})
		addField("ping_per_request_timeout", map[string]any{
			"hidden":      false,
			"id":          "number5200000009",
			"min":         1,
			"name":        "ping_per_request_timeout",
			"presentable": false,
			"required":    false,
			"system":      false,
			"type":        "number",
		})
		addField("ping_ip_family", map[string]any{
			"hidden":      false,
			"id":          "select5200000004",
			"name":        "ping_ip_family",
			"presentable": false,
			"required":    false,
			"system":      false,
			"type":        "select",
			"maxSelect":   1,
			"values":      []string{"", "ipv4", "ipv6"},
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
