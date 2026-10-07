//go:build testing

package agent

import (
	"net"
	"net/url"
	"sync"
	"testing"
	"time"

	app "github.com/Gu1llaum-3/vigil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func createTestAgent(t *testing.T) *Agent {
	dataDir := t.TempDir()
	agent, err := NewAgent(dataDir)
	require.NoError(t, err)
	return agent
}

// TestConnectionManager_NewConnectionManager tests connection manager creation
func TestConnectionManager_NewConnectionManager(t *testing.T) {
	agent := createTestAgent(t)
	cm := newConnectionManager(agent)

	assert.NotNil(t, cm, "Connection manager should not be nil")
	assert.Equal(t, agent, cm.agent, "Agent reference should be set")
	assert.Equal(t, Disconnected, cm.State(), "Initial state should be Disconnected")
	assert.Nil(t, cm.eventChan, "Event channel should be nil initially")
	assert.Nil(t, cm.wsClient, "WebSocket client should be nil initially")
	assert.Nil(t, cm.wsTicker, "WebSocket ticker should be nil initially")
	assert.Nil(t, cm.retryC, "No retry should be scheduled initially")
}

// TestConnectionManager_StateTransitions tests basic state transitions
func TestConnectionManager_StateTransitions(t *testing.T) {
	agent := createTestAgent(t)
	cm := agent.connectionManager
	initialState := cm.State()
	cm.wsClient = &WebSocketClient{
		hubURL: &url.URL{
			Host: "localhost:8080",
		},
	}
	assert.NotNil(t, cm, "Connection manager should not be nil")
	assert.Equal(t, Disconnected, initialState, "Initial state should be Disconnected")

	// Test state transitions
	cm.handleStateChange(WebSocketConnected)
	assert.Equal(t, WebSocketConnected, cm.State(), "State should change to WebSocketConnected")

	cm.handleStateChange(Disconnected)
	assert.Equal(t, Disconnected, cm.State(), "State should change to Disconnected")

	// Test that same state doesn't trigger changes
	cm.setState(WebSocketConnected)
	cm.handleStateChange(WebSocketConnected)
	assert.Equal(t, WebSocketConnected, cm.State(), "Same state should not trigger change")
}

// TestConnectionManager_EventHandling tests event handling logic
func TestConnectionManager_EventHandling(t *testing.T) {
	agent := createTestAgent(t)
	cm := agent.connectionManager
	cm.wsClient = &WebSocketClient{
		hubURL: &url.URL{
			Host: "localhost:8080",
		},
	}

	testCases := []struct {
		name          string
		initialState  ConnectionState
		event         ConnectionEvent
		expectedState ConnectionState
	}{
		{
			name:          "WebSocket connect from disconnected",
			initialState:  Disconnected,
			event:         WebSocketConnect,
			expectedState: WebSocketConnected,
		},
		{
			name:          "WebSocket disconnect from connected",
			initialState:  WebSocketConnected,
			event:         WebSocketDisconnect,
			expectedState: Disconnected,
		},
		{
			name:          "WebSocket disconnect from already disconnected (no change)",
			initialState:  Disconnected,
			event:         WebSocketDisconnect,
			expectedState: Disconnected,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			cm.setState(tc.initialState)
			cm.handleEvent(tc.event)
			assert.Equal(t, tc.expectedState, cm.State(), "State should match expected after event")
		})
	}
}

// TestConnectionManager_TickerManagement tests WebSocket ticker management
func TestConnectionManager_TickerManagement(t *testing.T) {
	agent := createTestAgent(t)
	cm := agent.connectionManager

	// Test starting ticker
	cm.startWsTicker()
	assert.NotNil(t, cm.wsTicker, "Ticker should be created")

	// Test stopping ticker (should not panic)
	assert.NotPanics(t, func() {
		cm.stopWsTicker()
	}, "Stopping ticker should not panic")

	// Test stopping nil ticker (should not panic)
	cm.wsTicker = nil
	assert.NotPanics(t, func() {
		cm.stopWsTicker()
	}, "Stopping nil ticker should not panic")

	// Test restarting ticker
	cm.startWsTicker()
	assert.NotNil(t, cm.wsTicker, "Ticker should be recreated")

	// Test resetting existing ticker
	firstTicker := cm.wsTicker
	cm.startWsTicker()
	assert.Equal(t, firstTicker, cm.wsTicker, "Same ticker instance should be reused")

	cm.stopWsTicker()
}

