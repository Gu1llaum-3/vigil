//go:build testing

package agent

import (
	"errors"
	"net/url"
	"os"
	"path/filepath"
	"testing"

	app "github.com/Gu1llaum-3/vigil"
	"github.com/Gu1llaum-3/vigil/internal/common"
	"github.com/fxamacker/cbor/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAgentTokenFileRoundTrip(t *testing.T) {
	dir := t.TempDir()
	_, ok := loadAgentToken(dir, "wss://hub.example.com", nil)
	assert.False(t, ok)

	require.NoError(t, saveAgentToken(dir, storedAgentToken{Hub: "wss://hub.example.com", HubKey: "SHA256:hubkey", Token: "unique-token-1234567890"}))
	info, err := os.Stat(filepath.Join(dir, agentTokenFileName))
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o600), info.Mode().Perm())

	stored, ok := loadAgentToken(dir, "wss://hub.example.com", nil)
	assert.True(t, ok)
	assert.Equal(t, "unique-token-1234567890", stored.Token)

	_, ok = loadAgentToken(dir, "wss://other-hub.example.com", []string{"SHA256:otherkey"})
	assert.False(t, ok, "a token issued by one hub is never sent to another")

	stored, ok = loadAgentToken(dir, "wss://hub-new-name.example.com", []string{"SHA256:hubkey"})
	assert.True(t, ok, "the same hub (same key) at a new URL keeps the token")
	assert.Equal(t, "unique-token-1234567890", stored.Token)

	assert.Error(t, saveAgentToken("", storedAgentToken{Token: "x"}), "no data dir, nothing written in the working directory")
}

func TestHubKeyIsCanonical(t *testing.T) {
	for in, want := range map[string]string{
		"https://hub.example.com":     "wss://hub.example.com",
		"wss://hub.example.com/":      "wss://hub.example.com",
		"http://10.0.0.1:8090/vigil/": "ws://10.0.0.1:8090/vigil",
		"HTTPS://Hub.Example.com/sub": "wss://hub.example.com/sub",
	} {
		assert.Equal(t, want, hubKeyFor(mustParseURL(t, in)), in)
	}
}

func newTokenTestClient(t *testing.T) *WebSocketClient {
	t.Helper()
	agent := createTestAgent(t)
	t.Setenv(app.AgentEnvPrefix+"HUB_URL", "https://hub.example.com")
	t.Setenv(app.AgentEnvPrefix+"TOKEN", "shared-enrollment-token")
	client, err := newWebSocketClient(agent)
	require.NoError(t, err)
	return client
}

func TestStoredTokenIsPreferred(t *testing.T) {
	client := newTokenTestClient(t)
	require.NoError(t, saveAgentToken(client.agent.dataDir, storedAgentToken{Hub: client.hubKey, Token: "stored-unique-token-123456"}))

	again, err := newWebSocketClient(client.agent)
	require.NoError(t, err)
	assert.Equal(t, "stored-unique-token-123456", again.currentToken())
	assert.Equal(t, "stored-unique-token-123456", again.getOptions().RequestHeader.Get("X-Token"))
}

func TestSetAgentTokenHandlerPersistsAndSwitches(t *testing.T) {
	client := newTokenTestClient(t)
	_ = client.getOptions()

	payload, err := cbor.Marshal(common.SetAgentTokenRequest{Token: "IssuedUniqueToken0123456789abcdefghijkl"})
	require.NoError(t, err)
	var sent any
	err = (&SetAgentTokenHandler{}).Handle(&HandlerContext{
		Client:       client,
		Agent:        client.agent,
		Request:      &common.HubRequest[cbor.RawMessage]{Action: common.SetAgentToken, Data: payload},
		HubVerified:  true,
		SendResponse: func(data any, _ *uint32) error { sent = data; return nil },
	})
	require.NoError(t, err)
	assert.NotNil(t, sent)

	stored, ok := loadAgentToken(client.agent.dataDir, client.hubKey, nil)
	assert.True(t, ok)
	assert.Equal(t, "IssuedUniqueToken0123456789abcdefghijkl", stored.Token)
	assert.True(t, stored.Unconfirmed)
	assert.Equal(t, tokenDigest("shared-enrollment-token"), stored.Configured)

	// Until the new token authenticates a verified connection, the token this connection
	// used stays a fallback (the hub may not have saved the new one); then it is dropped.
	confirmAgentToken(client.agent.dataDir, "IssuedUniqueToken0123456789abcdefghijkl", "shared-enrollment-token")
	stored, ok = loadAgentToken(client.agent.dataDir, client.hubKey, nil)
	require.True(t, ok)
	assert.False(t, stored.Unconfirmed)
	assert.Empty(t, stored.Previous, "the enrollment token is no longer kept once the new token works")
	assert.Equal(t, "IssuedUniqueToken0123456789abcdefghijkl", client.currentToken())
	assert.Equal(t, "IssuedUniqueToken0123456789abcdefghijkl", client.getOptions().RequestHeader.Get("X-Token"))
}

