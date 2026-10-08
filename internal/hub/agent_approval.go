package hub

import (
	"context"
	"net/http"
	"time"

	"github.com/Gu1llaum-3/vigil/internal/common"
	"github.com/Gu1llaum-3/vigil/internal/hub/ws"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/security"
)

// A host awaiting approval connected with the enrollment token using the fingerprint of a
// host that has its own token (see awaitApproval). The admin decides what it is:
//   - approve: a different machine; it becomes a host of its own (it gets its own token on
//     its next connection, like any enrollment);
//   - merge: the claimed host, reinstalled; it takes over that record and its history, with
//     a new token pushed to it now (it must be connected);
//   - reject: someone else; the pending record is deleted and the connection closed.

func (h *Hub) loadAwaitingAgent(e *core.RequestEvent) (*core.Record, error) {
	rec, err := h.FindRecordById("agents", e.Request.PathValue("id"))
	if err != nil {
		return nil, e.NotFoundError("Agent not found", nil)
	}
	if rec.GetString("status") != agentStatusAwaitingApproval {
		return nil, e.BadRequestError("This host is not awaiting approval", nil)
	}
	return rec, nil
}

// dropAgentConn closes the live connection of an agent, if any, so it reconnects under the
// identity just decided.
func (h *Hub) dropAgentConn(agentID string) {
	if conn, ok := h.agentConns.LoadAndDelete(agentID); ok {
		conn.(*ws.WsConn).Close([]byte("identity changed"))
	}
}

// approveAgent makes the pending host a host of its own: it is issued its own token right
// away, through its live connection, so the approved record never sits on the shared token
// (where anyone holding it could claim it before the host reconnects).
func (h *Hub) approveAgent(e *core.RequestEvent) error {
	rec, err := h.loadAwaitingAgent(e)
	if err != nil {
		return err
	}
	conn, ok := h.agentConns.Load(rec.Id)
	if !ok || !agentRecordHasCapability(rec, common.AgentTokenCapability) {
		return e.JSON(http.StatusConflict, map[string]string{"message": "The host must be connected (and run an agent that receives tokens) to be approved"})
	}
	token := security.RandomString(40)
	ctx, cancel := context.WithTimeout(e.Request.Context(), 10*time.Second)
	defer cancel()
	if err := conn.(*ws.WsConn).SetAgentToken(ctx, token); err != nil {
		return e.InternalServerError("The host did not confirm its new token; nothing was changed", err)
	}
	rec.Set("token", token)
	rec.Set("token_issued", true)
	rec.Set("status", "offline")
	rec.Set("duplicate_of", "")
	if err := h.SaveNoValidate(rec); err != nil {
		return err
	}
	// It reconnects with its token as a regular host, and collection starts.
	h.dropAgentConn(rec.Id)
	return e.JSON(http.StatusOK, map[string]bool{"ok": true})
}

// mergeAgent hands the claimed host's record to the pending host: a new token for that
// record is pushed to the pending connection, then the pending record is deleted.
func (h *Hub) mergeAgent(e *core.RequestEvent) error {
	pending, err := h.loadAwaitingAgent(e)
	if err != nil {
		return err
	}
	target, err := h.FindRecordById("agents", pending.GetString("duplicate_of"))
	if err != nil {
		return e.BadRequestError("The host it claims to be no longer exists; approve it instead", nil)
	}
	// The claimed host connected right now is the strongest sign that the pending one is
	// someone else: merging would hand its record over. Only with force, after checking.
	if _, live := h.agentConns.Load(target.Id); live && e.Request.URL.Query().Get("force") != "1" {
		return e.JSON(http.StatusConflict, map[string]string{"message": "The host it claims to be is connected right now: the pending host is probably not a reinstall of it. Check both before merging."})
	}
	conn, ok := h.agentConns.Load(pending.Id)
	if !ok || !agentRecordHasCapability(pending, common.AgentTokenCapability) {
		return e.JSON(http.StatusConflict, map[string]string{"message": "The host must be connected (and run an agent that receives tokens) to be merged"})
	}
	token := security.RandomString(40)
	ctx, cancel := context.WithTimeout(e.Request.Context(), 10*time.Second)
	defer cancel()
	if err := conn.(*ws.WsConn).SetAgentToken(ctx, token); err != nil {
		return e.InternalServerError("The host did not confirm its new token; nothing was changed", err)
	}
	target.IgnoreUnchangedFields(true)
	target.Set("token", token)
	target.Set("token_issued", true)
	if err := h.SaveNoValidate(target); err != nil {
		return err
	}
	if err := h.Delete(pending); err != nil {
		return err
	}
	// It reconnects with the new token and its fingerprint, i.e. as the claimed host; a
	// connection still on the old token (forced merge) is dropped.
	h.dropAgentConn(pending.Id)
	h.dropAgentConn(target.Id)
	return e.JSON(http.StatusOK, map[string]bool{"ok": true})
}

// rejectAgent deletes the pending host and closes its connection. It may connect again with
// the enrollment token (and wait for approval again): regenerate the enrollment token to stop it.
func (h *Hub) rejectAgent(e *core.RequestEvent) error {
	rec, err := h.loadAwaitingAgent(e)
	if err != nil {
		return err
	}
	if err := h.Delete(rec); err != nil {
		return err
	}
	h.dropAgentConn(rec.Id)
	return e.JSON(http.StatusOK, map[string]bool{"ok": true})
}

// protectAgentIdentityFields keeps the generic collection API (open to non-readonly users)
// away from the fields that decide an agent's identity: status, duplicate_of, token_issued and
// token change only through the hub (handshake, approval and rotation endpoints), and a host
// awaiting approval
// keeps its fingerprint (resetting it would let it adopt a record of its own). Superusers
// (the PocketBase dashboard) are not restricted.
func protectAgentIdentityFields(e *core.RecordRequestEvent) error {
	if e.HasSuperuserAuth() {
		return e.Next()
	}
	original := e.Record.Original()
	for _, field := range []string{"status", "duplicate_of", "token_issued", "token"} {
		if e.Record.GetString(field) != original.GetString(field) {
			return e.ForbiddenError("The "+field+" of an agent is managed by the hub", nil)
		}
	}
	if original.GetString("status") == agentStatusAwaitingApproval && e.Record.GetString("fingerprint") != original.GetString("fingerprint") {
		return e.ForbiddenError("A host awaiting approval keeps its fingerprint until an admin decides", nil)
	}
	// Resetting the fingerprint of a host with its own token lifts the guard that sends
	// enrollment-token connections with that fingerprint to approval: admins only.
	if original.GetBool("token_issued") && e.Record.GetString("fingerprint") != original.GetString("fingerprint") &&
		e.Auth.GetString("role") != "admin" {
		return e.ForbiddenError("Only an admin can reset the fingerprint of a host that has its own token", nil)
	}
	return e.Next()
}
