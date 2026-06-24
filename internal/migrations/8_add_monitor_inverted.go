package migrations

import (
	"encoding/json"

	"github.com/pocketbase/pocketbase/core"
	m "github.com/pocketbase/pocketbase/migrations"
)

// Adds the `inverted` flag to the monitors collection: when set, the scheduler
// flips up<->down so a monitor that is "up" when its target responds instead
// alerts when it responds (e.g. a maintenance page that should normally be
// unreachable).
//
// NOTE ON FILE NAME: PocketBase applies migrations in lexical filename order
// (core/migrations_list.go sorts by File string), NOT numeric order. The
// `monitors` collection is created in "3_create_monitors.go"; a two-digit prefix
// such as "32_" would sort BEFORE "3_" (because '2' < '_') and run before the
// collection exists. The single-digit migrations (2,3,4,6,7) sort last, so this
// monitor migration uses "8_" to run after monitors (and 4_add_monitor_failures)
// are in place. See docs/conventions-and-gotchas.md.
func init() {
	m.Register(func(app core.App) error {
		collection, err := app.FindCollectionByNameOrId("monitors")
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
			if existing, ok := raw.(map[string]any); ok && existing["name"] == "inverted" {
				return nil // already present
			}
		}
		fields = append(fields, map[string]any{
			"hidden":      false,
			"id":          "bool5200000003",
			"name":        "inverted",
			"presentable": false,
			"required":    false,
			"system":      false,
			"type":        "bool",
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
