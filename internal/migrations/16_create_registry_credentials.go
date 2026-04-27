package migrations

import (
	"github.com/pocketbase/pocketbase/core"
	m "github.com/pocketbase/pocketbase/migrations"
)

func init() {
	m.Register(func(app core.App) error {
		jsonData := `[
	{
		"id": "pbc_6000000007",
		"listRule": null,
		"viewRule": null,
		"createRule": null,
		"updateRule": null,
		"deleteRule": null,
		"name": "registry_credentials",
		"type": "base",
		"fields": [
			{
				"autogeneratePattern": "[a-z0-9]{15}",
				"hidden": false,
				"id": "text6700000000",
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
				"id": "text6700000001",
				"name": "name",
				"presentable": true,
				"required": true,
				"system": false,
				"type": "text"
			},
			{
				"hidden": false,
				"id": "text6700000002",
				"name": "registry",
				"presentable": true,
				"required": true,
				"system": false,
				"type": "text"
			},
			{
				"hidden": false,
				"id": "text6700000003",
				"name": "username",
				"presentable": false,
				"required": true,
				"system": false,
				"type": "text"
			},
			{
				"hidden": true,
				"id": "text6700000004",
				"name": "password_ciphertext",
				"presentable": false,
				"required": true,
				"system": false,
				"type": "text"
			},
			{
				"hidden": true,
				"id": "text6700000005",
				"name": "password_nonce",
				"presentable": false,
				"required": true,
				"system": false,
				"type": "text"
			},
			{
				"hidden": false,
				"id": "autodate6700000001",
				"name": "created",
				"onCreate": true,
				"onUpdate": false,
				"presentable": false,
				"system": false,
				"type": "autodate"
			},
			{
				"hidden": false,
				"id": "autodate6700000002",
				"name": "updated",
				"onCreate": true,
				"onUpdate": true,
				"presentable": false,
				"system": false,
				"type": "autodate"
			}
		],
		"indexes": [
			"CREATE UNIQUE INDEX ` + "`" + `idx_registry_credentials_registry` + "`" + ` ON ` + "`" + `registry_credentials` + "`" + ` (` + "`" + `registry` + "`" + `)"
		],
		"system": false
	}
]`
		return app.ImportCollectionsByMarshaledJSON([]byte(jsonData), false)
	}, nil)
}
