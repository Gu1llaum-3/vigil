package ws

import (
	"context"
	"errors"
	"time"
	"weak"

	"github.com/blang/semver"

	"github.com/Gu1llaum-3/vigil/internal/common"

	"github.com/fxamacker/cbor/v2"
	"github.com/lxzan/gws"
)

const (
	deadline = 70 * time.Second
)

// Handler implements the WebSocket event handler for agent connections.
type Handler struct {
	gws.BuiltinEventHandler
}

// WsConn represents a WebSocket connection to an agent.
type WsConn struct {
	conn           *gws.Conn
	requestManager *RequestManager
	DownChan       chan struct{}
	agentVersion   semver.Version
}

var upgrader *gws.Upgrader

// GetUpgrader returns a singleton WebSocket upgrader instance.
func GetUpgrader() *gws.Upgrader {
	if upgrader != nil {
		return upgrader
	}
	handler := &Handler{}
	upgrader = gws.NewUpgrader(handler, &gws.ServerOption{})
	return upgrader
}

// NewWsConnection creates a new WebSocket connection wrapper with agent version.
func NewWsConnection(conn *gws.Conn, agentVersion semver.Version) *WsConn {
	return &WsConn{
		conn:           conn,
		requestManager: NewRequestManager(conn),
		DownChan:       make(chan struct{}, 1),
		agentVersion:   agentVersion,
	}
}

// OnOpen sets a deadline for the WebSocket connection.
func (h *Handler) OnOpen(conn *gws.Conn) {
	conn.SetDeadline(time.Now().Add(deadline))
}

// OnMessage routes incoming WebSocket messages to the request manager.
func (h *Handler) OnMessage(conn *gws.Conn, message *gws.Message) {
	conn.SetDeadline(time.Now().Add(deadline))
	if message.Opcode != gws.OpcodeBinary || message.Data.Len() == 0 {
		return
	}
	wsConn, ok := conn.Session().Load("wsConn")
	if !ok {
		_ = conn.WriteClose(1000, nil)
		return
	}
	wsConn.(*WsConn).requestManager.handleResponse(message)
}

// OnClose handles WebSocket connection closures and triggers reconnection after a delay.
func (h *Handler) OnClose(conn *gws.Conn, err error) {
	wsConn, ok := conn.Session().Load("wsConn")
	if !ok {
		return
	}
	wsConn.(*WsConn).conn = nil
	// wait 5 seconds to allow reconnection before signaling down
	go func(downChan weak.Pointer[chan struct{}]) {
		time.Sleep(5 * time.Second)
		downChanValue := downChan.Value()
		if downChanValue != nil {
			*downChanValue <- struct{}{}
		}
	}(weak.Make(&wsConn.(*WsConn).DownChan))
}

// Close terminates the WebSocket connection gracefully.
func (ws *WsConn) Close(msg []byte) {
	if ws.IsConnected() {
		ws.conn.WriteClose(1000, msg)
	}
	if ws.requestManager != nil {
		ws.requestManager.Close()
	}
}

// Ping sends a ping frame to keep the connection alive.
func (ws *WsConn) Ping() error {
	if ws.conn == nil {
		return gws.ErrConnClosed
	}
	ws.conn.SetDeadline(time.Now().Add(deadline))
	return ws.conn.WritePing(nil)
}

// handleAgentRequest processes a response from the agent.
func (ws *WsConn) handleAgentRequest(req *PendingRequest, handler ResponseHandler) error {
	select {
	case message := <-req.ResponseCh:
		defer message.Close()
		defer req.Cancel()
		data := message.Data.Bytes()

		var agentResponse common.AgentResponse
		if err := cbor.Unmarshal(data, &agentResponse); err != nil {
			return err
		}
		if agentResponse.Error != "" {
			return errors.New(agentResponse.Error)
		}
		return handler.Handle(agentResponse)

	case <-req.Context.Done():
		return req.Context.Err()
	}
}

// IsConnected returns true if the WebSocket connection is active.
func (ws *WsConn) IsConnected() bool {
	return ws.conn != nil
}

// AgentVersion returns the connected agent's version (as reported during handshake).
func (ws *WsConn) AgentVersion() semver.Version {
	return ws.agentVersion
}

// SendRequest sends a request to the agent and returns a pending request handle.
func (ws *WsConn) SendRequest(ctx context.Context, action common.WebSocketAction, data any) (*PendingRequest, error) {
	return ws.requestManager.SendRequest(ctx, action, data)
}
