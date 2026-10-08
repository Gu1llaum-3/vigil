//go:build testing

package hub_test

import (
	"net/http"
	"testing"

	appTests "github.com/Gu1llaum-3/vigil/internal/tests"
	pbTests "github.com/pocketbase/pocketbase/tests"
	"github.com/stretchr/testify/require"
)

// Non-readonly users may edit agents through the collection API (tags, fingerprint reset),
// but never the fields that decide an agent's identity.
func TestAgentIdentityFieldsAreHubManaged(t *testing.T) {
	hub, _ := appTests.NewTestHub(t.TempDir())
	defer hub.Cleanup()
	_ = hub.StartHub()

	user, err := appTests.CreateUser(hub, "operator@example.com", "password123")
	require.NoError(t, err)
	auth, err := user.NewAuthToken()
	require.NoError(t, err)
	host, err := appTests.CreateRecord(hub, "agents", map[string]any{"name": "web1", "token": "t1", "fingerprint": "fp1", "status": "connected", "token_issued": true})
	require.NoError(t, err)
	pending, err := appTests.CreateRecord(hub, "agents", map[string]any{"name": "web1", "token": "e", "fingerprint": "fp1", "status": "awaiting_approval", "duplicate_of": host.Id})
	require.NoError(t, err)

	factory := func(t testing.TB) *pbTests.TestApp { return hub.TestApp }
	patch := func(name, id string, body map[string]any, status int, expect string) appTests.ApiScenario {
		return appTests.ApiScenario{
			Name:            name,
			Method:          http.MethodPatch,
			URL:             "/api/collections/agents/records/" + id,
			Body:            jsonReader(body),
			Headers:         map[string]string{"Authorization": auth},
			ExpectedStatus:  status,
			ExpectedContent: []string{expect},
			TestAppFactory:  factory,
		}
	}
	for _, sc := range []appTests.ApiScenario{
		patch("tags stay editable", host.Id, map[string]any{"tags": []string{"prod"}}, 200, `"prod"`),
		patch("fingerprint reset of a host with its own token is admin-only", host.Id, map[string]any{"fingerprint": ""}, 403, "Only an admin"),
		patch("status is hub-managed", pending.Id, map[string]any{"status": "connected"}, 403, "managed by the hub"),
		patch("duplicate_of is hub-managed", pending.Id, map[string]any{"duplicate_of": ""}, 403, "managed by the hub"),
		patch("token_issued is hub-managed", host.Id, map[string]any{"token_issued": false}, 403, "managed by the hub"),
		patch("pending keeps its fingerprint", pending.Id, map[string]any{"fingerprint": ""}, 403, "keeps its fingerprint"),
	} {
		sc.Test(t)
	}

	// token is a hidden field: PocketBase ignores it in a non-superuser body.
	plant := patch("token cannot be planted", host.Id, map[string]any{"token": "planted-token"}, 200, `"id"`)
	plant.Test(t)
	stored, err := hub.FindRecordById("agents", host.Id)
	require.NoError(t, err)
	require.Equal(t, "t1", stored.GetString("token"))
}
