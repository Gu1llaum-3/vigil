//go:build testing

package hub_test

import (
	"net/http"
	"strings"
	"testing"

	appTests "github.com/Gu1llaum-3/vigil/internal/tests"

	pbTests "github.com/pocketbase/pocketbase/tests"
	"github.com/stretchr/testify/require"
)

// TestMcpEndpoint is the Phase-0 spike check: it proves the full chain works — the /api/mcp
// route is mounted on the hub router, an API key authenticates it (a read key is NOT blocked
// despite POST, thanks to the MCP exemption), and the MCP SDK handler answers the JSON-RPC
// initialize handshake with the "vigil" server identity.
func TestMcpEndpoint(t *testing.T) {
	hub, _ := appTests.NewTestHub(t.TempDir())
	defer hub.Cleanup()
	_ = hub.StartHub()

	user, err := appTests.CreateUser(hub, "mcpuser@example.com", "password123")
	require.NoError(t, err)
	readToken := createTestApiKey(t, hub, user.Id, "read")

	testAppFactory := func(t testing.TB) *pbTests.TestApp { return hub.TestApp }

	initBody := `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"test","version":"1"}}}`

	scenarios := []appTests.ApiScenario{
		{
			Name:   "MCP initialize succeeds with a read API key",
			Method: http.MethodPost,
			URL:    "/api/mcp",
			Headers: map[string]string{
				"Authorization": readToken,
				"Accept":        "application/json, text/event-stream",
			},
			Body:            strings.NewReader(initBody),
			ExpectedStatus:  200,
			ExpectedContent: []string{"vigil", "protocolVersion"},
			TestAppFactory:  testAppFactory,
		},
		{
			Name:   "MCP endpoint rejects an unknown key",
			Method: http.MethodPost,
			URL:    "/api/mcp",
			Headers: map[string]string{
				"Authorization": "vk_unknown_key_value_000000000000000000",
				"Accept":        "application/json, text/event-stream",
			},
			Body:            strings.NewReader(initBody),
			ExpectedStatus:  401,
			ExpectedContent: []string{"requires valid"},
			TestAppFactory:  testAppFactory,
		},
	}

	for _, scenario := range scenarios {
		scenario.Test(t)
	}
}
