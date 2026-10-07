package migrations

import (
	"encoding/json"

	"github.com/pocketbase/pocketbase/core"
	m "github.com/pocketbase/pocketbase/migrations"
)

// Hides monitors.push_token so PocketBase strips it from generic collection API responses
// and realtime events. The collection is readable by every authenticated user (readonly
// included) and the scheduler saves each monitor on every check, so the token, enough to send
// heartbeats and keep a dead job's monitor "up", was broadcast to every open browser. Hub code
// reads it from the record directly; editors get the push URL from /api/app/monitors.
//
// The single-digit prefix is deliberate: migrations run in lexical filename order, and this
// one must run after 3_create_monitors.go (see docs/conventions-and-gotchas.md).
func init() {
	m.Register(func(app core.App) error {
		collection, err := app.FindCollectionByNameOrId("monitors")
		if err != nil {
			return nil // collection missing → nothing to migrate
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
		changed := false
		for _, raw := range fields {
			f, ok := raw.(map[string]any)
			if ok && f["name"] == "push_token" && f["hidden"] != true {
				f["hidden"] = true
				changed = true
			}
		}
		if !changed {
			return nil
		}
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
