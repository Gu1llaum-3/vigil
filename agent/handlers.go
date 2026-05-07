package agent

import (
	"errors"
	"fmt"
	"log/slog"
	"os"

	app "github.com/Gu1llaum-3/vigil"
	"github.com/Gu1llaum-3/vigil/agent/collectors"
	"github.com/Gu1llaum-3/vigil/internal/common"
	"github.com/fxamacker/cbor/v2"
)

// HandlerContext provides context for request handlers.
type HandlerContext struct {
	Client      *WebSocketClient
	Agent       *Agent
	Request     *common.HubRequest[cbor.RawMessage]
	RequestID   *uint32
	HubVerified bool
	// SendResponse sends a response back to the hub over WebSocket.
	SendResponse func(data any, requestID *uint32) error
}

// RequestHandler defines the interface for handling specific websocket request types.
type RequestHandler interface {
	Handle(hctx *HandlerContext) error
}

// HandlerRegistry manages the mapping between actions and their handlers.
type HandlerRegistry struct {
	handlers map[common.WebSocketAction]RequestHandler
}

// NewHandlerRegistry creates a new handler registry with default handlers.
func NewHandlerRegistry() *HandlerRegistry {
	registry := &HandlerRegistry{
		handlers: make(map[common.WebSocketAction]RequestHandler),
	}
	registry.Register(common.GetAgentInfo, &GetAgentInfoHandler{})
	registry.Register(common.CheckFingerprint, &CheckFingerprintHandler{})
	registry.Register(common.Ping, &PingHandler{})
	registry.Register(common.GetHostSnapshot, &GetHostSnapshotHandler{})
	registry.Register(common.GetHostMetrics, &GetHostMetricsHandler{})
	registry.Register(common.GetContainerMetrics, &GetContainerMetricsHandler{})
	return registry
}

// Register registers a handler for a specific action type.
func (hr *HandlerRegistry) Register(action common.WebSocketAction, handler RequestHandler) {
	hr.handlers[action] = handler
}

// Handle routes the request to the appropriate handler.
func (hr *HandlerRegistry) Handle(hctx *HandlerContext) error {
	handler, exists := hr.handlers[hctx.Request.Action]
	if !exists {
		return fmt.Errorf("unknown action: %d", hctx.Request.Action)
	}
	if hctx.Request.Action != common.CheckFingerprint && !hctx.HubVerified {
		return errors.New("hub not verified")
	}
	return handler.Handle(hctx)
}

// GetHandler returns the handler for a specific action.
func (hr *HandlerRegistry) GetHandler(action common.WebSocketAction) (RequestHandler, bool) {
	handler, exists := hr.handlers[action]
	return handler, exists
}

////////////////////////////////////////////////////////////////////////////

// GetAgentInfoHandler handles agent info requests from the hub.
type GetAgentInfoHandler struct{}

func (h *GetAgentInfoHandler) Handle(hctx *HandlerContext) error {
	slog.Debug("GetAgentInfo request received")
	hostname, _ := os.Hostname()
	info := map[string]any{
		"version": app.Version,
		"capabilities": map[string]any{
			"docker": collectors.DockerAvailable(),
		},
		"metadata": map[string]any{
			"hostname": hostname,
		},
	}
	return hctx.SendResponse(info, hctx.RequestID)
}

////////////////////////////////////////////////////////////////////////////

// PingHandler responds to liveness checks from the hub.
type PingHandler struct{}

func (h *PingHandler) Handle(hctx *HandlerContext) error {
	slog.Debug("Ping request received")
	return hctx.SendResponse(map[string]any{"pong": true}, hctx.RequestID)
}

////////////////////////////////////////////////////////////////////////////

// CheckFingerprintHandler handles authentication challenges.
type CheckFingerprintHandler struct{}

func (h *CheckFingerprintHandler) Handle(hctx *HandlerContext) error {
	return hctx.Client.handleAuthChallenge(hctx.Request, hctx.RequestID)
}

////////////////////////////////////////////////////////////////////////////

// GetHostSnapshotHandler handles host snapshot collection requests from the hub.
type GetHostSnapshotHandler struct{}

func (h *GetHostSnapshotHandler) Handle(hctx *HandlerContext) error {
	slog.Debug("GetHostSnapshot request received")
	snapshot := collectors.CollectSnapshot()
	return hctx.SendResponse(snapshot, hctx.RequestID)
}

////////////////////////////////////////////////////////////////////////////

// GetHostMetricsHandler handles lightweight host metrics collection requests from the hub.
type GetHostMetricsHandler struct{}

func (h *GetHostMetricsHandler) Handle(hctx *HandlerContext) error {
	slog.Debug("GetHostMetrics request received")
	metrics := collectors.CollectMetrics()
	return hctx.SendResponse(metrics, hctx.RequestID)
}

////////////////////////////////////////////////////////////////////////////

// GetContainerMetricsHandler handles lightweight running-container metrics collection requests from the hub.
type GetContainerMetricsHandler struct{}

func (h *GetContainerMetricsHandler) Handle(hctx *HandlerContext) error {
	slog.Debug("GetContainerMetrics request received")
	metrics := collectors.CollectContainerMetrics()
	return hctx.SendResponse(metrics, hctx.RequestID)
}
