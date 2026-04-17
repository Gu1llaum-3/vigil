// Package transport provides a unified abstraction for hub-agent communication
// over different transports (WebSocket, SSH).
package transport

import (
	"context"
	"errors"
	"fmt"

	"github.com/fxamacker/cbor/v2"
	"github.com/Gu1llaum-3/vigil/internal/common"
)

// Transport defines the interface for hub-agent communication.
// Both WebSocket and SSH transports implement this interface.
type Transport interface {
	// Request sends a request to the agent and unmarshals the response into dest.
	// The dest parameter should be a pointer to the expected response type.
	Request(ctx context.Context, action common.WebSocketAction, req any, dest any) error
	// IsConnected returns true if the transport connection is active.
	IsConnected() bool
	// Close terminates the transport connection.
	Close()
}

// UnmarshalResponse unmarshals an AgentResponse into the destination type.
func UnmarshalResponse(resp common.AgentResponse, _ common.WebSocketAction, dest any) error {
	if dest == nil {
		return errors.New("nil destination")
	}
	if len(resp.Data) == 0 {
		return errors.New("empty response data")
	}
	if err := cbor.Unmarshal(resp.Data, dest); err != nil {
		return fmt.Errorf("failed to unmarshal response data: %w", err)
	}
	return nil
}
