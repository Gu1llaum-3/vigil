package migrations

import (
	"github.com/pocketbase/pocketbase/core"
	m "github.com/pocketbase/pocketbase/migrations"
)

// Agent identity (per-agent tokens):
//   - token_issued marks a token the hub minted for this agent (SetAgentToken or a rotation).
//     A capable agent whose record lacks it is still on a shared token and gets one issued.
//   - status "awaiting_approval" + duplicate_of: a host that enrolled with the enrollment
//     token using the fingerprint of a host that has its own token (a reinstall, or someone
//     posing as it). Nothing is collected from it until an admin approves it as a new host,
//     merges it into duplicate_of, or rejects it.
func init() {
	m.Register(func(app core.App) error {
		collection, err := app.FindCollectionByNameOrId("agents")
		if err != nil {
			return nil
		}
		if status, ok := collection.Fields.GetByName("status").(*core.SelectField); ok {
			found := false
			for _, v := range status.Values {
				if v == "awaiting_approval" {
					found = true
				}
			}
			if !found {
				status.Values = append(status.Values, "awaiting_approval")
			}
		}
		if collection.Fields.GetByName("token_issued") == nil {
			collection.Fields.Add(&core.BoolField{Name: "token_issued"})
		}
		if collection.Fields.GetByName("duplicate_of") == nil {
			collection.Fields.Add(&core.RelationField{
				Name:          "duplicate_of",
				CollectionId:  collection.Id,
				MaxSelect:     1,
				CascadeDelete: true,
			})
		}
		return app.Save(collection)
	}, nil)
}
