package ws

import (
	"context"

	"github.com/fxamacker/cbor/v2"
	"github.com/Gu1llaum-3/vigil/internal/common"
	"github.com/lxzan/gws"
	"golang.org/x/crypto/ssh"
)

// ResponseHandler defines interface for handling agent responses.
type ResponseHandler interface {
	Handle(agentResponse common.AgentResponse) error
}

////////////////////////////////////////////////////////////////////////////
// Fingerprint handling (used for WebSocket authentication)
////////////////////////////////////////////////////////////////////////////

// fingerprintHandler implements ResponseHandler for fingerprint requests.
type fingerprintHandler struct {
	result *common.FingerprintResponse
}

func (h *fingerprintHandler) Handle(agentResponse common.AgentResponse) error {
	if len(agentResponse.Data) == 0 {
		return cbor.Unmarshal(agentResponse.Data, h.result)
	}
	return cbor.Unmarshal(agentResponse.Data, h.result)
}

////////////////////////////////////////////////////////////////////////////
// Agent info (used to fetch version, capabilities, metadata after auth)
////////////////////////////////////////////////////////////////////////////

// agentInfoHandler implements ResponseHandler for GetAgentInfo requests.
type agentInfoHandler struct {
	result *common.AgentInfoResponse
}

func (h *agentInfoHandler) Handle(agentResponse common.AgentResponse) error {
	return cbor.Unmarshal(agentResponse.Data, h.result)
}

// GetAgentInfo requests identity and capability info from the connected agent.
func (ws *WsConn) GetAgentInfo(ctx context.Context) (common.AgentInfoResponse, error) {
	if !ws.IsConnected() {
		return common.AgentInfoResponse{}, gws.ErrConnClosed
	}
	req, err := ws.requestManager.SendRequest(ctx, common.GetAgentInfo, nil)
	if err != nil {
		return common.AgentInfoResponse{}, err
	}
	var result common.AgentInfoResponse
	handler := &agentInfoHandler{result: &result}
	err = ws.handleAgentRequest(req, handler)
	return result, err
}

////////////////////////////////////////////////////////////////////////////
// Host snapshot (used to collect full system state from the agent)
////////////////////////////////////////////////////////////////////////////

// hostSnapshotHandler implements ResponseHandler for GetHostSnapshot requests.
type hostSnapshotHandler struct {
	result *common.HostSnapshotResponse
}

func (h *hostSnapshotHandler) Handle(agentResponse common.AgentResponse) error {
	return cbor.Unmarshal(agentResponse.Data, h.result)
}

// GetHostSnapshot requests a full system snapshot from the connected agent.
func (ws *WsConn) GetHostSnapshot(ctx context.Context) (common.HostSnapshotResponse, error) {
	if !ws.IsConnected() {
		return common.HostSnapshotResponse{}, gws.ErrConnClosed
	}
	req, err := ws.requestManager.SendRequest(ctx, common.GetHostSnapshot, nil)
	if err != nil {
		return common.HostSnapshotResponse{}, err
	}
	var result common.HostSnapshotResponse
	handler := &hostSnapshotHandler{result: &result}
	err = ws.handleAgentRequest(req, handler)
	return result, err
}

////////////////////////////////////////////////////////////////////////////
// Fingerprint handling (used for WebSocket authentication)
////////////////////////////////////////////////////////////////////////////

// GetFingerprint authenticates with the agent using SSH signature and returns the agent's fingerprint.
func (ws *WsConn) GetFingerprint(ctx context.Context, token string, signer ssh.Signer) (common.FingerprintResponse, error) {
	if !ws.IsConnected() {
		return common.FingerprintResponse{}, gws.ErrConnClosed
	}

	challenge := []byte(token)
	signature, err := signer.Sign(nil, challenge)
	if err != nil {
		return common.FingerprintResponse{}, err
	}

	req, err := ws.requestManager.SendRequest(ctx, common.CheckFingerprint, common.FingerprintRequest{
		Signature: signature.Blob,
	})
	if err != nil {
		return common.FingerprintResponse{}, err
	}

	var result common.FingerprintResponse
	handler := &fingerprintHandler{result: &result}
	err = ws.handleAgentRequest(req, handler)
	return result, err
}
