package migrations

import (
	"encoding/json"

	"github.com/pocketbase/pocketbase/core"
	m "github.com/pocketbase/pocketbase/migrations"
)

// Adds `ip_family` (auto / ipv4 / ipv6) to monitors, letting HTTP and TCP checks pin the
// connection to a single IP family instead of the default dual-stack (Happy Eyeballs). Same
// select shape/values as the existing `ping_ip_family`. The `9_` prefix is deliberate: fresh
// DBs apply migrations in lexical order, so a two-digit prefix would sort before
// 3_create_monitors.go and the field-add would silently skip (collection not yet created);
// a single digit > 3 lands after it (see docs/conventions-and-gotchas.md).
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
		for _, raw := range fields {
			if existing, ok := raw.(map[string]any); ok && existing["name"] == "ip_family" {
				return nil // already added
			}
		}
		fields = append(fields, map[string]any{
			"hidden":      false,
			"id":          "select5200000020",
			"name":        "ip_family",
			"presentable": false,
			"required":    false,
			"system":      false,
			"type":        "select",
			"maxSelect":   1,
			"values":      []string{"", "ipv4", "ipv6"},
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
