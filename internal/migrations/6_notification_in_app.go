package migrations

import (
	"encoding/json"
	"slices"

	"github.com/pocketbase/pocketbase/core"
	m "github.com/pocketbase/pocketbase/migrations"
)

func init() {
	m.Register(func(app core.App) error {
		if err := updateNotificationChannelsCollection(app); err != nil {
			return err
		}
		return updateNotificationLogsCollection(app)
	}, nil)
}

func updateNotificationChannelsCollection(app core.App) error {
	collection, err := app.FindCollectionByNameOrId("notification_channels")
	if err != nil {
		return err
	}

	snapshot, err := collectionSnapshot(collection)
	if err != nil {
		return err
	}

	fields, _ := snapshot["fields"].([]any)
	for _, raw := range fields {
		field, ok := raw.(map[string]any)
		if !ok || field["name"] != "kind" {
			continue
		}
		values, _ := field["values"].([]any)
		if !slices.Contains(values, any("in-app")) {
			field["values"] = append(values, "in-app")
		}
	}

	return saveCollectionSnapshot(app, collection, snapshot)
}

func updateNotificationLogsCollection(app core.App) error {
	collection, err := app.FindCollectionByNameOrId("notification_logs")
	if err != nil {
		return err
	}

	snapshot, err := collectionSnapshot(collection)
	if err != nil {
		return err
	}

	fields, _ := snapshot["fields"].([]any)
	addField := func(name string, field map[string]any) {
		for _, raw := range fields {
			existing, ok := raw.(map[string]any)
			if ok && existing["name"] == name {
				return
			}
		}
		fields = append(fields, field)
	}

	addField("created_by", map[string]any{
		"cascadeDelete": false,
		"collectionId":  "_pb_users_auth_",
		"hidden":        false,
		"id":            "relation6300000003",
		"maxSelect":     1,
		"minSelect":     0,
		"name":          "created_by",
		"presentable":   false,
		"required":      false,
		"system":        false,
		"type":          "relation",
	})
	addField("channel_kind", map[string]any{
		"hidden":      false,
		"id":          "text6300000006",
		"name":        "channel_kind",
		"presentable": false,
		"required":    false,
		"system":      false,
		"type":        "text",
	})
	addField("resource_name", map[string]any{
		"hidden":      false,
		"id":          "text6300000007",
		"name":        "resource_name",
		"presentable": false,
		"required":    false,
		"system":      false,
		"type":        "text",
	})

	snapshot["fields"] = fields

	indexes, _ := snapshot["indexes"].([]any)
	createdByIndex := "CREATE INDEX `idx_notification_logs_created_by_sent_at` ON `notification_logs` (`created_by`, `sent_at`)"
	if !slices.Contains(indexes, any(createdByIndex)) {
		snapshot["indexes"] = append(indexes, createdByIndex)
	}

	return saveCollectionSnapshot(app, collection, snapshot)
}

func collectionSnapshot(collection *core.Collection) (map[string]any, error) {
	data, err := collection.MarshalJSON()
	if err != nil {
		return nil, err
	}

	var snapshot map[string]any
	if err := json.Unmarshal(data, &snapshot); err != nil {
		return nil, err
	}

	return snapshot, nil
}

func saveCollectionSnapshot(app core.App, collection *core.Collection, snapshot map[string]any) error {
	updated, err := json.Marshal(snapshot)
	if err != nil {
		return err
	}
	if err := collection.UnmarshalJSON(updated); err != nil {
		return err
	}
	return app.Save(collection)
}
