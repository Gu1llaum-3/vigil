package migrations

import (
	"github.com/pocketbase/pocketbase/core"
	m "github.com/pocketbase/pocketbase/migrations"
)

func init() {
	m.Register(func(app core.App) error {
		jsonData := `[
	{
		"id": "pbc_6000000009",
		"listRule": "@request.auth.id != \"\"",
		"viewRule": "@request.auth.id != \"\"",
		"createRule": null,
		"updateRule": null,
		"deleteRule": null,
		"name": "system_notifications",
		"type": "base",
		"fields": [
			{
				"autogeneratePattern": "[a-z0-9]{15}",
				"hidden": false,
				"id": "text6900000000",
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
				"id": "text6900000001",
				"name": "event_kind",
				"presentable": false,
				"required": true,
				"system": false,
				"type": "text"
			},
			{
				"hidden": false,
				"id": "select6900000001",
				"name": "category",
				"presentable": false,
				"required": true,
				"system": false,
				"type": "select",
				"maxSelect": 1,
				"values": ["monitors", "agents", "container_images"]
			},
			{
				"hidden": false,
				"id": "select6900000002",
				"name": "severity",
				"presentable": false,
				"required": true,
				"system": false,
				"type": "select",
				"maxSelect": 1,
				"values": ["info", "warning", "critical"]
			},
			{
				"hidden": false,
				"id": "text6900000002",
				"name": "resource_type",
				"presentable": false,
				"required": false,
				"system": false,
				"type": "text"
			},
			{
				"hidden": false,
				"id": "text6900000003",
				"name": "resource_id",
				"presentable": false,
				"required": false,
				"system": false,
				"type": "text"
			},
			{
				"hidden": false,
				"id": "text6900000004",
				"name": "resource_name",
				"presentable": false,
				"required": false,
				"system": false,
				"type": "text"
			},
			{
				"hidden": false,
				"id": "text6900000005",
				"name": "title",
				"presentable": true,
				"required": true,
				"system": false,
				"type": "text"
			},
			{
				"hidden": false,
				"id": "text6900000006",
				"name": "message",
				"presentable": false,
				"required": false,
				"system": false,
				"type": "text"
			},
			{
				"hidden": false,
				"id": "json6900000001",
				"maxSize": 5000,
				"name": "payload",
				"presentable": false,
				"required": false,
				"system": false,
				"type": "json"
			},
			{
				"hidden": false,
				"id": "date6900000001",
				"max": "",
				"min": "",
				"name": "occurred_at",
				"presentable": false,
				"required": true,
				"system": false,
				"type": "date"
			},
			{
				"hidden": false,
				"id": "autodate6900000001",
				"name": "created",
				"onCreate": true,
				"onUpdate": false,
				"presentable": false,
				"system": false,
				"type": "autodate"
			},
			{
				"hidden": false,
				"id": "autodate6900000002",
				"name": "updated",
				"onCreate": true,
				"onUpdate": true,
				"presentable": false,
				"system": false,
				"type": "autodate"
			}
		],
		"indexes": [
			"CREATE INDEX ` + "`" + `idx_system_notifications_category_occurred` + "`" + ` ON ` + "`" + `system_notifications` + "`" + ` (` + "`" + `category` + "`" + `, ` + "`" + `occurred_at` + "`" + `)",
			"CREATE INDEX ` + "`" + `idx_system_notifications_event_occurred` + "`" + ` ON ` + "`" + `system_notifications` + "`" + ` (` + "`" + `event_kind` + "`" + `, ` + "`" + `occurred_at` + "`" + `)"
		],
		"system": false
	}
]`
		return app.ImportCollectionsByMarshaledJSON([]byte(jsonData), false)
	}, nil)
}
