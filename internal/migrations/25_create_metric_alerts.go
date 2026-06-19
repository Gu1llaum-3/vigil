package migrations

import (
	"github.com/pocketbase/pocketbase/core"
	m "github.com/pocketbase/pocketbase/migrations"
)

// metric_alerts holds host metric-threshold alert definitions. A row with an
// empty `agent` is the global default for that metric; a row with an agent is a
// per-agent override. Admin-only: collection rules are null and access is gated
// by the requireAdminRole middleware on /api/app/metric-alerts.
func init() {
	m.Register(func(app core.App) error {
		jsonData := `[
	{
		"id": "pbc_7000000004",
		"listRule": null,
		"viewRule": null,
		"createRule": null,
		"updateRule": null,
		"deleteRule": null,
		"name": "metric_alerts",
		"type": "base",
		"fields": [
			{
				"autogeneratePattern": "[a-z0-9]{15}",
				"hidden": false,
				"id": "text7400000000",
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
				"id": "relation7400000001",
				"maxSelect": 1,
				"minSelect": 0,
				"name": "agent",
				"presentable": false,
				"required": false,
				"system": false,
				"type": "relation"
			},
			{
				"hidden": false,
				"id": "select7400000001",
				"name": "metric",
				"presentable": false,
				"required": true,
				"system": false,
				"type": "select",
				"maxSelect": 1,
				"values": ["cpu", "memory", "disk", "loadavg"]
			},
			{
				"hidden": false,
				"id": "bool7400000001",
				"name": "enabled",
				"presentable": false,
				"required": false,
				"system": false,
				"type": "bool"
			},
			{
				"hidden": false,
				"id": "number7400000001",
				"name": "warning_value",
				"presentable": false,
				"required": false,
				"system": false,
				"type": "number",
				"min": 0
			},
			{
				"hidden": false,
				"id": "number7400000002",
				"name": "critical_value",
				"presentable": false,
				"required": false,
				"system": false,
				"type": "number",
				"min": 0
			},
			{
				"hidden": false,
				"id": "number7400000003",
				"name": "hysteresis",
				"presentable": false,
				"required": false,
				"system": false,
				"type": "number",
				"min": 0
			},
			{
				"hidden": false,
				"id": "autodate7400000001",
				"name": "created",
				"onCreate": true,
				"onUpdate": false,
				"presentable": false,
				"system": false,
				"type": "autodate"
			},
			{
				"hidden": false,
				"id": "autodate7400000002",
				"name": "updated",
				"onCreate": true,
				"onUpdate": true,
				"presentable": false,
				"system": false,
				"type": "autodate"
			}
		],
		"indexes": [
			"CREATE UNIQUE INDEX ` + "`" + `idx_metric_alerts_agent_metric` + "`" + ` ON ` + "`" + `metric_alerts` + "`" + ` (` + "`" + `agent` + "`" + `, ` + "`" + `metric` + "`" + `)"
		],
		"system": false
	}
]`
		return app.ImportCollectionsByMarshaledJSON([]byte(jsonData), false)
	}, nil)
}
