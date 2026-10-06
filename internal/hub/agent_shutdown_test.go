//go:build testing

package hub

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	appmeta "github.com/Gu1llaum-3/vigil"
	"github.com/Gu1llaum-3/vigil/agent"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/ssh"
)

// TestStopAgentConnections checks that stopping the hub's agent connections ends the
// per-connection goroutines (so none touches the database after shutdown), does not
// mark the agent offline, and refuses new connections.
func TestStopAgentConnections(t *testing.T) {
	hub, testApp, err := createTestHub(t)
	require.NoError(t, err)
	defer cleanupTestHub(hub, testApp)

	signer, err := hub.GetSSHKey("")
	require.NoError(t, err)

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		acr := &agentConnectRequest{hub: hub, req: r, res: w}
		acr.agentConnect()
	}))
	defer ts.Close()

	rec, err := createTestRecord(testApp, "agents", map[string]any{
		"name":   "shutdown-agent",
		"token":  "shutdown-token",
		"status": "pending",
	})
	require.NoError(t, err)

	testAgent, err := agent.NewAgent(t.TempDir())
	require.NoError(t, err)
	t.Setenv(appmeta.AgentEnvPrefix+"HUB_URL", ts.URL)
	t.Setenv(appmeta.AgentEnvPrefix+"TOKEN", "shutdown-token")
	go func() { _ = testAgent.Start([]ssh.PublicKey{signer.PublicKey()}) }()

	// Wait until the hub tracks the live connection (its lifecycle goroutine runs).
	require.Eventually(t, func() bool {
		_, ok := hub.agentConns.Load(rec.Id)
		return ok
	}, 10*time.Second, 20*time.Millisecond, "agent never connected")

	stopped := make(chan struct{})
	go func() {
		hub.stopAgentConnections()
		close(stopped)
	}()
	select {
	case <-stopped:
	case <-time.After(5 * time.Second):
		t.Fatal("stopAgentConnections did not return: connection goroutines are still running")
	}

	got, err := testApp.FindRecordById("agents", rec.Id)
	require.NoError(t, err)
	assert.Equal(t, "connected", got.GetString("status"), "a hub shutdown must not mark agents offline")

	// New connections are refused once the hub is stopping.
	req, err := http.NewRequest(http.MethodGet, ts.URL+"/api/app/agent-connect", nil)
	require.NoError(t, err)
	req.Header.Set("X-Token", "shutdown-token")
	req.Header.Set("X-App", "1.0.0")
	res, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	res.Body.Close()
	assert.Equal(t, http.StatusServiceUnavailable, res.StatusCode)
}

// TestGoAgentAfterStop checks that no connection goroutine can start once the shutdown
// began, so a connection racing the shutdown never runs against a closed database.
func TestGoAgentAfterStop(t *testing.T) {
	hub, testApp, err := createTestHub(t)
	require.NoError(t, err)
	defer cleanupTestHub(hub, testApp)

	ran := make(chan struct{}, 1)
	require.True(t, hub.goAgent(func() { ran <- struct{}{} }))
	<-ran

	hub.stopAgentConnections()
	assert.False(t, hub.goAgent(func() { ran <- struct{}{} }), "goAgent must refuse once stopping")
	select {
	case <-ran:
		t.Fatal("fn ran after stopAgentConnections")
	case <-time.After(50 * time.Millisecond):
	}
	// Stopping twice is harmless (OnTerminate and the test cleanup may both call it).
	hub.stopAgentConnections()
}
