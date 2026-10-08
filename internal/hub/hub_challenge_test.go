//go:build testing

package hub

import (
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	appmeta "github.com/Gu1llaum-3/vigil"
	"github.com/Gu1llaum-3/vigil/internal/common"
	"github.com/fxamacker/cbor/v2"
	"github.com/lxzan/gws"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/ssh"
)

// challengeCatcher is a bare WebSocket client that records the hub's CheckFingerprint
// signature instead of answering it.
type challengeCatcher struct {
	gws.BuiltinEventHandler
	signatures chan []byte
}

func (c *challengeCatcher) OnMessage(_ *gws.Conn, message *gws.Message) {
	defer message.Close()
	var req common.HubRequest[cbor.RawMessage]
	if cbor.Unmarshal(message.Bytes(), &req) != nil || req.Action != common.CheckFingerprint {
		return
	}
	var fp common.FingerprintRequest
	if cbor.Unmarshal(req.Data, &fp) == nil {
		c.signatures <- fp.Signature
	}
}

// hubSignatureFor connects like an agent sending nonce (none: an agent older than it) and
// returns the signature the hub sent.
func hubSignatureFor(t *testing.T, env identityTestEnv, nonce string) []byte {
	t.Helper()
	header := http.Header{"X-Token": {env.enrollment}, "X-App": {appmeta.Version}}
	if nonce != "" {
		header.Set(common.ChallengeNonceHeader, nonce)
	}
	catcher := &challengeCatcher{signatures: make(chan []byte, 1)}
	addr := strings.Replace(os.Getenv(appmeta.AgentEnvPrefix+"HUB_URL"), "http", "ws", 1) + "/api/app/agent-connect"
	conn, _, err := gws.NewClient(catcher, &gws.ClientOption{Addr: addr, RequestHeader: header})
	require.NoError(t, err)
	t.Cleanup(func() { _ = conn.WriteClose(1000, nil) })
	go conn.ReadLoop()
	select {
	case sig := <-catcher.signatures:
		return sig
	case <-time.After(10 * time.Second):
		t.Fatal("the hub sent no challenge")
		return nil
	}
}

func verifiesWith(key ssh.PublicKey, message, signature []byte) bool {
	return key.Verify(message, &ssh.Signature{Format: key.Type(), Blob: signature}) == nil
}

func TestHubSignsTheAgentNonce(t *testing.T) {
	env := newIdentityTestEnv(t)
	nonce := common.NewChallengeNonce()
	sig := hubSignatureFor(t, env, nonce)
	assert.True(t, verifiesWith(env.key, common.HubChallenge(nonce, env.enrollment), sig))
	assert.False(t, verifiesWith(env.key, []byte(env.enrollment), sig), "not the replayable form")
}

func TestHubSignsTheTokenForAgentsWithoutNonce(t *testing.T) {
	env := newIdentityTestEnv(t)
	sig := hubSignatureFor(t, env, "")
	assert.True(t, verifiesWith(env.key, []byte(env.enrollment), sig), "agents older than the nonce keep working")
}