// TestConnectionManager_WebSocketConnectionFlow tests WebSocket connection logic
func TestConnectionManager_WebSocketConnectionFlow(t *testing.T) {
	agent := createTestAgent(t)
	cm := agent.connectionManager

	// Test WebSocket connection without proper environment
	err := cm.startWebSocketConnection()
	assert.Error(t, err, "WebSocket connection should fail without proper environment")
	assert.Equal(t, Disconnected, cm.State(), "State should remain Disconnected after failed connection")

	// Test with invalid URL
	t.Setenv(app.AgentEnvPrefix+"HUB_URL", "1,33%")
	t.Setenv(app.AgentEnvPrefix+"TOKEN", "test-token")

	_, err2 := newWebSocketClient(agent)
	assert.Error(t, err2, "WebSocket client creation should fail with invalid URL")

	// Test with missing token
	t.Setenv(app.AgentEnvPrefix+"HUB_URL", "http://localhost:8080")
	t.Setenv(app.AgentEnvPrefix+"TOKEN", "")

	_, err3 := newWebSocketClient(agent)
	assert.Error(t, err3, "WebSocket client creation should fail without token")
}

// TestConnectionManager_ConnectWithRateLimit tests connection rate limiting
func TestConnectionManager_ConnectWithRateLimit(t *testing.T) {
	agent := createTestAgent(t)
	cm := agent.connectionManager

	// Set up environment for WebSocket client creation, pointing at a port nothing
	// listens on (a fixed port like 8080 may be taken by a local hub).
	t.Setenv(app.AgentEnvPrefix+"HUB_URL", "ws://"+closedLocalAddr(t))
	t.Setenv(app.AgentEnvPrefix+"TOKEN", "test-token")

	// Create WebSocket client
	wsClient, err := newWebSocketClient(agent)
	require.NoError(t, err)
	cm.wsClient = wsClient

	// Set recent connection attempt
	cm.wsClient.lastConnectAttempt = time.Now()

	// Test that connection is rate limited
	err = cm.startWebSocketConnection()
	require.Error(t, err, "Should error due to rate limiting")
	assert.Contains(t, err.Error(), "already connecting", "Error should indicate rate limiting")

	// Test connection after rate limit expires
	cm.wsClient.lastConnectAttempt = time.Now().Add(-10 * time.Second)
	err = cm.startWebSocketConnection()
	// This will fail due to no actual server, but should not be rate limited
	require.Error(t, err, "Connection should fail but not due to rate limiting")
	assert.NotContains(t, err.Error(), "already connecting", "Error should not indicate rate limiting")
}

// TestConnectionManager_StartWithInvalidConfig tests starting with invalid configuration
func TestConnectionManager_StartWithInvalidConfig(t *testing.T) {
	agent := createTestAgent(t)
	cm := agent.connectionManager

	// Test starting when already started
	cm.eventChan = make(chan ConnectionEvent, 5)
	err := cm.Start()
	assert.Error(t, err, "Should error when starting already started connection manager")
}

// A configuration the agent can never connect with must stop it, not leave it running idle.
func TestConnectionManager_StartFailsOnInvalidHubURL(t *testing.T) {
	agent := createTestAgent(t)
	t.Setenv(app.AgentEnvPrefix+"HUB_URL", "ftp://hub.example.com")
	t.Setenv(app.AgentEnvPrefix+"TOKEN", "test-token")

	err := agent.connectionManager.Start()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "HUB_URL must start with https://")
}

// TestConnectionManager_CloseWebSocket tests WebSocket closing
func TestConnectionManager_CloseWebSocket(t *testing.T) {
	agent := createTestAgent(t)
	cm := agent.connectionManager

	// Test closing when no WebSocket client exists
	assert.NotPanics(t, func() {
		cm.closeWebSocket()
	}, "Should not panic when closing nil WebSocket client")

	// Set up environment and create WebSocket client
	t.Setenv(app.AgentEnvPrefix+"HUB_URL", "ws://localhost:8080")
	t.Setenv(app.AgentEnvPrefix+"TOKEN", "test-token")

	wsClient, err := newWebSocketClient(agent)
	require.NoError(t, err)
	cm.wsClient = wsClient

	// Test closing when WebSocket client exists
	assert.NotPanics(t, func() {
		cm.closeWebSocket()
	}, "Should not panic when closing WebSocket client")
}

// TestConnectionManager_ConnectFlow tests the connect method
func TestConnectionManager_ConnectFlow(t *testing.T) {
	agent := createTestAgent(t)
	cm := agent.connectionManager

	// Test connect without WebSocket client
	assert.NotPanics(t, func() {
		cm.connect()
	}, "Connect should not panic without WebSocket client")
}

