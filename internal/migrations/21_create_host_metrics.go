package migrations

import (
	"github.com/pocketbase/pocketbase/core"
	m "github.com/pocketbase/pocketbase/migrations"
)

func init() {
	m.Register(func(app core.App) error {
		jsonData := `[
	{
		"id": "pbc_7000000001",
		"listRule": "@request.auth.id != \"\"",
		"viewRule": "@request.auth.id != \"\"",
		"createRule": null,
		"updateRule": null,
		"deleteRule": null,
		"name": "host_metric_samples",
		"type": "base",
		"fields": [
			{
				"autogeneratePattern": "[a-z0-9]{15}",
				"hidden": false,
				"id": "text7100000000",
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
				"id": "relation7100000001",
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
				"id": "number7100000001",
				"name": "cpu_percent",
				"presentable": false,
				"required": false,
				"system": false,
				"type": "number"
			},
			{
				"hidden": false,
				"id": "number7100000002",
				"name": "memory_total_bytes",
				"presentable": false,
				"required": false,
				"system": false,
				"type": "number"
			},
			{
				"hidden": false,
				"id": "number7100000003",
				"name": "memory_used_bytes",
				"presentable": false,
				"required": false,
				"system": false,
				"type": "number"
			},
			{
				"hidden": false,
				"id": "number7100000004",
				"name": "memory_used_percent",
				"presentable": false,
				"required": false,
				"system": false,
				"type": "number"
			},
			{
				"hidden": false,
				"id": "number7100000005",
				"name": "disk_total_bytes",
				"presentable": false,
				"required": false,
				"system": false,
				"type": "number"
			},
			{
				"hidden": false,
				"id": "number7100000006",
				"name": "disk_used_bytes",
				"presentable": false,
				"required": false,
				"system": false,
				"type": "number"
			},
			{
				"hidden": false,
				"id": "number7100000007",
				"name": "disk_used_percent",
				"presentable": false,
				"required": false,
				"system": false,
				"type": "number"
			},
			{
				"hidden": false,
				"id": "number7100000008",
				"name": "network_rx_bps",
				"presentable": false,
				"required": false,
				"system": false,
				"type": "number"
			},
			{
				"hidden": false,
				"id": "number7100000009",
				"name": "network_tx_bps",
				"presentable": false,
				"required": false,
				"system": false,
				"type": "number"
			},
			{
				"hidden": false,
				"id": "date7100000001",
				"max": "",
				"min": "",
				"name": "collected_at",
				"presentable": false,
				"required": true,
				"system": false,
				"type": "date"
			},
			{
				"hidden": false,
				"id": "autodate7100000001",
				"name": "created",
				"onCreate": true,
				"onUpdate": false,
				"presentable": false,
				"system": false,
				"type": "autodate"
			},
			{
				"hidden": false,
				"id": "autodate7100000002",
				"name": "updated",
				"onCreate": true,
				"onUpdate": true,
				"presentable": false,
				"system": false,
				"type": "autodate"
			}
		],
		"indexes": [
			"CREATE INDEX ` + "`" + `idx_host_metric_samples_agent_collected` + "`" + ` ON ` + "`" + `host_metric_samples` + "`" + ` (` + "`" + `agent` + "`" + `, ` + "`" + `collected_at` + "`" + `)"
		],
		"system": false
	},
	{
		"id": "pbc_7000000002",
		"listRule": "@request.auth.id != \"\"",
		"viewRule": "@request.auth.id != \"\"",
		"createRule": null,
		"updateRule": null,
		"deleteRule": null,
		"name": "host_metric_current",
		"type": "base",
		"fields": [
			{
				"autogeneratePattern": "[a-z0-9]{15}",
				"hidden": false,
				"id": "text7200000000",
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
				"id": "relation7200000001",
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
				"id": "number7200000001",
				"name": "cpu_percent",
				"presentable": false,
				"required": false,
				"system": false,
				"type": "number"
			},
			{
				"hidden": false,
				"id": "number7200000002",
				"name": "memory_total_bytes",
				"presentable": false,
				"required": false,
				"system": false,
				"type": "number"
			},
			{
				"hidden": false,
				"id": "number7200000003",
				"name": "memory_used_bytes",
				"presentable": false,
				"required": false,
				"system": false,
				"type": "number"
			},
			{
				"hidden": false,
				"id": "number7200000004",
				"name": "memory_used_percent",
				"presentable": false,
				"required": false,
				"system": false,
				"type": "number"
			},
			{
				"hidden": false,
				"id": "number7200000005",
				"name": "disk_total_bytes",
				"presentable": false,
				"required": false,
				"system": false,
				"type": "number"
			},
			{
				"hidden": false,
				"id": "number7200000006",
				"name": "disk_used_bytes",
				"presentable": false,
				"required": false,
				"system": false,
				"type": "number"
			},
			{
				"hidden": false,
				"id": "number7200000007",
				"name": "disk_used_percent",
				"presentable": false,
				"required": false,
				"system": false,
				"type": "number"
			},
			{
				"hidden": false,
				"id": "number7200000008",
				"name": "network_rx_bps",
				"presentable": false,
				"required": false,
				"system": false,
				"type": "number"
			},
			{
				"hidden": false,
				"id": "number7200000009",
				"name": "network_tx_bps",
				"presentable": false,
				"required": false,
				"system": false,
				"type": "number"
			},
			{
				"hidden": false,
				"id": "date7200000001",
				"max": "",
				"min": "",
				"name": "collected_at",
				"presentable": false,
				"required": true,
				"system": false,
				"type": "date"
			},
			{
				"hidden": false,
				"id": "autodate7200000001",
				"name": "created",
				"onCreate": true,
				"onUpdate": false,
				"presentable": false,
				"system": false,
				"type": "autodate"
			},
			{
				"hidden": false,
				"id": "autodate7200000002",
				"name": "updated",
				"onCreate": true,
				"onUpdate": true,
				"presentable": false,
				"system": false,
				"type": "autodate"
			}
		],
		"indexes": [
			"CREATE UNIQUE INDEX ` + "`" + `idx_host_metric_current_agent` + "`" + ` ON ` + "`" + `host_metric_current` + "`" + ` (` + "`" + `agent` + "`" + `)"
		],
		"system": false
	}
]`
		return app.ImportCollectionsByMarshaledJSON([]byte(jsonData), false)
	}, nil)
}
