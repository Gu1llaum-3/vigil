package agent

import (
	"context"
	"errors"
	"log/slog"
	"os/signal"
	"syscall"
	"time"

	"github.com/Gu1llaum-3/vigil/agent/health"
)

// ConnectionManager manages the connection state and events for the agent.
type ConnectionManager struct {
	agent        *Agent
	State        ConnectionState
	eventChan    chan ConnectionEvent
	wsClient     *WebSocketClient
	wsTicker     *time.Ticker
	isConnecting bool
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
	WebSocketConnect    ConnectionEvent = iota
	WebSocketDisconnect
)

const wsTickerInterval = 10 * time.Second

// newConnectionManager creates a new connection manager for the given agent.
func newConnectionManager(agent *Agent) *ConnectionManager {
	return &ConnectionManager{
		agent: agent,
		State: Disconnected,
	}
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

	wsClient, err := newWebSocketClient(c.agent)
	if err != nil {
		slog.Warn("Error creating WebSocket client", "err", err)
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
		if c.State == WebSocketConnected {
			c.handleStateChange(Disconnected)
		}
	}
}

func (c *ConnectionManager) handleStateChange(newState ConnectionState) {
	if c.State == newState {
		return
	}
	c.State = newState
	switch newState {
	case WebSocketConnected:
		slog.Info("WebSocket connected", "host", c.wsClient.hubURL.Host)
		c.stopWsTicker()
		c.isConnecting = false
	case Disconnected:
		if c.isConnecting {
			return
		}
		c.isConnecting = true
		slog.Warn("Disconnected from hub")
		c.closeWebSocket()
		go c.connect()
	}
}

func (c *ConnectionManager) connect() {
	c.isConnecting = true
	defer func() { c.isConnecting = false }()

	if c.wsClient != nil && time.Since(c.wsClient.lastConnectAttempt) < 5*time.Second {
		time.Sleep(5 * time.Second)
	}

	err := c.startWebSocketConnection()
	if err != nil && c.State == Disconnected {
		c.startWsTicker()
	}
}

func (c *ConnectionManager) startWebSocketConnection() error {
	if c.State != Disconnected {
		return errors.New("already connected")
	}
	if c.wsClient == nil {
		return errors.New("WebSocket client not initialized")
	}
	if time.Since(c.wsClient.lastConnectAttempt) < 5*time.Second {
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
