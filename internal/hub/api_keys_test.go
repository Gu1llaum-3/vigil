//go:build testing

package hub_test

import (
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"testing"

	appTests "github.com/Gu1llaum-3/vigil/internal/tests"

	"github.com/pocketbase/pocketbase/core"
	pbTests "github.com/pocketbase/pocketbase/tests"
	"github.com/stretchr/testify/require"
)

// createTestApiKey inserts a user_api_keys record for userID with the given scope and
// returns the plaintext token (hashing inline, which also asserts the middleware uses a
// plain hex SHA-256 of the token).
func createTestApiKey(t *testing.T, hub *appTests.TestHub, userID, scope string) string {
	t.Helper()
	token := "vk_" + scope + "_0123456789abcdef0123456789abcdef"
	col, err := hub.FindCachedCollectionByNameOrId("user_api_keys")
	require.NoError(t, err)
	rec := core.NewRecord(col)
	rec.Set("name", "test-key")
	rec.Set("created_by", userID)
	sum := sha256.Sum256([]byte(token))
	rec.Set("token_hash", hex.EncodeToString(sum[:]))
	rec.Set("scope", scope)
	require.NoError(t, hub.SaveNoValidate(rec))
	return token
}

func TestApiKeyAuthentication(t *testing.T) {
	hub, _ := appTests.NewTestHub(t.TempDir())
	defer hub.Cleanup()
	// StartHub registers the OnServe middleware/route bindings the scenario harness needs;
	// its pb.Start() fails on the test app ("not a pocketbase app"), which is expected.
	_ = hub.StartHub()

	user, err := appTests.CreateUser(hub, "keyuser@example.com", "password123")
	require.NoError(t, err)

	readToken := createTestApiKey(t, hub, user.Id, "read")
	rwToken := createTestApiKey(t, hub, user.Id, "read-write")
	userJWT, err := user.NewAuthToken()
	require.NoError(t, err)

	testAppFactory := func(t testing.TB) *pbTests.TestApp { return hub.TestApp }

	scenarios := []appTests.ApiScenario{
		{
			Name:            "read key authenticates a read endpoint",
			Method:          http.MethodGet,
			URL:             "/api/app/info",
			Headers:         map[string]string{"Authorization": readToken},
			ExpectedStatus:  200,
			ExpectedContent: []string{"\"key\":", "\"v\":"},
			TestAppFactory:  testAppFactory,
		},
		{
			Name:            "read key authenticates with a lowercase bearer scheme",
			Method:          http.MethodGet,
			URL:             "/api/app/info",
			Headers:         map[string]string{"Authorization": "bearer " + readToken},
			ExpectedStatus:  200,
			ExpectedContent: []string{"\"key\":"},
			TestAppFactory:  testAppFactory,
		},
		{
			Name:            "create with a malformed expires_at is rejected",
			Method:          http.MethodPost,
			URL:             "/api/app/api-keys",
			Headers:         map[string]string{"Authorization": userJWT},
			Body:            jsonReader(map[string]any{"name": "k", "expires_at": "not-a-date"}),
			ExpectedStatus:  400,
			ExpectedContent: []string{"Invalid expires_at"},
			TestAppFactory:  testAppFactory,
		},
		{
			Name:            "create with a past expires_at is rejected",
			Method:          http.MethodPost,
			URL:             "/api/app/api-keys",
			Headers:         map[string]string{"Authorization": userJWT},
			Body:            jsonReader(map[string]any{"name": "k", "expires_at": "2000-01-01T00:00:00Z"}),
			ExpectedStatus:  400,
			ExpectedContent: []string{"future"},
			TestAppFactory:  testAppFactory,
		},
		{
			Name:            "create with a valid name succeeds and returns the token once",
			Method:          http.MethodPost,
			URL:             "/api/app/api-keys",
			Headers:         map[string]string{"Authorization": userJWT},
			Body:            jsonReader(map[string]any{"name": "mcp"}),
			ExpectedStatus:  200,
			ExpectedContent: []string{"\"token\":\"vk_", "\"scope\":\"read\""},
			TestAppFactory:  testAppFactory,
		},
		{
			Name:            "read key is rejected on a write endpoint",
			Method:          http.MethodPost,
			URL:             "/api/app/monitors",
			Headers:         map[string]string{"Authorization": readToken},
			ExpectedStatus:  403,
			ExpectedContent: []string{"read-only"},
			TestAppFactory:  testAppFactory,
		},
		{
			Name:            "read key cannot create another API key (write blocked)",
			Method:          http.MethodPost,
			URL:             "/api/app/api-keys",
			Headers:         map[string]string{"Authorization": readToken},
			ExpectedStatus:  403,
			ExpectedContent: []string{"read-only"},
			TestAppFactory:  testAppFactory,
		},
		{
			Name:            "read-write key may not manage keys either",
			Method:          http.MethodPost,
			URL:             "/api/app/api-keys",
			Headers:         map[string]string{"Authorization": rwToken},
			ExpectedStatus:  403,
			ExpectedContent: []string{"cannot be managed with an API key"},
			TestAppFactory:  testAppFactory,
		},
		{
			Name:            "read-write key can list keys (read) for its owner",
			Method:          http.MethodGet,
			URL:             "/api/app/api-keys",
			Headers:         map[string]string{"Authorization": rwToken},
			ExpectedStatus:  200,
			ExpectedContent: []string{"["},
			TestAppFactory:  testAppFactory,
		},
		{
			Name:            "unknown API key is unauthorized",
			Method:          http.MethodGet,
			URL:             "/api/app/info",
			Headers:         map[string]string{"Authorization": "vk_unknown_key_value_000000000000000000"},
			ExpectedStatus:  401,
			ExpectedContent: []string{"requires valid"},
			TestAppFactory:  testAppFactory,
		},
	}

	for _, scenario := range scenarios {
		scenario.Test(t)
	}
}
