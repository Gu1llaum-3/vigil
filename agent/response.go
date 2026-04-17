package agent

import (
	"github.com/fxamacker/cbor/v2"
	"github.com/Gu1llaum-3/vigil/internal/common"
)

// newAgentResponse creates an AgentResponse with CBOR-encoded data.
func newAgentResponse(data any, requestID *uint32) common.AgentResponse {
	response := common.AgentResponse{Id: requestID}
	response.Data, _ = cbor.Marshal(data)
	return response
}
