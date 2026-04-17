package migrations

import (
	"github.com/pocketbase/pocketbase/core"
	m "github.com/pocketbase/pocketbase/migrations"
)

func init() {
	m.Register(func(app core.App) error {
		// update collections
		jsonData := `[
	{
		"id": "_pb_users_auth_",
		"listRule": "id = @request.auth.id",
		"viewRule": "id = @request.auth.id",
		"createRule": null,
		"updateRule": null,
		"deleteRule": null,
		"name": "users",
		"type": "auth",
		"fields": [
			{
				"autogeneratePattern": "[a-z0-9]{15}",
				"hidden": false,
				"id": "text3208210256",
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
				"cost": 10,
				"hidden": true,
				"id": "password901924565",
				"max": 0,
				"min": 8,
				"name": "password",
				"pattern": "",
				"presentable": false,
				"required": true,
				"system": true,
				"type": "password"
			},
			{
				"autogeneratePattern": "[a-zA-Z0-9_]{50}",
				"hidden": true,
				"id": "text2504183744",
				"max": 60,
				"min": 30,
				"name": "tokenKey",
				"pattern": "",
				"presentable": false,
				"primaryKey": false,
				"required": true,
				"system": true,
				"type": "text"
			},
			{
				"exceptDomains": null,
				"hidden": false,
				"id": "email3885137012",
				"name": "email",
				"onlyDomains": null,
				"presentable": false,
				"required": true,
				"system": true,
				"type": "email"
			},
			{
				"hidden": false,
				"id": "bool1547992806",
				"name": "emailVisibility",
				"presentable": false,
				"required": false,
				"system": true,
				"type": "bool"
			},
			{
				"hidden": false,
				"id": "bool256245529",
				"name": "verified",
				"presentable": false,
				"required": false,
				"system": true,
				"type": "bool"
			},
			{
				"autogeneratePattern": "users[0-9]{6}",
				"hidden": false,
				"id": "text4166911607",
				"max": 150,
				"min": 3,
				"name": "username",
				"pattern": "^[\\w][\\w\\.\\-]*$",
				"presentable": false,
				"primaryKey": false,
				"required": false,
				"system": false,
				"type": "text"
			},
			{
				"hidden": false,
				"id": "qkbp58ae",
				"maxSelect": 1,
				"name": "role",
				"presentable": false,
				"required": false,
				"system": false,
				"type": "select",
				"values": [
					"user",
					"admin",
					"readonly"
				]
			},
			{
				"hidden": false,
				"id": "autodate2990389176",
				"name": "created",
				"onCreate": true,
				"onUpdate": false,
				"presentable": false,
				"system": false,
				"type": "autodate"
			},
			{
				"hidden": false,
				"id": "autodate3332085495",
				"name": "updated",
				"onCreate": true,
				"onUpdate": true,
				"presentable": false,
				"system": false,
				"type": "autodate"
			}
		],
		"indexes": [
			"CREATE UNIQUE INDEX ` + "`" + `__pb_users_auth__username_idx` + "`" + ` ON ` + "`" + `users` + "`" + ` (username COLLATE NOCASE)",
			"CREATE UNIQUE INDEX ` + "`" + `__pb_users_auth__email_idx` + "`" + ` ON ` + "`" + `users` + "`" + ` (` + "`" + `email` + "`" + `) WHERE ` + "`" + `email` + "`" + ` != ''",
			"CREATE UNIQUE INDEX ` + "`" + `__pb_users_auth__tokenKey_idx` + "`" + ` ON ` + "`" + `users` + "`" + ` (` + "`" + `tokenKey` + "`" + `)"
		],
		"system": false,
		"authRule": "verified=true",
		"manageRule": null
	},
	{
		"id": "4afacsdnlu8q8r2",
		"listRule": "@request.auth.id != \"\" && user = @request.auth.id",
		"viewRule": null,
		"createRule": "@request.auth.id != \"\" && user = @request.auth.id",
		"updateRule": "@request.auth.id != \"\" && user = @request.auth.id",
		"deleteRule": null,
		"name": "user_settings",
		"type": "base",
		"fields": [
			{
				"autogeneratePattern": "[a-z0-9]{15}",
				"hidden": false,
				"id": "text3208210256",
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
				"collectionId": "_pb_users_auth_",
				"hidden": false,
				"id": "d5vztyxa",
				"maxSelect": 1,
				"minSelect": 0,
				"name": "user",
				"presentable": false,
				"required": true,
				"system": false,
				"type": "relation"
			},
			{
				"hidden": false,
				"id": "xcx4qgqq",
				"maxSize": 2000000,
				"name": "settings",
				"presentable": false,
				"required": false,
				"system": false,
				"type": "json"
			},
			{
				"hidden": false,
				"id": "autodate2990389176",
				"name": "created",
				"onCreate": true,
				"onUpdate": false,
				"presentable": false,
				"system": false,
				"type": "autodate"
			},
			{
				"hidden": false,
				"id": "autodate3332085495",
				"name": "updated",
				"onCreate": true,
				"onUpdate": true,
				"presentable": false,
				"system": false,
				"type": "autodate"
			}
		],
		"indexes": [
			"CREATE UNIQUE INDEX ` + "`" + `idx_30Lwgf2` + "`" + ` ON ` + "`" + `user_settings` + "`" + ` (` + "`" + `user` + "`" + `)"
		],
		"system": false
	},
	{
		"id": "pbc_4000000001",
		"listRule": "@request.auth.id != \"\"",
		"viewRule": "@request.auth.id != \"\"",
		"createRule": null,
		"updateRule": "@request.auth.id != \"\" && @request.auth.role != \"readonly\"",
		"deleteRule": "@request.auth.id != \"\" && @request.auth.role != \"readonly\"",
		"name": "agents",
		"type": "base",
		"fields": [
			{
				"autogeneratePattern": "[a-z0-9]{15}",
				"hidden": false,
				"id": "text3208210256",
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
				"autogeneratePattern": "",
				"hidden": false,
				"id": "text1579384326",
				"max": 0,
				"min": 0,
				"name": "name",
				"pattern": "",
				"presentable": true,
				"primaryKey": false,
				"required": false,
				"system": false,
				"type": "text"
			},
			{
				"autogeneratePattern": "",
				"hidden": false,
				"id": "text1597481275",
				"max": 255,
				"min": 1,
				"name": "token",
				"pattern": "",
				"presentable": false,
				"primaryKey": false,
				"required": true,
				"system": false,
				"type": "text"
			},
			{
				"autogeneratePattern": "",
				"hidden": false,
				"id": "text4228609354",
				"max": 255,
				"min": 0,
				"name": "fingerprint",
				"pattern": "",
				"presentable": false,
				"primaryKey": false,
				"required": false,
				"system": false,
				"type": "text"
			},
			{
				"hidden": false,
				"id": "select2844932856",
				"maxSelect": 1,
				"name": "status",
				"presentable": false,
				"required": false,
				"system": false,
				"type": "select",
				"values": [
					"pending",
					"connected",
					"offline"
				]
			},
			{
				"autogeneratePattern": "",
				"hidden": false,
				"id": "text2063623452",
				"max": 50,
				"min": 0,
				"name": "version",
				"pattern": "",
				"presentable": false,
				"primaryKey": false,
				"required": false,
				"system": false,
				"type": "text"
			},
			{
				"hidden": false,
				"id": "date2675529103",
				"max": "",
				"min": "",
				"name": "last_seen",
				"presentable": false,
				"required": false,
				"system": false,
				"type": "date"
			},
			{
				"hidden": false,
				"id": "json832282224",
				"maxSize": 2000000,
				"name": "capabilities",
				"presentable": false,
				"required": false,
				"system": false,
				"type": "json"
			},
			{
				"hidden": false,
				"id": "json832282225",
				"maxSize": 2000000,
				"name": "metadata",
				"presentable": false,
				"required": false,
				"system": false,
				"type": "json"
			},
			{
				"cascadeDelete": true,
				"collectionId": "_pb_users_auth_",
				"hidden": false,
				"id": "relation2375276105",
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
				"id": "autodate2990389176",
				"name": "created",
				"onCreate": true,
				"onUpdate": false,
				"presentable": false,
				"system": false,
				"type": "autodate"
			},
			{
				"hidden": false,
				"id": "autodate3332085495",
				"name": "updated",
				"onCreate": true,
				"onUpdate": true,
				"presentable": false,
				"system": false,
				"type": "autodate"
			}
		],
		"indexes": [
			"CREATE INDEX ` + "`" + `idx_agents_token` + "`" + ` ON ` + "`" + `agents` + "`" + ` (` + "`" + `token` + "`" + `)",
			"CREATE INDEX ` + "`" + `idx_agents_status` + "`" + ` ON ` + "`" + `agents` + "`" + ` (` + "`" + `status` + "`" + `)"
		],
		"system": false
	},
	{
		"id": "pbc_4000000002",
		"listRule": null,
		"viewRule": null,
		"createRule": null,
		"updateRule": null,
		"deleteRule": null,
		"name": "agent_enrollment_tokens",
		"type": "base",
		"fields": [
			{
				"autogeneratePattern": "[a-z0-9]{10}",
				"hidden": false,
				"id": "text3208210256",
				"max": 10,
				"min": 10,
				"name": "id",
				"pattern": "^[a-z0-9]+$",
				"presentable": false,
				"primaryKey": true,
				"required": true,
				"system": true,
				"type": "text"
			},
			{
				"autogeneratePattern": "",
				"hidden": false,
				"id": "text1579384326",
				"max": 0,
				"min": 0,
				"name": "name",
				"pattern": "",
				"presentable": false,
				"primaryKey": false,
				"required": false,
				"system": false,
				"type": "text"
			},
			{
				"cascadeDelete": true,
				"collectionId": "_pb_users_auth_",
				"hidden": false,
				"id": "relation2375276105",
				"maxSelect": 1,
				"minSelect": 0,
				"name": "created_by",
				"presentable": false,
				"required": true,
				"system": false,
				"type": "relation"
			},
			{
				"autogeneratePattern": "",
				"hidden": false,
				"id": "text1597481275",
				"max": 0,
				"min": 0,
				"name": "token",
				"pattern": "",
				"presentable": false,
				"primaryKey": false,
				"required": false,
				"system": false,
				"type": "text"
			},
			{
				"hidden": false,
				"id": "autodate2990389176",
				"name": "created",
				"onCreate": true,
				"onUpdate": false,
				"presentable": false,
				"system": false,
				"type": "autodate"
			}
		],
		"indexes": [
			"CREATE INDEX ` + "`" + `idx_aet_token` + "`" + ` ON ` + "`" + `agent_enrollment_tokens` + "`" + ` (` + "`" + `token` + "`" + `)",
			"CREATE UNIQUE INDEX ` + "`" + `idx_aet_user` + "`" + ` ON ` + "`" + `agent_enrollment_tokens` + "`" + ` (` + "`" + `created_by` + "`" + `)"
		],
		"system": false
	}
]`

		err := app.ImportCollectionsByMarshaledJSON([]byte(jsonData), true)
		if err != nil {
			return err
		}

		return nil
	}, func(app core.App) error {
		return nil
	})
}
