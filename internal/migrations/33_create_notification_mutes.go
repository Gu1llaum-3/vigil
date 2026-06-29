package migrations

import (
	"github.com/pocketbase/pocketbase/core"
	m "github.com/pocketbase/pocketbase/migrations"
)

// Creates the notification_mutes collection: a generic, per-resource silence list that
// suppresses BOTH the in-app bell and external channel delivery for a single monitor,
// host (agent), or container. It is intentionally schema-free of foreign keys — the
// resource is identified by (resource_type, resource_id) plain text so the same table
// covers containers, which have no first-class collection record (their identity is
// "<agentID>|<containerID>", matching the container_image audit event resource id).
//
// Because there is no FK relation, deleting a monitor/agent/container leaves an orphan
// mute row behind; this is harmless (no event ever references it again) and avoids the
// migration lexical-ordering trap (a relation to monitors/agents in a "33_" file would
// run before "3_create_monitors.go"). The unique index makes muting idempotent (upsert).
//
// Rules: any authenticated user may read; non-readonly users may create/update/delete,
// so the frontend uses pb.collection("notification_mutes") directly (like host tags).
func init() {
	m.Register(func(app core.App) error {
		jsonData := `[
	{
		"id": "pbc_9000000001",
		"listRule": "@request.auth.id != \"\"",
		"viewRule": "@request.auth.id != \"\"",
		"createRule": "@request.auth.id != \"\" && @request.auth.role != \"readonly\"",
		"updateRule": "@request.auth.id != \"\" && @request.auth.role != \"readonly\"",
		"deleteRule": "@request.auth.id != \"\" && @request.auth.role != \"readonly\"",
		"name": "notification_mutes",
		"type": "base",
		"fields": [
			{
				"autogeneratePattern": "[a-z0-9]{15}",
				"hidden": false,
				"id": "text9000000000",
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
				"id": "select9000000001",
				"maxSelect": 1,
				"name": "resource_type",
				"presentable": false,
				"required": true,
				"system": false,
				"type": "select",
				"values": ["monitor", "agent", "container_image"]
			},
			{
				"hidden": false,
				"id": "text9000000002",
				"name": "resource_id",
				"presentable": true,
				"required": true,
				"system": false,
				"type": "text"
			},
			{
				"hidden": false,
				"id": "date9000000003",
				"max": "",
				"min": "",
				"name": "muted_until",
				"presentable": false,
				"required": false,
				"system": false,
				"type": "date"
			},
			{
				"cascadeDelete": true,
				"collectionId": "_pb_users_auth_",
				"hidden": false,
				"id": "relation9000000004",
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
				"id": "text9000000005",
				"name": "note",
				"presentable": false,
				"required": false,
				"system": false,
				"type": "text"
			},
			{
				"hidden": false,
				"id": "autodate9000000006",
				"name": "created",
				"onCreate": true,
				"onUpdate": false,
				"presentable": false,
				"system": false,
				"type": "autodate"
			},
			{
				"hidden": false,
				"id": "autodate9000000007",
				"name": "updated",
				"onCreate": true,
				"onUpdate": true,
				"presentable": false,
				"system": false,
				"type": "autodate"
			}
		],
		"indexes": [
			"CREATE UNIQUE INDEX ` + "`" + `idx_notification_mutes_resource` + "`" + ` ON ` + "`" + `notification_mutes` + "`" + ` (` + "`" + `resource_type` + "`" + `, ` + "`" + `resource_id` + "`" + `)"
		],
		"system": false
	}
]`
		return app.ImportCollectionsByMarshaledJSON([]byte(jsonData), false)
	}, nil)
}