// newUnreachableWsClient builds a real client pointed at a closed port, so Connect fails fast.
func newUnreachableWsClient(t *testing.T, agent *Agent) *WebSocketClient {
	t.Setenv(app.AgentEnvPrefix+"HUB_URL", "ws://127.0.0.1:1")
	t.Setenv(app.AgentEnvPrefix+"TOKEN", "test-token")
	wsClient, err := newWebSocketClient(agent)
	require.NoError(t, err)
	return wsClient
}

// TestConnectionManager_StateIsSafeForConcurrentReads reads State() from another goroutine
// while the owning goroutine transitions; run under -race to catch unsynchronized access.
func TestConnectionManager_StateIsSafeForConcurrentReads(t *testing.T) {
	agent := createTestAgent(t)
	cm := agent.connectionManager
	cm.wsClient = newUnreachableWsClient(t, agent)
	cm.wsClient.lastConnectAttempt = time.Now() // keep reconnects scheduled, not dialed

	stop := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-stop:
				return
			default:
				_ = cm.State()
			}
		}
	}()
	for range 50 {
		cm.handleStateChange(WebSocketConnected)
		cm.handleStateChange(Disconnected)
	}
	close(stop)
	wg.Wait()
	assert.Equal(t, Disconnected, cm.State())
}

// TestConnectionManager_DisconnectSchedulesRetryOnLoop checks that a disconnect shortly after
// a connection attempt arms a retry for the event loop instead of dialing (or sleeping) in a
// separate goroutine.
func TestConnectionManager_DisconnectSchedulesRetryOnLoop(t *testing.T) {
	agent := createTestAgent(t)
	cm := agent.connectionManager
	cm.wsClient = newUnreachableWsClient(t, agent)
	lastAttempt := time.Now()
	cm.wsClient.lastConnectAttempt = lastAttempt
	cm.setState(WebSocketConnected)

	cm.handleStateChange(Disconnected)

	assert.Equal(t, Disconnected, cm.State())
	assert.NotNil(t, cm.retryC, "a retry should be scheduled on the event loop")
	assert.Equal(t, lastAttempt, cm.wsClient.lastConnectAttempt, "no connection attempt should be made yet")

	select {
	case <-cm.retryC:
	case <-time.After(connectAttemptSpacing + time.Second):
		t.Fatal("scheduled retry never fired")
	}
}

// TestConnectionManager_DisconnectReconnectsImmediately checks that once the attempt spacing has
// elapsed, a disconnect reconnects synchronously and falls back to the ticker when that fails.
func TestConnectionManager_DisconnectReconnectsImmediately(t *testing.T) {
	agent := createTestAgent(t)
	cm := agent.connectionManager
	cm.wsClient = newUnreachableWsClient(t, agent)
	cm.wsClient.lastConnectAttempt = time.Now().Add(-10 * time.Second)
	cm.setState(WebSocketConnected)

	cm.handleStateChange(Disconnected)

	assert.Equal(t, Disconnected, cm.State())
	assert.Less(t, time.Since(cm.wsClient.lastConnectAttempt), time.Second, "reconnect should be attempted before returning")
	assert.Nil(t, cm.retryC, "no retry should be scheduled when the attempt was made")
	require.NotNil(t, cm.wsTicker, "a failed attempt should start the retry ticker")
	cm.stopWsTicker()
}

// TestConnectionManager_ConnectedClearsScheduledRetry checks that a successful connection cancels
// a pending retry.
func TestConnectionManager_ConnectedClearsScheduledRetry(t *testing.T) {
	agent := createTestAgent(t)
	cm := agent.connectionManager
	cm.wsClient = newUnreachableWsClient(t, agent)
	cm.wsClient.lastConnectAttempt = time.Now()
	cm.setState(WebSocketConnected)
	cm.handleStateChange(Disconnected)
	require.NotNil(t, cm.retryC)

	cm.handleStateChange(WebSocketConnected)

	assert.Equal(t, WebSocketConnected, cm.State())
	assert.Nil(t, cm.retryC, "connecting should cancel the scheduled retry")
}

// closedLocalAddr returns a loopback address with no listener: it binds a free port and
// releases it, so a connection attempt is refused instead of reaching a local service.
func closedLocalAddr(t *testing.T) string {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	addr := l.Addr().String()
	require.NoError(t, l.Close())
	return addr
}
