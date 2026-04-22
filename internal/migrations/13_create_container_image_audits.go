package migrations

import (
	"github.com/pocketbase/pocketbase/core"
	m "github.com/pocketbase/pocketbase/migrations"
)

func init() {
	m.Register(func(app core.App) error {
		jsonData := `[
	{
		"id": "pbc_6000000006",
		"listRule": "@request.auth.id != \"\"",
		"viewRule": "@request.auth.id != \"\"",
		"createRule": null,
		"updateRule": null,
		"deleteRule": null,
		"name": "container_image_audits",
		"type": "base",
		"fields": [
			{
				"autogeneratePattern": "[a-z0-9]{15}",
				"hidden": false,
				"id": "text6600000000",
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
				"collectionId": "pbc_4000000001",
				"hidden": false,
				"id": "relation6600000001",
				"maxSelect": 1,
				"minSelect": 0,
				"name": "agent",
				"presentable": false,
				"required": true,
				"system": false,
				"type": "relation"
			},
			{
				"hidden": false,
				"id": "text6600000002",
				"name": "container_id",
				"presentable": false,
				"required": true,
				"system": false,
				"type": "text"
			},
			{
				"hidden": false,
				"id": "text6600000003",
				"name": "container_name",
				"presentable": false,
				"required": false,
				"system": false,
				"type": "text"
			},
			{
				"hidden": false,
				"id": "text6600000004",
				"name": "image_ref",
				"presentable": false,
				"required": true,
				"system": false,
				"type": "text"
			},
			{
				"hidden": false,
				"id": "text6600000005",
				"name": "registry",
				"presentable": false,
				"required": false,
				"system": false,
				"type": "text"
			},
			{
				"hidden": false,
				"id": "text6600000006",
				"name": "repository",
				"presentable": false,
				"required": false,
				"system": false,
				"type": "text"
			},
			{
				"hidden": false,
				"id": "text6600000007",
				"name": "tag",
				"presentable": false,
				"required": false,
				"system": false,
				"type": "text"
			},
			{
				"hidden": false,
				"id": "text6600000008",
				"name": "local_image_id",
				"presentable": false,
				"required": false,
				"system": false,
				"type": "text"
			},
			{
				"hidden": false,
				"id": "text6600000009",
				"name": "local_digest",
				"presentable": false,
				"required": false,
				"system": false,
				"type": "text"
			},
			{
				"hidden": false,
				"id": "select6600000001",
				"name": "policy",
				"presentable": false,
				"required": false,
				"system": false,
				"type": "select",
				"maxSelect": 1,
				"values": ["digest_latest", "semver_major", "semver_minor", "unsupported"]
			},
			{
				"hidden": false,
				"id": "select6600000002",
				"name": "status",
				"presentable": false,
				"required": true,
				"system": false,
				"type": "select",
				"maxSelect": 1,
				"values": ["up_to_date", "update_available", "unknown", "unsupported", "check_failed"]
			},
			{
				"hidden": false,
				"id": "text6600000010",
				"name": "latest_tag",
				"presentable": false,
				"required": false,
				"system": false,
				"type": "text"
			},
			{
				"hidden": false,
				"id": "text6600000011",
				"name": "latest_digest",
				"presentable": false,
				"required": false,
				"system": false,
				"type": "text"
			},
			{
				"hidden": false,
				"id": "date6600000001",
				"max": "",
				"min": "",
				"name": "checked_at",
				"presentable": false,
				"required": false,
				"system": false,
				"type": "date"
			},
			{
				"hidden": false,
				"id": "text6600000012",
				"name": "error",
				"presentable": false,
				"required": false,
				"system": false,
				"type": "text"
			},
			{
				"hidden": false,
				"id": "json6600000001",
				"maxSize": 2000000,
				"name": "details",
				"presentable": false,
				"required": false,
				"system": false,
				"type": "json"
			},
			{
				"hidden": false,
				"id": "autodate6600000001",
				"name": "created",
				"onCreate": true,
				"onUpdate": false,
				"presentable": false,
				"system": false,
				"type": "autodate"
			},
			{
				"hidden": false,
				"id": "autodate6600000002",
				"name": "updated",
				"onCreate": true,
				"onUpdate": true,
				"presentable": false,
				"system": false,
				"type": "autodate"
			}
		],
		"indexes": [
			"CREATE UNIQUE INDEX ` + "`" + `idx_container_image_audits_agent_container` + "`" + ` ON ` + "`" + `container_image_audits` + "`" + ` (` + "`" + `agent` + "`" + `, ` + "`" + `container_id` + "`" + `)",
			"CREATE INDEX ` + "`" + `idx_container_image_audits_status` + "`" + ` ON ` + "`" + `container_image_audits` + "`" + ` (` + "`" + `status` + "`" + `)"
		],
		"system": false
	}
]`
		return app.ImportCollectionsByMarshaledJSON([]byte(jsonData), false)
	}, nil)
}