func TestSetAgentTokenRejectsWeakTokens(t *testing.T) {
	client := newTokenTestClient(t)
	for _, bad := range []string{"", "short", "has a space in it, which no hub token has", "control\x00characters0123456789abcdefgh"} {
		payload, err := cbor.Marshal(common.SetAgentTokenRequest{Token: bad})
		require.NoError(t, err)
		err = (&SetAgentTokenHandler{}).Handle(&HandlerContext{
			Client:       client,
			Agent:        client.agent,
			Request:      &common.HubRequest[cbor.RawMessage]{Action: common.SetAgentToken, Data: payload},
			HubVerified:  true,
			SendResponse: func(any, *uint32) error { return nil },
		})
		assert.Error(t, err, "%q", bad)
	}
	_, ok := loadAgentToken(client.agent.dataDir, client.hubKey, nil)
	assert.False(t, ok)
}

// The hub keeps rejecting the issued token: try the one it replaced, and back — never the
// configured enrollment token (a deleted host stays revoked, an outage of the hub cannot
// make it re-enroll), and a transient rejection never changes the token.
func TestRejectedTokenCyclesThroughIssuedTokens(t *testing.T) {
	client := newTokenTestClient(t)
	require.NoError(t, saveAgentToken(client.agent.dataDir, storedAgentToken{
		Hub: client.hubKey, Token: "stored-unique-token-123456", Previous: "previous-unique-token-1234",
		Configured: tokenDigest("shared-enrollment-token"),
	}))
	client, err := newWebSocketClient(client.agent)
	require.NoError(t, err)
	_ = client.getOptions()

	reject := func() {
		for range rejectionsBeforeFallback {
			client.handleConnectError(errUnauthorizedForTest)
		}
	}
	client.handleConnectError(errUnauthorizedForTest)
	client.handleConnectError(errors.New("connection refused")) // resets the count
	client.handleConnectError(errUnauthorizedForTest)
	assert.Equal(t, "stored-unique-token-123456", client.currentToken(), "a transient rejection keeps the token")

	reject()
	assert.Equal(t, "previous-unique-token-1234", client.currentToken())
	reject()
	assert.Equal(t, "stored-unique-token-123456", client.currentToken(), "wraps around without the enrollment token")
}

// A TOKEN changed since the issuance (the install command re-run with the current
// enrollment token, or an admin setting a rotated token) comes last: the issued token keeps
// working, and the changed one is only tried if the hub refuses it.
func TestChangedConfiguredTokenComesLast(t *testing.T) {
	client := newTokenTestClient(t)
	require.NoError(t, saveAgentToken(client.agent.dataDir, storedAgentToken{
		Hub: client.hubKey, Token: "stored-unique-token-123456", Configured: tokenDigest("an-older-enrollment-token"),
	}))
	again, err := newWebSocketClient(client.agent)
	require.NoError(t, err)
	assert.Equal(t, "stored-unique-token-123456", again.currentToken())
	assert.Equal(t, []string{"stored-unique-token-123456", "shared-enrollment-token"}, again.tokenCandidates())
}

func TestStoredTokenNeverSentOverAWeakerTransport(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, saveAgentToken(dir, storedAgentToken{Hub: "wss://hub.example.com", HubKey: "SHA256:k", Token: "unique-token-1234567890abcdef"}))
	_, ok := loadAgentToken(dir, "ws://hub.example.com", []string{"SHA256:k"})
	assert.False(t, ok)
}

func TestDeleteAgentToken(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, DeleteAgentToken(dir), "nothing to delete is fine")
	require.NoError(t, saveAgentToken(dir, storedAgentToken{Hub: "wss://hub", Token: "unique-token-1234567890abcdef"}))
	require.NoError(t, DeleteAgentToken(dir))
	_, ok := loadAgentToken(dir, "wss://hub", nil)
	assert.False(t, ok)
}

// errUnauthorizedForTest is what gws returns when the hub answers 401 to the upgrade.
var errUnauthorizedForTest = errors.New("unexpected status code: 401")

func mustParseURL(t *testing.T, raw string) *url.URL {
	t.Helper()
	u, err := url.Parse(raw)
	require.NoError(t, err)
	return u
}

// The hub never saved the issued token: the fallback worked, so it becomes the token.
func TestWorkingFallbackIsPromoted(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, saveAgentToken(dir, storedAgentToken{Hub: "wss://hub", Token: "UnsavedIssuedToken0123456789abcdef", Previous: "OldIssuedToken0123456789abcdefgh", Unconfirmed: true}))
	confirmAgentToken(dir, "OldIssuedToken0123456789abcdefgh", "enrollment")
	stored, ok := loadAgentToken(dir, "wss://hub", nil)
	require.True(t, ok)
	assert.Equal(t, "OldIssuedToken0123456789abcdefgh", stored.Token)
	assert.Empty(t, stored.Previous)
	assert.False(t, stored.Unconfirmed)
}
