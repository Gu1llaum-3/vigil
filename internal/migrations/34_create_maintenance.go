package migrations

import (
	"github.com/pocketbase/pocketbase/core"
	m "github.com/pocketbase/pocketbase/migrations"
)

// Creates the maintenance collection: planned maintenance windows that (a) suppress
// notifications for the covered resources while active and (b) drive the global
// "maintenance in progress" banner. A window is either one-time (single: absolute
// start_at/end_at) or recurring (daily/weekly: local start_time/end_time in `timezone`,
// optional weekdays + active_from/active_to date bounds). Scope is global ({} ) or
// targeted ({monitor_ids:[…], agent_ids:[…]}, same shape as notification_rules.filter).
//
// Writes are admin-only: create/update/delete rules are null and all writes go through the
// requireAdminRole-gated /api/app/maintenance-windows handlers (which use app.Save and
// bypass collection rules). Read (list/view) is open to any authenticated user so the
// frontend banner can subscribe over realtime and react instantly to a teammate's change —
// the same pattern agents/monitors already use; the banner still renders from the slim
// authenticated /api/app/maintenance/active endpoint and uses the collection only as a
// change signal. scope/weekdays are plain JSON (no FK) — a stale id after a monitor/agent is
// deleted is harmless (no window references it directly), and it keeps the migration free of
// the lexical-ordering relation trap.
func init() {
	m.Register(func(app core.App) error {
		jsonData := `[
	{
		"id": "pbc_9000000002",
		"listRule": "@request.auth.id != \"\"",
		"viewRule": "@request.auth.id != \"\"",
		"createRule": null,
		"updateRule": null,
		"deleteRule": null,
		"name": "maintenance",
		"type": "base",
		"fields": [
			{
				"autogeneratePattern": "[a-z0-9]{15}",
				"hidden": false,
				"id": "text9100000000",
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
				"id": "text9100000001",
				"name": "title",
				"presentable": true,
				"required": true,
				"system": false,
				"type": "text"
			},
			{
				"hidden": false,
				"id": "text9100000002",
				"name": "description",
				"presentable": false,
				"required": false,
				"system": false,
				"type": "text"
			},
			{
				"hidden": false,
				"id": "bool9100000003",
				"name": "enabled",
				"presentable": false,
				"required": false,
				"system": false,
				"type": "bool"
			},
			{
				"hidden": false,
				"id": "select9100000004",
				"maxSelect": 1,
				"name": "severity",
				"presentable": false,
				"required": false,
				"system": false,
				"type": "select",
				"values": ["info", "warning", "critical"]
			},
			{
				"hidden": false,
				"id": "select9100000005",
				"maxSelect": 1,
				"name": "strategy",
				"presentable": false,
				"required": false,
				"system": false,
				"type": "select",
				"values": ["single", "recurring"]
			},
			{
				"hidden": false,
				"id": "date9100000006",
				"max": "",
				"min": "",
				"name": "start_at",
				"presentable": false,
				"required": false,
				"system": false,
				"type": "date"
			},
			{
				"hidden": false,
				"id": "date9100000007",
				"max": "",
				"min": "",
				"name": "end_at",
				"presentable": false,
				"required": false,
				"system": false,
				"type": "date"
			},
			{
				"hidden": false,
				"id": "text9100000008",
				"name": "start_time",
				"presentable": false,
				"required": false,
				"system": false,
				"type": "text"
			},
			{
				"hidden": false,
				"id": "text9100000009",
				"name": "end_time",
				"presentable": false,
				"required": false,
				"system": false,
				"type": "text"
			},
			{
				"hidden": false,
				"id": "json9100000010",
				"maxSize": 2000,
				"name": "weekdays",
				"presentable": false,
				"required": false,
				"system": false,
				"type": "json"
			},
			{
				"hidden": false,
				"id": "date9100000011",
				"max": "",
				"min": "",
				"name": "active_from",
				"presentable": false,
				"required": false,
				"system": false,
				"type": "date"
			},
			{
				"hidden": false,
				"id": "date9100000012",
				"max": "",
				"min": "",
				"name": "active_to",
				"presentable": false,
				"required": false,
				"system": false,
				"type": "date"
			},
			{
				"hidden": false,
				"id": "text9100000013",
				"name": "timezone",
				"presentable": false,
				"required": false,
				"system": false,
				"type": "text"
			},
			{
				"hidden": false,
				"id": "json9100000014",
				"maxSize": 4000,
				"name": "scope",
				"presentable": false,
				"required": false,
				"system": false,
				"type": "json"
			},
			{
				"cascadeDelete": true,
				"collectionId": "_pb_users_auth_",
				"hidden": false,
				"id": "relation9100000015",
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
				"id": "autodate9100000016",
				"name": "created",
				"onCreate": true,
				"onUpdate": false,
				"presentable": false,
				"system": false,
				"type": "autodate"
			},
			{
				"hidden": false,
				"id": "autodate9100000017",
				"name": "updated",
				"onCreate": true,
				"onUpdate": true,
				"presentable": false,
				"system": false,
				"type": "autodate"
			}
		],
		"indexes": [
			"CREATE INDEX ` + "`" + `idx_maintenance_enabled` + "`" + ` ON ` + "`" + `maintenance` + "`" + ` (` + "`" + `enabled` + "`" + `)"
		],
		"system": false
	}
]`
		return app.ImportCollectionsByMarshaledJSON([]byte(jsonData), false)
	}, nil)
}
