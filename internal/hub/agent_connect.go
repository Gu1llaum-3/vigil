package hub

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/Gu1llaum-3/vigil/internal/common"
	"github.com/Gu1llaum-3/vigil/internal/hub/expirymap"
	"github.com/Gu1llaum-3/vigil/internal/hub/notifications"
	"github.com/Gu1llaum-3/vigil/internal/hub/ws"

	"github.com/blang/semver"
	"github.com/lxzan/gws"
	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// agentConnectRequest holds information related to an agent's connection attempt.
type agentConnectRequest struct {
	hub               *Hub
	req               *http.Request
	res               http.ResponseWriter
	token             string
	agentSemVer       semver.Version
	isEnrollmentToken bool
	userId            string
}

// enrollmentTokenMap stores active enrollment tokens and their associated user IDs.
var enrollmentTokenMap tokenMap

type tokenMap struct {
	store *expirymap.ExpiryMap[string]
	once  sync.Once
}

// GetMap returns the expirymap, creating it if necessary.
func (tm *tokenMap) GetMap() *expirymap.ExpiryMap[string] {
	tm.once.Do(func() {
		tm.store = expirymap.New[string](time.Hour)
	})
	return tm.store
}

// handleAgentConnect is the HTTP handler for an agent's connection request.
func (h *Hub) handleAgentConnect(e *core.RequestEvent) error {
	agentRequest := agentConnectRequest{req: e.Request, res: e.Response, hub: h}
	_ = agentRequest.agentConnect()
	return nil
}

// agentConnect validates agent credentials and upgrades the connection to a WebSocket.
func (acr *agentConnectRequest) agentConnect() (err error) {
	var agentVersion string

	acr.token, agentVersion, err = acr.validateAgentHeaders(acr.req.Header)
	if err != nil {
		return acr.sendResponseError(acr.res, http.StatusBadRequest, "")
	}

	// Check if token is an active enrollment token (in-memory)
	acr.userId, acr.isEnrollmentToken = enrollmentTokenMap.GetMap().GetOk(acr.token)
	if !acr.isEnrollmentToken {
		// Fallback: check for a permanent enrollment token stored in the DB
		if rec, err := acr.hub.FindFirstRecordByFilter("agent_enrollment_tokens", "token = {:token}", dbx.Params{"token": acr.token}); err == nil {
			if userID := rec.GetString("created_by"); userID != "" {
				acr.userId = userID
				acr.isEnrollmentToken = true
			}
		}
	}

	// Find matching agent records for this token
	agentRecords := getAgentsByToken(acr.token, acr.hub)
	if len(agentRecords) == 0 && !acr.isEnrollmentToken {
		return acr.sendResponseError(acr.res, http.StatusUnauthorized, "Invalid token")
	}

	acr.agentSemVer, err = semver.Parse(agentVersion)
	if err != nil {
		return acr.sendResponseError(acr.res, http.StatusUnauthorized, "Invalid agent version")
	}

	conn, err := ws.GetUpgrader().Upgrade(acr.res, acr.req)
	if err != nil {
		return acr.sendResponseError(acr.res, http.StatusInternalServerError, "WebSocket upgrade failed")
	}

	go acr.verifyWsConn(conn, agentRecords)
	return nil
}

// AgentRecord holds agent data from the agents collection.
type AgentRecord struct {
	Id          string `db:"id"`
	Token       string `db:"token"`
	Fingerprint string `db:"fingerprint"`
	Status      string `db:"status"`
	Version     string `db:"version"`
}

