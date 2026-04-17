package migrations

import (
	"github.com/pocketbase/pocketbase/core"
	m "github.com/pocketbase/pocketbase/migrations"
)

func init() {
	m.Register(func(app core.App) error {
		jsonData := `[
	{
		"id": "pbc_6000000001",
		"listRule": "@request.auth.role = \"admin\"",
		"viewRule": "@request.auth.role = \"admin\"",
		"createRule": null,
		"updateRule": null,
		"deleteRule": null,
		"name": "notification_channels",
		"type": "base",
		"fields": [
			{
				"autogeneratePattern": "[a-z0-9]{15}",
				"hidden": false,
				"id": "text6100000000",
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
				"id": "text6100000001",
				"name": "name",
				"presentable": false,
				"required": true,
				"system": false,
				"type": "text"
			},
			{
				"hidden": false,
				"id": "select6100000001",
				"name": "kind",
				"presentable": false,
				"required": true,
				"system": false,
				"type": "select",
				"maxSelect": 1,
				"values": ["email", "webhook", "slack", "teams", "gchat", "ntfy", "gotify"]
			},
			{
				"hidden": false,
				"id": "bool6100000001",
				"name": "enabled",
				"presentable": false,
				"required": false,
				"system": false,
				"type": "bool"
			},
			{
				"hidden": false,
				"id": "json6100000001",
				"maxSize": 5000,
				"name": "config",
				"presentable": false,
				"required": false,
				"system": false,
				"type": "json"
			},
			{
				"cascadeDelete": false,
				"collectionId": "_pb_users_auth_",
				"hidden": false,
				"id": "relation6100000001",
				"maxSelect": 1,
				"minSelect": 0,
				"name": "created_by",
				"presentable": false,
				"required": false,
				"system": false,
				"type": "relation"
			},
			{
				"hidden": false,
				"id": "autodate6100000001",
				"name": "created",
				"onCreate": true,
				"onUpdate": false,
				"presentable": false,
				"system": false,
				"type": "autodate"
			},
			{
				"hidden": false,
				"id": "autodate6100000002",
				"name": "updated",
				"onCreate": true,
				"onUpdate": true,
				"presentable": false,
				"system": false,
				"type": "autodate"
			}
		],
		"indexes": [
			"CREATE UNIQUE INDEX ` + "`" + `idx_notification_channels_name` + "`" + ` ON ` + "`" + `notification_channels` + "`" + ` (` + "`" + `name` + "`" + `)"
		],
		"system": false
	},
	{
		"id": "pbc_6000000002",
		"listRule": "@request.auth.role = \"admin\"",
		"viewRule": "@request.auth.role = \"admin\"",
		"createRule": null,
		"updateRule": null,
		"deleteRule": null,
		"name": "notification_rules",
		"type": "base",
		"fields": [
			{
				"autogeneratePattern": "[a-z0-9]{15}",
				"hidden": false,
				"id": "text6200000000",
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
				"id": "text6200000001",
				"name": "name",
				"presentable": false,
				"required": true,
				"system": false,
				"type": "text"
			},
			{
				"hidden": false,
				"id": "bool6200000001",
				"name": "enabled",
				"presentable": false,
				"required": false,
				"system": false,
				"type": "bool"
			},
			{
				"hidden": false,
				"id": "json6200000001",
				"maxSize": 2000,
				"name": "events",
				"presentable": false,
				"required": false,
				"system": false,
				"type": "json"
			},
			{
				"hidden": false,
				"id": "json6200000002",
				"maxSize": 2000,
				"name": "filter",
				"presentable": false,
				"required": false,
				"system": false,
				"type": "json"
			},
			{
				"cascadeDelete": false,
				"collectionId": "pbc_6000000001",
				"hidden": false,
				"id": "relation6200000001",
				"maxSelect": 2147483647,
				"minSelect": 0,
				"name": "channels",
				"presentable": false,
				"required": false,
				"system": false,
				"type": "relation"
			},
			{
				"hidden": false,
				"id": "select6200000001",
				"name": "min_severity",
				"presentable": false,
				"required": false,
				"system": false,
				"type": "select",
				"maxSelect": 1,
				"values": ["info", "warning", "critical"]
			},
			{
				"hidden": false,
				"id": "number6200000001",
				"name": "throttle_seconds",
				"presentable": false,
				"required": false,
				"system": false,
				"type": "number"
			},
			{
				"cascadeDelete": false,
				"collectionId": "_pb_users_auth_",
				"hidden": false,
				"id": "relation6200000002",
				"maxSelect": 1,
				"minSelect": 0,
				"name": "created_by",
				"presentable": false,
				"required": false,
				"system": false,
				"type": "relation"
			},
			{
				"hidden": false,
				"id": "autodate6200000001",
				"name": "created",
				"onCreate": true,
				"onUpdate": false,
				"presentable": false,
				"system": false,
				"type": "autodate"
			},
			{
				"hidden": false,
				"id": "autodate6200000002",
				"name": "updated",
				"onCreate": true,
				"onUpdate": true,
				"presentable": false,
				"system": false,
				"type": "autodate"
			}
		],
		"system": false
	},
	{
		"id": "pbc_6000000003",
		"listRule": "@request.auth.role = \"admin\"",
		"viewRule": "@request.auth.role = \"admin\"",
		"createRule": null,
		"updateRule": null,
		"deleteRule": null,
		"name": "notification_logs",
		"type": "base",
		"fields": [
			{
				"autogeneratePattern": "[a-z0-9]{15}",
				"hidden": false,
				"id": "text6300000000",
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
				"cascadeDelete": true,
				"collectionId": "pbc_6000000002",
				"hidden": false,
				"id": "relation6300000001",
				"maxSelect": 1,
				"minSelect": 0,
				"name": "rule",
				"presentable": false,
				"required": false,
				"system": false,
				"type": "relation"
			},
			{
				"cascadeDelete": true,
				"collectionId": "pbc_6000000001",
				"hidden": false,
				"id": "relation6300000002",
				"maxSelect": 1,
				"minSelect": 0,
				"name": "channel",
				"presentable": false,
				"required": false,
				"system": false,
				"type": "relation"
			},
			{
				"hidden": false,
				"id": "text6300000001",
				"name": "event_kind",
				"presentable": false,
				"required": false,
				"system": false,
				"type": "text"
			},
			{
				"hidden": false,
				"id": "text6300000002",
				"name": "resource_id",
				"presentable": false,
				"required": false,
				"system": false,
				"type": "text"
			},
			{
				"hidden": false,
				"id": "text6300000003",
				"name": "resource_type",
				"presentable": false,
				"required": false,
				"system": false,
				"type": "text"
			},
			{
				"hidden": false,
				"id": "select6300000001",
				"name": "status",
				"presentable": false,
				"required": false,
				"system": false,
				"type": "select",
				"maxSelect": 1,
				"values": ["sent", "failed", "throttled"]
			},
			{
				"hidden": false,
				"id": "text6300000004",
				"name": "error",
				"presentable": false,
				"required": false,
				"system": false,
				"type": "text"
			},
			{
				"hidden": false,
				"id": "text6300000005",
				"name": "payload_preview",
				"presentable": false,
				"required": false,
				"system": false,
				"type": "text"
			},
			{
				"hidden": false,
				"id": "date6300000001",
				"max": "",
				"min": "",
				"name": "sent_at",
				"presentable": false,
				"required": false,
				"system": false,
				"type": "date"
			},
			{
				"hidden": false,
				"id": "autodate6300000001",
				"name": "created",
				"onCreate": true,
				"onUpdate": false,
				"presentable": false,
				"system": false,
				"type": "autodate"
			},
			{
				"hidden": false,
				"id": "autodate6300000002",
				"name": "updated",
				"onCreate": true,
				"onUpdate": true,
				"presentable": false,
				"system": false,
				"type": "autodate"
			}
		],
		"indexes": [
			"CREATE INDEX ` + "`" + `idx_notification_logs_rule_sent_at` + "`" + ` ON ` + "`" + `notification_logs` + "`" + ` (` + "`" + `rule` + "`" + `, ` + "`" + `sent_at` + "`" + `)",
			"CREATE INDEX ` + "`" + `idx_notification_logs_resource_sent_at` + "`" + ` ON ` + "`" + `notification_logs` + "`" + ` (` + "`" + `resource_id` + "`" + `, ` + "`" + `sent_at` + "`" + `)"
		],
		"system": false
	}
]`
		return app.ImportCollectionsByMarshaledJSON([]byte(jsonData), false)
	}, nil)
}
