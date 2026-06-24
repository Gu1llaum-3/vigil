package migrations

import (
	"encoding/json"

	"github.com/pocketbase/pocketbase/core"
	m "github.com/pocketbase/pocketbase/migrations"
)

// Adds a free-text `tags` field (JSON array of strings) to the agents collection,
// for grouping/searching/filtering hosts. There is no managed tag catalog — tags
// are arbitrary strings set per host.
//
// NOTE ON FILE NAME: PocketBase applies migrations in lexical filename order (see
// docs/conventions-and-gotchas.md). The `agents` collection is created in
// `0_collections_snapshot_*.go`, which sorts first, so any prefix runs after it —
// the sequential `32_` is safe here (unlike collections created in mid-range
// single-digit migrations such as `3_create_monitors.go`).
func init() {
	m.Register(func(app core.App) error {
		collection, err := app.FindCollectionByNameOrId("agents")
		if err != nil {
			return err
		}

		data, err := collection.MarshalJSON()
		if err != nil {
			return err
		}

		var snapshot map[string]any
		if err := json.Unmarshal(data, &snapshot); err != nil {
			return err
		}

		fields, _ := snapshot["fields"].([]any)
		for _, raw := range fields {
			if existing, ok := raw.(map[string]any); ok && existing["name"] == "tags" {
				return nil // already present
			}
		}
		fields = append(fields, map[string]any{
			"hidden":      false,
			"id":          "jsonagenttags",
			"maxSize":     float64(50000),
			"name":        "tags",
			"presentable": false,
			"required":    false,
			"system":      false,
			"type":        "json",
		})
		snapshot["fields"] = fields

		updated, err := json.Marshal(snapshot)
		if err != nil {
			return err
		}
		if err := collection.UnmarshalJSON(updated); err != nil {
			return err
		}
		return app.Save(collection)
	}, nil)
}
