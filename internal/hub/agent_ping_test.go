//go:build testing

package hub

import (
	"testing"
	"time"

	"github.com/Gu1llaum-3/vigil/internal/hub/ws"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// runLifecycleUntilPingFails runs manageAgentLifecycle for conn, whose ping always fails
// (no underlying connection), and waits for it to return.
func runLifecycleUntilPingFails(t *testing.T, hub *Hub, conn *ws.WsConn, agentID string) {
	t.Helper()
	previous := agentPingInterval
	agentPingInterval = 10 * time.Millisecond
	defer func() { agentPingInterval = previous }()

	done := make(chan struct{})
	go func() {
		hub.manageAgentLifecycle(conn, agentID)
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("lifecycle did not return after a failed ping")
	}
}

func TestFailedPingOfStaleConnectionKeepsAgentConnected(t *testing.T) {
	hub, testApp, err := createTestHub(t)
	require.NoError(t, err)
	defer cleanupTestHub(hub, testApp)

	id := createAgentWithStatus(t, hub, "restarted", "connected")
	stale, current := &ws.WsConn{}, &ws.WsConn{}
	hub.agentConns.Store(id, current) // the agent restarted and reconnected
	defer hub.agentConns.Delete(id)

	runLifecycleUntilPingFails(t, hub, stale, id)

	assert.Equal(t, "connected", agentStatus(t, hub, id), "the old connection's ping failure must not mark a reconnected agent offline")
	stored, ok := hub.agentConns.Load(id)
	require.True(t, ok)
	assert.Same(t, current, stored)
}

func TestFailedPingOfCurrentConnectionMarksAgentOffline(t *testing.T) {
	hub, testApp, err := createTestHub(t)
	require.NoError(t, err)
	defer cleanupTestHub(hub, testApp)

	id := createAgentWithStatus(t, hub, "dead", "connected")
	conn := &ws.WsConn{}
	hub.agentConns.Store(id, conn)

	runLifecycleUntilPingFails(t, hub, conn, id)

	assert.Equal(t, "offline", agentStatus(t, hub, id))
	_, ok := hub.agentConns.Load(id)
	assert.False(t, ok)
}

// An offline write from an older connection can land between the new handshake's
// status=connected and the moment the new connection is stored: registering it must
// restore connected.
func TestRegisterAgentConnRepairsInterleavedOfflineWrite(t *testing.T) {
	hub, testApp, err := createTestHub(t)
	require.NoError(t, err)
	defer cleanupTestHub(hub, testApp)

	id := createAgentWithStatus(t, hub, "flapped", "connected")
	stale := &ws.WsConn{}
	hub.agentConns.Store(id, stale) // the old connection is still the stored one
	runLifecycleUntilPingFails(t, hub, stale, id)
	require.Equal(t, "offline", agentStatus(t, hub, id))

	fresh := &ws.WsConn{}
	hub.registerAgentConn(id, fresh)
	defer hub.agentConns.Delete(id)

	assert.Equal(t, "connected", agentStatus(t, hub, id))
}
