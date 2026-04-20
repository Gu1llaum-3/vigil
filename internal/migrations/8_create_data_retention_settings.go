package migrations

import (
	"github.com/pocketbase/pocketbase/core"
	m "github.com/pocketbase/pocketbase/migrations"
)

func init() {
	m.Register(func(app core.App) error {
		jsonData := `[
	{
		"id": "pbc_6000000004",
		"listRule": "@request.auth.role = \"admin\"",
		"viewRule": "@request.auth.role = \"admin\"",
		"createRule": null,
		"updateRule": null,
		"deleteRule": null,
		"name": "data_retention_settings",
		"type": "base",
		"fields": [
			{
				"autogeneratePattern": "[a-z0-9]{15}",
				"hidden": false,
				"id": "text6400000000",
				"max": 15,
				"min": 15,
				"name": "id",
				"pattern": "^[a-z0-9]+$",
				"presentable": false,
				"primaryKey": true,
				"required": true,
				"system": true,
				"type": "text"
			},
			{
				"hidden": false,
				"id": "text6400000001",
				"max": 50,
				"min": 1,
				"name": "key",
				"pattern": "",
				"presentable": false,
				"required": true,
				"system": false,
				"type": "text"
			},
			{
				"hidden": false,
				"id": "number6400000001",
				"min": 1,
				"name": "monitor_events_retention_days",
				"presentable": false,
				"required": false,
				"system": false,
				"type": "number"
			},
			{
				"hidden": false,
				"id": "number6400000002",
				"min": 1,
				"name": "notification_logs_retention_days",
				"presentable": false,
				"required": false,
				"system": false,
				"type": "number"
			},
			{
				"hidden": false,
				"id": "number6400000003",
				"min": 1,
				"name": "monitor_events_manual_default_days",
				"presentable": false,
				"required": false,
				"system": false,
				"type": "number"
			},
			{
				"hidden": false,
				"id": "number6400000004",
				"min": 1,
				"name": "notification_logs_manual_default_days",
				"presentable": false,
				"required": false,
				"system": false,
				"type": "number"
			},
			{
				"hidden": false,
				"id": "number6400000005",
				"min": 1,
				"name": "offline_agents_manual_default_days",
				"presentable": false,
				"required": false,
				"system": false,
				"type": "number"
			},
			{
				"hidden": false,
				"id": "autodate6400000001",
				"name": "created",
				"onCreate": true,
				"onUpdate": false,
				"presentable": false,
				"system": false,
				"type": "autodate"
			},
			{
				"hidden": false,
				"id": "autodate6400000002",
				"name": "updated",
				"onCreate": true,
				"onUpdate": true,
				"presentable": false,
				"system": false,
				"type": "autodate"
			}
		],
		"indexes": [
			"CREATE UNIQUE INDEX ` + "`" + `idx_data_retention_settings_key` + "`" + ` ON ` + "`" + `data_retention_settings` + "`" + ` (` + "`" + `key` + "`" + `)"
		],
		"system": false
	}
]`
		return app.ImportCollectionsByMarshaledJSON([]byte(jsonData), false)
	}, nil)
}
