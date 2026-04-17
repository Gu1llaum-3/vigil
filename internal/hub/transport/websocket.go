package transport

import (
	"context"
	"errors"

	"github.com/fxamacker/cbor/v2"
	"github.com/Gu1llaum-3/vigil/internal/common"
	"github.com/Gu1llaum-3/vigil/internal/hub/ws"
)

// ErrWebSocketNotConnected indicates a WebSocket transport is not currently connected.
var ErrWebSocketNotConnected = errors.New("websocket not connected")

// WebSocketTransport implements Transport over WebSocket connections.
type WebSocketTransport struct {
	wsConn *ws.WsConn
}

// NewWebSocketTransport creates a new WebSocket transport wrapper.
func NewWebSocketTransport(wsConn *ws.WsConn) *WebSocketTransport {
	return &WebSocketTransport{wsConn: wsConn}
}

// Request sends a request to the agent via WebSocket and unmarshals the response.
func (t *WebSocketTransport) Request(ctx context.Context, action common.WebSocketAction, req any, dest any) error {
	if !t.IsConnected() {
		return ErrWebSocketNotConnected
	}

	pendingReq, err := t.wsConn.SendRequest(ctx, action, req)
	if err != nil {
		return err
	}

	// Wait for response
	select {
	case message := <-pendingReq.ResponseCh:
		defer message.Close()
		defer pendingReq.Cancel()

		var agentResponse common.AgentResponse
		if err := cbor.Unmarshal(message.Data.Bytes(), &agentResponse); err != nil {
			return err
		}

		if agentResponse.Error != "" {
			return errors.New(agentResponse.Error)
		}

		return UnmarshalResponse(agentResponse, action, dest)

	case <-pendingReq.Context.Done():
		return pendingReq.Context.Err()
	}
}

// IsConnected returns true if the WebSocket connection is active.
func (t *WebSocketTransport) IsConnected() bool {
	return t.wsConn != nil && t.wsConn.IsConnected()
}

// Close terminates the WebSocket connection.
func (t *WebSocketTransport) Close() {
	if t.wsConn != nil {
		t.wsConn.Close(nil)
	}
}
