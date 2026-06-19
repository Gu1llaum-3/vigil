package migrations

import (
	"encoding/json"

	"github.com/pocketbase/pocketbase/core"
	m "github.com/pocketbase/pocketbase/migrations"
)

// Hides the agents.token field so PocketBase strips it from generic collection API
// responses and realtime events. The token is the per-agent authentication secret; with
// the collection readable by every authenticated user (incl. readonly), it was being
// exposed fleet-wide. Internal hub code reads the token via direct SQL (getAgentsByToken),
// so the handshake is unaffected; the admin UI fetches tokens via /api/app/agent-tokens.
func init() {
	m.Register(func(app core.App) error {
		collection, err := app.FindCollectionByNameOrId("agents")
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
			if ok && f["name"] == "token" && f["hidden"] != true {
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
