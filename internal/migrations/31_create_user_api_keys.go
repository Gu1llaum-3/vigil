package migrations

import (
	"github.com/pocketbase/pocketbase/core"
	m "github.com/pocketbase/pocketbase/migrations"
)

// Creates the user_api_keys collection: long-lived bearer tokens that let a non-browser
// client (scripts, the MCP server) authenticate to /api/app/* as the owning user without
// the short-lived browser JWT. Only the SHA-256 hash of the token is stored (the plaintext
// is shown once at creation); `prefix` keeps a display-only head of the token; `scope`
// gates read vs read-write. Collection rules are null — access is exclusively through the
// /api/app/api-keys handlers (per-user) and the authenticateApiKey middleware.
func init() {
	m.Register(func(app core.App) error {
		jsonData := `[
	{
		"id": "pbc_8000000001",
		"listRule": null,
		"viewRule": null,
		"createRule": null,
		"updateRule": null,
		"deleteRule": null,
		"name": "user_api_keys",
		"type": "base",
		"fields": [
			{
				"autogeneratePattern": "[a-z0-9]{15}",
				"hidden": false,
				"id": "text8000000000",
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
				"id": "text8000000001",
				"name": "name",
				"presentable": true,
				"required": true,
				"system": false,
				"type": "text"
			},
			{
				"cascadeDelete": true,
				"collectionId": "_pb_users_auth_",
				"hidden": false,
				"id": "relation8000000002",
				"maxSelect": 1,
				"minSelect": 0,
				"name": "created_by",
				"presentable": false,
				"required": false,
				"system": false,
				"type": "relation"
			},
			{
				"hidden": true,
				"id": "text8000000003",
				"name": "token_hash",
				"presentable": false,
				"required": true,
				"system": false,
				"type": "text"
			},
			{
				"hidden": false,
				"id": "text8000000004",
				"name": "prefix",
				"presentable": false,
				"required": false,
				"system": false,
				"type": "text"
			},
			{
				"hidden": false,
				"id": "select8000000005",
				"maxSelect": 1,
				"name": "scope",
				"presentable": false,
				"required": false,
				"system": false,
				"type": "select",
				"values": ["read", "read-write"]
			},
			{
				"hidden": false,
				"id": "date8000000006",
				"max": "",
				"min": "",
				"name": "last_used_at",
				"presentable": false,
				"required": false,
				"system": false,
				"type": "date"
			},
			{
				"hidden": false,
				"id": "date8000000007",
				"max": "",
				"min": "",
				"name": "expires_at",
				"presentable": false,
				"required": false,
				"system": false,
				"type": "date"
			},
			{
				"hidden": false,
				"id": "autodate8000000008",
				"name": "created",
				"onCreate": true,
				"onUpdate": false,
				"presentable": false,
				"system": false,
				"type": "autodate"
			},
			{
				"hidden": false,
				"id": "autodate8000000009",
				"name": "updated",
				"onCreate": true,
				"onUpdate": true,
				"presentable": false,
				"system": false,
				"type": "autodate"
			}
		],
		"indexes": [
			"CREATE UNIQUE INDEX ` + "`" + `idx_user_api_keys_token_hash` + "`" + ` ON ` + "`" + `user_api_keys` + "`" + ` (` + "`" + `token_hash` + "`" + `)",
			"CREATE INDEX ` + "`" + `idx_user_api_keys_created_by` + "`" + ` ON ` + "`" + `user_api_keys` + "`" + ` (` + "`" + `created_by` + "`" + `)"
		],
		"system": false
	}
]`
		return app.ImportCollectionsByMarshaledJSON([]byte(jsonData), false)
	}, nil)
}
