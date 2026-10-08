//go:build testing

package agent

import (
	"crypto/ed25519"
	"os"
	"path/filepath"
	"testing"

	"github.com/Gu1llaum-3/vigil"
	"github.com/Gu1llaum-3/vigil/internal/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/ssh"
)

func newChallengeTestClient(t *testing.T, dataDir string, keys ...ssh.PublicKey) *WebSocketClient {
	t.Helper()
	agent, err := NewAgent(dataDir)
	require.NoError(t, err)
	agent.keys = keys
	t.Setenv(app.AgentEnvPrefix+"HUB_URL", "https://hub.example.com")
	t.Setenv(app.AgentEnvPrefix+"TOKEN", "test-token")
	client, err := newWebSocketClient(agent)
	require.NoError(t, err)
	return client
}

func newTestHubKey(t *testing.T) (ed25519.PrivateKey, ssh.PublicKey) {
	t.Helper()
	_, priv, err := ed25519.GenerateKey(nil)
	require.NoError(t, err)
	pub, err := ssh.NewPublicKey(priv.Public().(ed25519.PublicKey))
	require.NoError(t, err)
	return priv, pub
}

func TestVerifyHubSignatureNonce(t *testing.T) {
	priv, pub := newTestHubKey(t)
	client := newChallengeTestClient(t, t.TempDir(), pub)
	nonce, other := common.NewChallengeNonce(), common.NewChallengeNonce()

	_, err := client.verifyHubSignature("tok", nonce, ed25519.Sign(priv, common.HubChallenge(nonce, "tok")))
	require.NoError(t, err, "a signature over this connection's nonce proves the hub")

	_, err = client.verifyHubSignature("tok", nonce, ed25519.Sign(priv, common.HubChallenge(other, "tok")))
	require.Error(t, err, "a signature captured on another connection is not replayable")

	_, err = client.verifyHubSignature("other", nonce, ed25519.Sign(priv, common.HubChallenge(nonce, "tok")))
	require.Error(t, err, "nor is it good for another token")
}

func TestVerifyHubSignatureLegacy(t *testing.T) {
	priv, pub := newTestHubKey(t)
	dataDir := t.TempDir()
	client := newChallengeTestClient(t, dataDir, pub)
	nonce := common.NewChallengeNonce()
	legacy := ed25519.Sign(priv, []byte("tok"))

	_, err := client.verifyHubSignature("tok", nonce, legacy)
	require.NoError(t, err, "a hub older than the nonce signs the token alone: accepted while this key never signed a nonce")

	_, err = client.verifyHubSignature("tok", nonce, ed25519.Sign(priv, common.HubChallenge(nonce, "tok")))
	require.NoError(t, err)

	_, err = client.verifyHubSignature("tok", common.NewChallengeNonce(), legacy)
	require.Error(t, err, "once the key signed a nonce, a static signature is a replay")

	// The record survives a restart of the agent.
	assert.FileExists(t, filepath.Join(dataDir, hubChallengeFileName))
	info, err := os.Stat(filepath.Join(dataDir, hubChallengeFileName))
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o600), info.Mode().Perm())
	restarted := newChallengeTestClient(t, dataDir, pub)
	_, err = restarted.verifyHubSignature("tok", common.NewChallengeNonce(), legacy)
	require.Error(t, err, "the downgrade is refused after a restart too")
}

func TestVerifyHubSignatureLegacyRefusedWithoutNonce(t *testing.T) {
	priv, pub := newTestHubKey(t)
	client := newChallengeTestClient(t, t.TempDir(), pub)
	nonce := common.NewChallengeNonce()
	_, err := client.verifyHubSignature("tok", nonce, ed25519.Sign(priv, common.HubChallenge(nonce, "tok")))
	require.NoError(t, err)
	_, err = client.verifyHubSignature("tok", "", ed25519.Sign(priv, []byte("tok")))
	require.Error(t, err, "a connection without a recorded nonce gets no exemption")
}

func TestVerifyHubSignatureLegacyBoundToKey(t *testing.T) {
	privA, pubA := newTestHubKey(t)
	privB, pubB := newTestHubKey(t)
	client := newChallengeTestClient(t, t.TempDir(), pubA, pubB)
	nonce := common.NewChallengeNonce()

	_, err := client.verifyHubSignature("tok", nonce, ed25519.Sign(privA, common.HubChallenge(nonce, "tok")))
	require.NoError(t, err)

	_, err = client.verifyHubSignature("tok", common.NewChallengeNonce(), ed25519.Sign(privB, []byte("tok")))
	require.NoError(t, err, "only the key that signed a nonce loses the static signature")
	_, err = client.verifyHubSignature("tok", common.NewChallengeNonce(), ed25519.Sign(privA, []byte("tok")))
	require.Error(t, err)
}

func TestVerifyHubSignatureWithoutNonce(t *testing.T) {
	priv, pub := newTestHubKey(t)
	client := newChallengeTestClient(t, t.TempDir(), pub)
	_, err := client.verifyHubSignature("tok", "", ed25519.Sign(priv, []byte("tok")))
	require.NoError(t, err)
	_, err = client.verifyHubSignature("tok", "", ed25519.Sign(priv, []byte("other")))
	require.Error(t, err)
}

func TestConnectOptionsCarryFreshNonce(t *testing.T) {
	_, pub := newTestHubKey(t)
	client := newChallengeTestClient(t, t.TempDir(), pub)
	first := client.getOptions().RequestHeader.Get(common.ChallengeNonceHeader)
	second := client.getOptions().RequestHeader.Get(common.ChallengeNonceHeader)
	assert.True(t, common.ValidChallengeNonce(first))
	assert.NotEqual(t, first, second, "every dial carries its own nonce")
}