// verifyWsConn verifies the WebSocket connection using the agent's fingerprint.
func (acr *agentConnectRequest) verifyWsConn(conn *gws.Conn, agentRecords []AgentRecord) (err error) {
	wsConn := ws.NewWsConnection(conn, acr.agentSemVer)
	conn.Session().Store("wsConn", wsConn)

	defer func() {
		if err != nil {
			wsConn.Close([]byte(err.Error()))
		}
	}()

	go conn.ReadLoop()

	signer, err := acr.hub.GetSSHKey("")
	if err != nil {
		return err
	}

	agentFingerprint, err := wsConn.GetFingerprint(context.Background(), acr.token, signer)
	if err != nil {
		return err
	}

	agentRec, err := acr.findOrUpsertAgent(agentRecords, agentFingerprint.Fingerprint)
	if err != nil {
		return err
	}

	// Track the live connection for later hub-initiated requests.
	acr.hub.agentConns.Store(agentRec.Id, wsConn)

	// Fetch initial agent info (version, capabilities, metadata) and persist it.
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if info, infoErr := wsConn.GetAgentInfo(ctx); infoErr == nil {
		acr.hub.updateAgentInfo(agentRec.Id, info)
	} else {
		slog.Warn("Failed to fetch agent info", "agent", agentRec.Id, "err", infoErr)
	}

	// Collect initial host snapshot.
	snapshotCtx, snapshotCancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer snapshotCancel()
	if snapshot, snapshotErr := wsConn.GetHostSnapshot(snapshotCtx); snapshotErr == nil {
		acr.hub.upsertHostSnapshot(agentRec.Id, snapshot)
	} else {
		slog.Warn("Failed to fetch host snapshot", "agent", agentRec.Id, "err", snapshotErr)
	}

	// Keep the connection alive and detect disconnection.
	go acr.hub.manageAgentLifecycle(wsConn, agentRec.Id)
	return nil
}

// validateAgentHeaders extracts and validates token and version from HTTP headers.
func (acr *agentConnectRequest) validateAgentHeaders(headers http.Header) (string, string, error) {
	token := headers.Get("X-Token")
	agentVersion := headers.Get("X-App")

	if agentVersion == "" || token == "" || len(token) > 64 {
		return "", "", errors.New("missing or invalid headers")
	}
	return token, agentVersion, nil
}

// sendResponseError writes an HTTP error response.
func (acr *agentConnectRequest) sendResponseError(res http.ResponseWriter, code int, message string) error {
	res.WriteHeader(code)
	if message != "" {
		res.Write([]byte(message))
	}
	return nil
}

// getAgentsByToken retrieves all agent records for a given token.
func getAgentsByToken(token string, h *Hub) []AgentRecord {
	var records []AgentRecord
	_ = h.DB().NewQuery("SELECT id, token, fingerprint, status, version FROM agents WHERE token = {:token}").
		Bind(dbx.Params{"token": token}).
		All(&records)
	return records
}

// findOrUpsertAgent validates an agent fingerprint or creates a new agent record.
func (acr *agentConnectRequest) findOrUpsertAgent(agentRecords []AgentRecord, fingerprint string) (AgentRecord, error) {
	version := acr.agentSemVer.String()

	// Match existing agent by fingerprint
	for _, rec := range agentRecords {
		if rec.Fingerprint == fingerprint {
			// Matching fingerprint - update status, version, and last_seen
			if err := acr.hub.UpdateAgent(&rec, fingerprint, "connected", version); err != nil {
				return rec, err
			}
			return rec, nil
		}
		if rec.Fingerprint == "" {
			// First connection: store the fingerprint
			if err := acr.hub.UpdateAgent(&rec, fingerprint, "connected", version); err != nil {
				return rec, err
			}
			rec.Fingerprint = fingerprint
			return rec, nil
		}
	}

	// Enrollment token path - create new agent
	if acr.isEnrollmentToken && acr.userId != "" {
		newRec := AgentRecord{Token: acr.token}
		if err := acr.hub.CreateAgent(&newRec, fingerprint, acr.userId, version); err != nil {
			return newRec, err
		}
		newRec.Fingerprint = fingerprint
		return newRec, nil
	}

	if len(agentRecords) == 1 {
		return agentRecords[0], errors.New("fingerprint mismatch")
	}

	return AgentRecord{}, errors.New("no matching agent record")
}

// UpdateAgent updates an agent's fingerprint, status, version, and last_seen.
func (h *Hub) UpdateAgent(record *AgentRecord, fingerprint, status, version string) error {
	rec, err := h.FindRecordById("agents", record.Id)
	if err != nil {
		return err
	}
	previous := rec.GetString("status")
	rec.Set("fingerprint", fingerprint)
	rec.Set("status", status)
	rec.Set("version", version)
	rec.Set("last_seen", time.Now())
	if err := h.SaveNoValidate(rec); err != nil {
		return err
	}

	if previous == "offline" && status != "offline" {
		h.notifier.Dispatch(notifications.Event{
			Kind:       notifications.KindForAgent(status),
			OccurredAt: time.Now(),
			Resource: notifications.ResourceRef{
				ID:   rec.Id,
				Name: rec.GetString("name"),
				Type: "agent",
			},
			Previous: previous,
			Current:  status,
		})
	}

	return nil
}

