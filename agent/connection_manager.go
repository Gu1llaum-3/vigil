package agent

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os/signal"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/Gu1llaum-3/vigil/agent/health"
)

// ConnectionManager manages the connection state and events for the agent.
//
// All connection work (state transitions, connect attempts, ticker and retry handling) runs
// on the Start event loop goroutine; the WebSocket client only reports back through
// eventChan. The state is stored atomically so State can be read from any goroutine.
type ConnectionManager struct {
	agent     *Agent
	state     atomic.Uint32 // ConnectionState
	eventChan chan ConnectionEvent
	wsClient  *WebSocketClient
	wsTicker  *time.Ticker
	retryC    <-chan time.Time // pending reconnect after a too-recent attempt; nil when none
}

// ConnectionState represents the current connection state of the agent.
type ConnectionState uint8

// ConnectionEvent represents connection-related events.
type ConnectionEvent uint8

const (
	Disconnected ConnectionState = iota
	WebSocketConnected
)

const (
	WebSocketConnect ConnectionEvent = iota
	WebSocketDisconnect
)

const (
	wsTickerInterval = 10 * time.Second
	// connectAttemptSpacing is the minimum delay between two connection attempts.
	connectAttemptSpacing = 5 * time.Second
)

// newConnectionManager creates a new connection manager for the given agent.
func newConnectionManager(agent *Agent) *ConnectionManager {
	return &ConnectionManager{agent: agent} // zero state is Disconnected
}

// State returns the current connection state. It is safe to call from any goroutine.
func (c *ConnectionManager) State() ConnectionState {
	return ConnectionState(c.state.Load())
}

func (c *ConnectionManager) setState(state ConnectionState) {
	c.state.Store(uint32(state))
}

func (c *ConnectionManager) startWsTicker() {
	if c.wsTicker == nil {
		c.wsTicker = time.NewTicker(wsTickerInterval)
	} else {
		c.wsTicker.Reset(wsTickerInterval)
	}
}

func (c *ConnectionManager) stopWsTicker() {
	if c.wsTicker != nil {
		c.wsTicker.Stop()
	}
}

// Start begins connection attempts and enters the main event loop.
func (c *ConnectionManager) Start() error {
	if c.eventChan != nil {
		return errors.New("already started")
	}

	// A configuration the agent can never connect with (no or invalid HUB_URL, no token)
	// stops it: running idle would look healthy to the service manager.
	wsClient, err := newWebSocketClient(c.agent)
	if err != nil {
		return fmt.Errorf("invalid hub connection settings: %w", err)
	}
	c.wsClient = wsClient
	c.eventChan = make(chan ConnectionEvent, 1)

	sigCtx, stopSignals := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stopSignals()

	c.startWsTicker()
	c.connect()

	_ = health.Update()
	healthTicker := time.Tick(90 * time.Second)

	for {
		select {
		case connectionEvent := <-c.eventChan:
			c.handleEvent(connectionEvent)
		case <-c.wsTicker.C:
			_ = c.startWebSocketConnection()
		case <-c.retryC:
			c.retryC = nil
			c.connect()
		case <-healthTicker:
			_ = health.Update()
		case <-sigCtx.Done():
			slog.Info("Shutting down", "cause", context.Cause(sigCtx))
			c.closeWebSocket()
			return health.CleanUp()
		}
	}
}

func (c *ConnectionManager) handleEvent(event ConnectionEvent) {
	switch event {
	case WebSocketConnect:
		c.handleStateChange(WebSocketConnected)
	case WebSocketDisconnect:
		if c.State() == WebSocketConnected {
			c.handleStateChange(Disconnected)
		}
	}
}

func (c *ConnectionManager) handleStateChange(newState ConnectionState) {
	if c.State() == newState {
		return
	}
	c.setState(newState)
	switch newState {
	case WebSocketConnected:
		slog.Info("WebSocket connected", "host", c.wsClient.hubURL.Host)
		c.stopWsTicker()
		c.retryC = nil
	case Disconnected:
		slog.Warn("Disconnected from hub")
		c.closeWebSocket()
		c.connect()
	}
}

// connect attempts a connection now, or schedules the attempt on the event loop when the
// previous one is more recent than connectAttemptSpacing. A failed attempt starts the
// retry ticker. Must run on the event loop goroutine.
func (c *ConnectionManager) connect() {
	if c.wsClient != nil {
		if wait := connectAttemptSpacing - time.Since(c.wsClient.lastConnectAttempt); wait > 0 {
			c.retryC = time.After(wait)
			return
		}
	}

	err := c.startWebSocketConnection()
	if err != nil && c.State() == Disconnected {
		c.startWsTicker()
	}
}

func (c *ConnectionManager) startWebSocketConnection() error {
	if c.State() != Disconnected {
		return errors.New("already connected")
	}
	if c.wsClient == nil {
		return errors.New("WebSocket client not initialized")
	}
	if time.Since(c.wsClient.lastConnectAttempt) < connectAttemptSpacing {
		return errors.New("already connecting")
	}
	err := c.wsClient.Connect()
	if err != nil {
		slog.Warn("WebSocket connection failed", "err", err)
		c.closeWebSocket()
	}
	return err
}

func (c *ConnectionManager) closeWebSocket() {
	if c.wsClient != nil {
		c.wsClient.Close()
	}
}
