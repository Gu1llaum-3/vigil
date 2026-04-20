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
		for _, raw := range fields {
			field, ok := raw.(map[string]any)
			if !ok || field["name"] != "type" {
				continue
			}

			values, _ := field["values"].([]any)
			for _, value := range values {
				if s, ok := value.(string); ok && s == "ping" {
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

			field["values"] = append(values, "ping")
			break
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
