package ws

import (
	"context"
	"errors"
	"sync/atomic"
	"time"
	"weak"

	"github.com/Gu1llaum-3/vigil/internal/common"

	"github.com/fxamacker/cbor/v2"
	"github.com/lxzan/gws"
)

// defaultDeadline is how long a connection may stay silent: it is pushed forward only by
// frames received from the agent.
const defaultDeadline = 70 * time.Second

// deadlineNanos holds the deadline in use; atomic so tests can shorten it while read loops run.
var deadlineNanos atomic.Int64

func init() { deadlineNanos.Store(int64(defaultDeadline)) }

func nextDeadline() time.Time { return time.Now().Add(time.Duration(deadlineNanos.Load())) }

// Handler implements the WebSocket event handler for agent connections.
type Handler struct {
	gws.BuiltinEventHandler
}

// WsConn represents a WebSocket connection to an agent.
type WsConn struct {
	// conn is cleared by OnClose (read-loop goroutine) while other goroutines ping or
	// close the connection, hence the atomic pointer.
	conn           atomic.Pointer[gws.Conn]
	requestManager *RequestManager
	DownChan       chan struct{}
}

var upgrader *gws.Upgrader

// GetUpgrader returns a singleton WebSocket upgrader instance.
func GetUpgrader() *gws.Upgrader {
	if upgrader != nil {
		return upgrader
	}
	handler := &Handler{}
	// Recovery: a panic in a callback (OnMessage, OnClose…) ends that connection's read loop
	// with a logged stack instead of crashing the hub.
	upgrader = gws.NewUpgrader(handler, &gws.ServerOption{Recovery: gws.Recovery})
	return upgrader
}

// NewWsConnection creates a new WebSocket connection wrapper.
func NewWsConnection(conn *gws.Conn) *WsConn {
	wsConn := &WsConn{
		requestManager: NewRequestManager(conn),
		DownChan:       make(chan struct{}, 1),
	}
	wsConn.conn.Store(conn)
	return wsConn
}

// OnOpen sets a deadline for the WebSocket connection.
func (h *Handler) OnOpen(conn *gws.Conn) {
	receivedFrame(conn)
}

// OnMessage routes incoming WebSocket messages to the request manager.
func (h *Handler) OnMessage(conn *gws.Conn, message *gws.Message) {
	receivedFrame(conn)
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

// OnPong extends the deadline: the agent answered a ping, so the connection is alive.
func (h *Handler) OnPong(conn *gws.Conn, _ []byte) {
	receivedFrame(conn)
}

// receivedFrame records that the agent was heard from and extends the deadline.
func receivedFrame(conn *gws.Conn) {
	if wsConn, ok := conn.Session().Load("wsConn"); ok {
		wsConn.(*WsConn).requestManager.receivedFrame()
		return
	}
	conn.SetDeadline(nextDeadline())
}

// OnClose handles WebSocket connection closures and triggers reconnection after a delay.
func (h *Handler) OnClose(conn *gws.Conn, err error) {
	wsConn, ok := conn.Session().Load("wsConn")
	if !ok {
		return
	}
	wsConn.(*WsConn).conn.Store(nil)
	// Fail the requests waiting for this agent now rather than at their timeout (up to 60s).
	wsConn.(*WsConn).requestManager.Close()
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
	if conn := ws.conn.Load(); conn != nil {
		conn.WriteClose(1000, msg)
	}
	if ws.requestManager != nil {
		ws.requestManager.Close()
	}
}

// Ping sends a ping frame; the agent answers with a pong (OnPong). It never touches the
// deadline: otherwise the hub's own pings would keep a dead peer (power loss, network cut)
// "connected" until TCP gives up, minutes later. See RequestManager.receivedFrame.
func (ws *WsConn) Ping() error {
	conn := ws.conn.Load()
	if conn == nil {
		return gws.ErrConnClosed
	}
	return conn.WritePing(nil)
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
		if ws.requestManager.closed.Load() {
			return gws.ErrConnClosed
		}
		return req.Context.Err()
	}
}

// IsConnected returns true if the WebSocket connection is active.
func (ws *WsConn) IsConnected() bool {
	return ws.conn.Load() != nil
}

// SendRequest sends a request to the agent and returns a pending request handle.
func (ws *WsConn) SendRequest(ctx context.Context, action common.WebSocketAction, data any) (*PendingRequest, error) {
	return ws.requestManager.SendRequest(ctx, action, data)
}
