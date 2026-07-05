package migrations

import (
	"github.com/pocketbase/pocketbase/core"
	m "github.com/pocketbase/pocketbase/migrations"
)

// Creates the disk_monitor_rules collection: a per-host policy for which filesystems count
// toward the "worst monitored mount" that drives the disk bar and the (single) disk alert.
// `mode` is all (default, every mount) / include (only listed) / exclude (all but listed);
// `mounts` is the JSON list of mountpoints. One row per agent (unique index). The agent is a
// relation to `agents` (created in the 0_ snapshot, so it exists at import time regardless of
// the migration filename order). Admin-only: the rules are null (superuser) and access is
// gated by requireAdminRole on /api/app/disk-monitor-rules, exactly like metric_alerts.
func init() {
	m.Register(func(app core.App) error {
		jsonData := `[
	{
		"id": "pbc_7000000005",
		"name": "disk_monitor_rules",
		"type": "base",
		"fields": [
			{
				"autogeneratePattern": "[a-z0-9]{15}",
				"hidden": false,
				"id": "text7500000000",
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
				"id": "relation7500000001",
				"maxSelect": 1,
				"minSelect": 1,
				"name": "agent",
				"presentable": false,
				"required": true,
				"system": false,
				"type": "relation"
			},
			{
				"hidden": false,
				"id": "select7500000002",
				"maxSelect": 1,
				"name": "mode",
				"presentable": false,
				"required": true,
				"system": false,
				"type": "select",
				"values": ["all", "include", "exclude"]
			},
			{
				"hidden": false,
				"id": "json7500000003",
				"maxSize": 20000,
				"name": "mounts",
				"presentable": false,
				"required": false,
				"system": false,
				"type": "json"
			},
			{
				"hidden": false,
				"id": "autodate7500000004",
				"name": "created",
				"onCreate": true,
				"onUpdate": false,
				"presentable": false,
				"system": false,
				"type": "autodate"
			},
			{
				"hidden": false,
				"id": "autodate7500000005",
				"name": "updated",
				"onCreate": true,
				"onUpdate": true,
				"presentable": false,
				"system": false,
				"type": "autodate"
			}
		],
		"indexes": [
			"CREATE UNIQUE INDEX ` + "`" + `idx_disk_monitor_rules_agent` + "`" + ` ON ` + "`" + `disk_monitor_rules` + "`" + ` (` + "`" + `agent` + "`" + `)"
		],
		"system": false
	}
]`
		return app.ImportCollectionsByMarshaledJSON([]byte(jsonData), false)
	}, nil)
}
