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
			return err
		}
		field := collection.Fields.GetByName("status")
		if field == nil {
			return nil
		}
		raw, err := json.Marshal(field)
		if err != nil {
			return err
		}
		var generic map[string]any
		if err := json.Unmarshal(raw, &generic); err != nil {
			return err
		}
		values, _ := generic["values"].([]any)
		seen := make(map[string]struct{}, len(values))
		for _, v := range values {
			if s, ok := v.(string); ok {
				seen[s] = struct{}{}
			}
		}
		if _, ok := seen["disabled"]; ok {
			return nil
		}
		values = append(values, "disabled")
		generic["values"] = values
		updated, err := json.Marshal(generic)
		if err != nil {
			return err
		}
		var newField core.SelectField
		if err := json.Unmarshal(updated, &newField); err != nil {
			return err
		}
		collection.Fields.Add(&newField)
		return app.Save(collection)
	}, nil)
}
