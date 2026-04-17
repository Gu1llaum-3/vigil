package migrations

import (
	"github.com/pocketbase/pocketbase/core"
	m "github.com/pocketbase/pocketbase/migrations"
)

const notificationRuleChannelsMaxSelect = 2147483647

func init() {
	m.Register(func(app core.App) error {
		collection, err := app.FindCollectionByNameOrId("notification_rules")
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
			if !ok || field["name"] != "channels" {
				continue
			}
			field["maxSelect"] = notificationRuleChannelsMaxSelect
		}

		snapshot["fields"] = fields
		return saveCollectionSnapshot(app, collection, snapshot)
	}, nil)
}
