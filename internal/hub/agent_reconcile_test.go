//go:build testing

package hub

import (
	"testing"
	"time"

	"github.com/Gu1llaum-3/vigil/internal/hub/ws"
	"github.com/pocketbase/dbx"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func agentStatus(t *testing.T, hub *Hub, id string) string {
	t.Helper()
	rec, err := hub.FindRecordById("agents", id)
	require.NoError(t, err)
	return rec.GetString("status")
}

func createAgentWithStatus(t *testing.T, hub *Hub, name, status string) string {
	t.Helper()
	return createAgentSeenAt(t, hub, name, status, time.Now().Add(-time.Hour))
}

func createAgentSeenAt(t *testing.T, hub *Hub, name, status string, lastSeen time.Time) string {
	t.Helper()
	rec, err := createTestRecord(hub, "agents", map[string]any{"name": name, "token": name + "-token", "status": status, "last_seen": lastSeen})
	require.NoError(t, err)
	return rec.Id
}

func TestReconcileAgentStatuses(t *testing.T) {
	hub, testApp, err := createTestHub(t)
	require.NoError(t, err)
	defer cleanupTestHub(hub, testApp)

	dead := createAgentWithStatus(t, hub, "dead", "connected")
	live := createAgentWithStatus(t, hub, "live", "connected")
	pending := createAgentWithStatus(t, hub, "pending", "pending")
	offline := createAgentWithStatus(t, hub, "offline", "offline")
	// reconnected after boot but not yet in agentConns (handshake still storing the conn)
	reconnecting := createAgentSeenAt(t, hub, "reconnecting", "connected", time.Now())
	hub.agentConns.Store(live, &ws.WsConn{})
	defer hub.agentConns.Delete(live)

	before := time.Now()
	hub.reconcileAgentStatuses(time.Now().Add(-time.Second))

	assert.Equal(t, "offline", agentStatus(t, hub, dead), "a host recorded as connected that never reconnected is offline")
	assert.Equal(t, "connected", agentStatus(t, hub, reconnecting), "an agent seen since boot is reconnecting, not dead")
	rec, err := hub.FindRecordById("agents", dead)
	require.NoError(t, err)
	assert.False(t, rec.GetDateTime("last_seen").Time().Before(before.Truncate(time.Millisecond)),
		"going offline refreshes last_seen, so the offline purge counts from now")
	notes, err := hub.FindRecordsByFilter(systemNotificationsCollection, "resource_id = {:id}", "", 0, 0, dbx.Params{"id": dead})
	require.NoError(t, err)
	assert.Len(t, notes, 1, "the agent-offline notification is emitted")
	assert.Equal(t, "connected", agentStatus(t, hub, live))
	assert.Equal(t, "pending", agentStatus(t, hub, pending))
	assert.Equal(t, "offline", agentStatus(t, hub, offline))
}

func TestAgentStatusReconcilerRunsAfterBootDelay(t *testing.T) {
	hub, testApp, err := createTestHub(t)
	require.NoError(t, err)
	defer cleanupTestHub(hub, testApp)

	previous := agentBootReconcileDelay
	agentBootReconcileDelay = 200 * time.Millisecond
	defer func() { agentBootReconcileDelay = previous }()

	dead := createAgentWithStatus(t, hub, "dead", "connected")
	hub.startAgentStatusReconciler()

	assert.Equal(t, "connected", agentStatus(t, hub, dead), "agents get the delay to reconnect first")
	agentBootReconcileDelay = previous // the scheduled run already captured its delay
	require.Eventually(t, func() bool { return agentStatus(t, hub, dead) == "offline" }, 5*time.Second, 10*time.Millisecond)
}

func TestAgentStatusReconcilerStopsWithHub(t *testing.T) {
	hub, testApp, err := createTestHub(t)
	require.NoError(t, err)
	defer cleanupTestHub(hub, testApp)

	dead := createAgentWithStatus(t, hub, "dead", "connected")
	hub.startAgentStatusReconciler() // default delay: far longer than the test

	stopped := make(chan struct{})
	go func() {
		hub.stopAgentConnections()
		close(stopped)
	}()
	select {
	case <-stopped:
	case <-time.After(5 * time.Second):
		t.Fatal("stopAgentConnections waited for the reconciler delay")
	}
	assert.Equal(t, "connected", agentStatus(t, hub, dead), "a stopping hub must not mark agents offline")
}
