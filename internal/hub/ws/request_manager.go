package ws

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Gu1llaum-3/vigil/internal/common"
	"github.com/fxamacker/cbor/v2"
	"github.com/lxzan/gws"
)

// RequestID uniquely identifies a request
type RequestID uint32

// PendingRequest tracks an in-flight request
type PendingRequest struct {
	ID         RequestID
	ResponseCh chan *gws.Message
	Context    context.Context
	Cancel     context.CancelFunc
	CreatedAt  time.Time
}

// RequestManager handles concurrent requests to an agent
type RequestManager struct {
	sync.RWMutex
	conn        *gws.Conn
	pendingReqs map[RequestID]*PendingRequest
	nextID      atomic.Uint32

	deadlineMu sync.Mutex
	deadlineAt time.Time
	lastHeard  time.Time
}

// NewRequestManager creates a new request manager for a WebSocket connection
func NewRequestManager(conn *gws.Conn) *RequestManager {
	rm := &RequestManager{
		conn:        conn,
		pendingReqs: make(map[RequestID]*PendingRequest),
		lastHeard:   time.Now(),
	}
	return rm
}

// SendRequest sends a request and returns a channel for the response
func (rm *RequestManager) SendRequest(ctx context.Context, action common.WebSocketAction, data any) (*PendingRequest, error) {
	reqID := RequestID(rm.nextID.Add(1))

	// Respect any caller-provided deadline. If none is set, apply a reasonable default
	// so pending requests don't live forever if the agent never responds.
	var reqCtx context.Context
	var cancel context.CancelFunc
	if _, hasDeadline := ctx.Deadline(); hasDeadline {
		reqCtx, cancel = context.WithCancel(ctx)
	} else {
		reqCtx, cancel = context.WithTimeout(ctx, 5*time.Second)
	}

	req := &PendingRequest{
		ID:         reqID,
		ResponseCh: make(chan *gws.Message, 1),
		Context:    reqCtx,
		Cancel:     cancel,
		CreatedAt:  time.Now(),
	}

	rm.Lock()
	rm.pendingReqs[reqID] = req
	rm.Unlock()

	hubReq := common.HubRequest[any]{
		Id:     (*uint32)(&reqID),
		Action: action,
		Data:   data,
	}

	// Give a busy agent until the request's own deadline (plus a margin) to answer.
	if requestDeadline, ok := reqCtx.Deadline(); ok {
		rm.requestSent(requestDeadline)
	}

	// Send the request
	if err := rm.sendMessage(hubReq); err != nil {
		rm.cancelRequest(reqID)
		return nil, fmt.Errorf("failed to send request: %w", err)
	}

	// Start cleanup watcher for timeout/cancellation
	go rm.cleanupRequest(req)

	return req, nil
}

// sendMessage encodes and sends a message over WebSocket
func (rm *RequestManager) sendMessage(data any) error {
	if rm.conn == nil {
		return gws.ErrConnClosed
	}

	bytes, err := cbor.Marshal(data)
	if err != nil {
		return fmt.Errorf("failed to marshal request: %w", err)
	}

	return rm.conn.WriteMessage(gws.OpcodeBinary, bytes)
}

// handleResponse processes a single response message
func (rm *RequestManager) handleResponse(message *gws.Message) {
	var response common.AgentResponse
	if err := cbor.Unmarshal(message.Data.Bytes(), &response); err != nil {
		// Legacy response without ID - route to first pending request of any type
		rm.routeLegacyResponse(message)
		return
	}

	if response.Id == nil {
		rm.routeLegacyResponse(message)
		return
	}

	reqID := RequestID(*response.Id)

	rm.RLock()
	req, exists := rm.pendingReqs[reqID]
	rm.RUnlock()

	if !exists {
		// Request not found (might have timed out) - close the message
		message.Close()
		return
	}

	select {
	case req.ResponseCh <- message:
		// Message successfully delivered - the receiver will close it
		rm.deleteRequest(reqID)
	case <-req.Context.Done():
		// Request was cancelled/timed out - close the message
		message.Close()
	}
}

// routeLegacyResponse handles responses that don't have request IDs (backwards compatibility)
func (rm *RequestManager) routeLegacyResponse(message *gws.Message) {
	// Snapshot the oldest pending request without holding the lock during send
	rm.RLock()
	var oldestReq *PendingRequest
	for _, req := range rm.pendingReqs {
		if oldestReq == nil || req.CreatedAt.Before(oldestReq.CreatedAt) {
			oldestReq = req
		}
	}
	rm.RUnlock()

	if oldestReq != nil {
		select {
		case oldestReq.ResponseCh <- message:
			// Message successfully delivered - the receiver will close it
			rm.deleteRequest(oldestReq.ID)
		case <-oldestReq.Context.Done():
			// Request was cancelled - close the message
			message.Close()
		}
	} else {
		// No pending requests - close the message
		message.Close()
	}
}

// cleanupRequest handles request timeout and cleanup
func (rm *RequestManager) cleanupRequest(req *PendingRequest) {
	<-req.Context.Done()
	rm.cancelRequest(req.ID)
}

// cancelRequest removes a request and cancels its context
func (rm *RequestManager) cancelRequest(reqID RequestID) {
	rm.Lock()
	defer rm.Unlock()

	if req, exists := rm.pendingReqs[reqID]; exists {
		req.Cancel()
		delete(rm.pendingReqs, reqID)
	}
}

// The connection deadline (read and write) only moves forward, and only for two reasons:
//   - a frame arrived from the agent (receivedFrame): the deadline becomes now + the deadline;
//   - a request was sent (requestSent): the agent runs its handlers on its read loop, so it
//     sends nothing, not even pongs, while a slow snapshot runs. The deadline then covers the
//     request's own deadline plus half the deadline, but never beyond twice the deadline
//     after the agent was last heard from: a peer that stays silent that long is dead, even
//     when the hub polls it more often than its requests time out.
//
// The hub's own pings never extend it.

func (rm *RequestManager) receivedFrame() {
	now := time.Now()
	rm.deadlineMu.Lock()
	defer rm.deadlineMu.Unlock()
	rm.lastHeard = now
	rm.extendDeadlineLocked(now.Add(time.Duration(deadlineNanos.Load())))
}

func (rm *RequestManager) requestSent(requestDeadline time.Time) {
	d := time.Duration(deadlineNanos.Load())
	rm.deadlineMu.Lock()
	defer rm.deadlineMu.Unlock()
	t := requestDeadline.Add(d / 2)
	if limit := rm.lastHeard.Add(2 * d); t.After(limit) {
		t = limit
	}
	rm.extendDeadlineLocked(t)
}

func (rm *RequestManager) extendDeadlineLocked(t time.Time) {
	if !t.After(rm.deadlineAt) {
		return
	}
	rm.deadlineAt = t
	if rm.conn != nil {
		_ = rm.conn.SetDeadline(t)
	}
}

// deleteRequest removes a request from the pending map without cancelling its context.
func (rm *RequestManager) deleteRequest(reqID RequestID) {
	rm.Lock()
	defer rm.Unlock()
	delete(rm.pendingReqs, reqID)
}

// Close shuts down the request manager
func (rm *RequestManager) Close() {
	rm.Lock()
	defer rm.Unlock()

	// Cancel all pending requests
	for _, req := range rm.pendingReqs {
		req.Cancel()
	}
	rm.pendingReqs = make(map[RequestID]*PendingRequest)
}