// CreateAgent creates a new agent record for self-registered agents.
func (h *Hub) CreateAgent(record *AgentRecord, fingerprint, userId, version string) error {
	collection, err := h.FindCachedCollectionByNameOrId("agents")
	if err != nil {
		return err
	}
	rec := core.NewRecord(collection)
	rec.Set("token", record.Token)
	rec.Set("fingerprint", fingerprint)
	rec.Set("status", "connected")
	rec.Set("version", version)
	rec.Set("last_seen", time.Now())
	if userId != "" {
		rec.Set("created_by", userId)
	}
	if err := h.SaveNoValidate(rec); err != nil {
		return err
	}
	record.Id = rec.Id
	return nil
}

const agentPingInterval = 30 * time.Second

// agentOfflineGracePeriod is the time to wait after a WebSocket disconnect before
// marking an agent offline. This absorbs brief connection drops caused by service
// restarts or upgrades: the agent typically reconnects within a few seconds, so
// waiting 30s prevents spurious offline notifications and status flaps.
// Combined with the 5s delay already applied in ws.go OnClose, the total window
// before an offline status is written is ~35s.
// Ping failures bypass this grace period and mark offline immediately.
const agentOfflineGracePeriod = 30 * time.Second

// manageAgentLifecycle keeps the connection alive with periodic pings and sets
// the agent status to offline when the connection drops.
func (h *Hub) manageAgentLifecycle(wsConn *ws.WsConn, agentId string) {
	slog.Info("Agent connected", "id", agentId)
	ticker := time.NewTicker(agentPingInterval)
	defer ticker.Stop()
	for {
		select {
		case <-wsConn.DownChan:
			// CompareAndDelete ensures we only remove this specific WsConn pointer.
			// A rapid restart may have already stored a new WsConn for the same
			// agentId — a plain Delete would evict it and leave the hub blind.
			h.agentConns.CompareAndDelete(agentId, wsConn)
			slog.Info("Agent disconnected", "id", agentId)
			time.Sleep(agentOfflineGracePeriod)
			if _, stillConnected := h.agentConns.Load(agentId); !stillConnected {
				h.setAgentStatus(agentId, "offline")
			}
			return
		case <-ticker.C:
			if err := wsConn.Ping(); err != nil {
				h.agentConns.CompareAndDelete(agentId, wsConn)
				h.setAgentStatus(agentId, "offline")
				slog.Warn("Agent ping failed", "id", agentId, "err", err)
				return
			}
		}
	}
}

// setAgentStatus updates the status field of an agent record and emits a notification on transition.
func (h *Hub) setAgentStatus(agentId, status string) {
	rec, err := h.FindRecordById("agents", agentId)
	if err != nil {
		return
	}
	previous := rec.GetString("status")
	if previous == status {
		return
	}
	rec.Set("status", status)
	if err := h.SaveNoValidate(rec); err != nil {
		return
	}
	h.notifier.Dispatch(notifications.Event{
		Kind:       notifications.KindForAgent(status),
		OccurredAt: time.Now(),
		Resource: notifications.ResourceRef{
			ID:   agentId,
			Name: rec.GetString("name"),
			Type: "agent",
		},
		Previous: previous,
		Current:  status,
	})
}

// updateAgentInfo persists capabilities and metadata returned by GetAgentInfo.
func (h *Hub) updateAgentInfo(agentId string, info common.AgentInfoResponse) {
	rec, err := h.FindRecordById("agents", agentId)
	if err != nil {
		return
	}
	if hostname, ok := info.Metadata["hostname"].(string); ok && hostname != "" {
		rec.Set("name", hostname)
	}
	rec.Set("capabilities", info.Capabilities)
	rec.Set("metadata", info.Metadata)
	_ = h.SaveNoValidate(rec)
}

// getRealIP extracts the client's real IP address from request headers.
func getRealIP(r *http.Request) string {
	if ip := r.Header.Get("CF-Connecting-IP"); ip != "" {
		return ip
	}
	if ip := r.Header.Get("X-Forwarded-For"); ip != "" {
		ips := strings.Split(ip, ",")
		if len(ips) > 0 {
			return strings.TrimSpace(ips[0])
		}
	}
	ip, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return ip
}
