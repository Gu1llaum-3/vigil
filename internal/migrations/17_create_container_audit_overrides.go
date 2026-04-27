package migrations

import (
	"github.com/pocketbase/pocketbase/core"
	m "github.com/pocketbase/pocketbase/migrations"
)

func init() {
	m.Register(func(app core.App) error {
		jsonData := `[
	{
		"id": "pbc_6000000008",
		"listRule": "@request.auth.id != \"\"",
		"viewRule": "@request.auth.id != \"\"",
		"createRule": null,
		"updateRule": null,
		"deleteRule": null,
		"name": "container_audit_overrides",
		"type": "base",
		"fields": [
			{
				"autogeneratePattern": "[a-z0-9]{15}",
				"hidden": false,
				"id": "text6800000000",
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
				"id": "relation6800000001",
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
				"id": "text6800000001",
				"name": "container_name",
				"presentable": true,
				"required": true,
				"system": false,
				"type": "text"
			},
			{
				"hidden": false,
				"id": "select6800000001",
				"name": "policy",
				"presentable": false,
				"required": true,
				"system": false,
				"type": "select",
				"maxSelect": 1,
				"values": ["digest", "patch", "minor", "disabled"]
			},
			{
				"hidden": false,
				"id": "text6800000002",
				"name": "notes",
				"presentable": false,
				"required": false,
				"system": false,
				"type": "text"
			},
			{
				"hidden": false,
				"id": "autodate6800000001",
				"name": "created",
				"onCreate": true,
				"onUpdate": false,
				"presentable": false,
				"system": false,
				"type": "autodate"
			},
			{
				"hidden": false,
				"id": "autodate6800000002",
				"name": "updated",
				"onCreate": true,
				"onUpdate": true,
				"presentable": false,
				"system": false,
				"type": "autodate"
			}
		],
		"indexes": [
			"CREATE UNIQUE INDEX ` + "`" + `idx_container_audit_overrides_agent_name` + "`" + ` ON ` + "`" + `container_audit_overrides` + "`" + ` (` + "`" + `agent` + "`" + `, ` + "`" + `container_name` + "`" + `)"
		],
		"system": false
	}
]`
		return app.ImportCollectionsByMarshaledJSON([]byte(jsonData), false)
	}, nil)
}
