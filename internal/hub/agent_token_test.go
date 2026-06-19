//go:build testing

package hub

import (
	"encoding/json"
	"testing"
)

// TestAgentTokenFieldHidden locks the #1 fix: migration 27 hides the agents.token field
// so PocketBase strips it from generic collection API responses and realtime events.
func TestAgentTokenFieldHidden(t *testing.T) {
	hub, testApp, err := createTestHub(t)
	if err != nil {
		t.Fatal(err)
	}
	defer cleanupTestHub(hub, testApp)

	col, err := hub.FindCollectionByNameOrId("agents")
	if err != nil {
		t.Fatalf("agents collection: %v", err)
	}
	data, err := col.MarshalJSON()
	if err != nil {
		t.Fatal(err)
	}
	var snap struct {
		Fields []struct {
			Name   string `json:"name"`
			Hidden bool   `json:"hidden"`
		} `json:"fields"`
	}
	if err := json.Unmarshal(data, &snap); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, f := range snap.Fields {
		if f.Name == "token" {
			found = true
			if !f.Hidden {
				t.Fatal("agents.token must be hidden so it is not exposed via the collection API")
			}
		}
	}
	if !found {
		t.Fatal("agents.token field not found")
	}
}
