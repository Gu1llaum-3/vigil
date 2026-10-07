package hub

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
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
	"github.com/pocketbase/pocketbase/tools/types"
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

	// The hub is shutting down: no new connection goroutines.
	if acr.hub.agentCtx.Err() != nil {
		return acr.sendResponseError(acr.res, http.StatusServiceUnavailable, "Hub is shutting down")
	}

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

	if !acr.hub.goAgent(func() { _ = acr.verifyWsConn(conn, agentRecords) }) {
		// The hub started stopping after the check above; the connection is already
		// upgraded, so close it instead of answering 503.
		_ = conn.WriteClose(1001, []byte("hub shutting down"))
	}
	return nil
}

// AgentRecord holds agent data from the agents collection.
type AgentRecord struct {
	Id          string `db:"id"`
	Token       string `db:"token"`
	Fingerprint string `db:"fingerprint"`
	Status      string `db:"status"`
	Version     string `db:"version"`
	Name        string `db:"name"`
}

// logName returns a human-readable label for the agent (its hostname-derived name),
// falling back to the record id when no name is set yet.
func (a AgentRecord) logName() string {
	if a.Name != "" {
		return a.Name
	}
	return a.Id
}

// verifyWsConn verifies the WebSocket connection using the agent's fingerprint.
func (acr *agentConnectRequest) verifyWsConn(conn *gws.Conn, agentRecords []AgentRecord) (err error) {
	wsConn := ws.NewWsConnection(conn)
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

	agentFingerprint, err := wsConn.GetFingerprint(acr.hub.agentCtx, acr.token, signer)
	if err != nil {
		return err
	}

	agentRec, firstEnroll, err := acr.findOrUpsertAgent(agentRecords, agentFingerprint.Fingerprint)
	if err != nil {
		return err
	}

	// Track the live connection for later hub-initiated requests.
	acr.hub.registerAgentConn(agentRec.Id, wsConn)

	// Fetch initial agent info (version, capabilities, metadata) and persist it.
	ctx, cancel := context.WithTimeout(acr.hub.agentCtx, 10*time.Second)
	defer cancel()
	if info, infoErr := wsConn.GetAgentInfo(ctx); infoErr == nil {
		acr.hub.updateAgentInfo(agentRec.Id, info, firstEnroll)
	} else {
		slog.Warn("Failed to fetch agent info", "agent", agentRec.logName(), "id", agentRec.Id, "err", infoErr)
	}

	// Collect initial host snapshot.
	snapshotCtx, snapshotCancel := context.WithTimeout(acr.hub.agentCtx, 60*time.Second)
	defer snapshotCancel()
	if snapshot, snapshotErr := wsConn.GetHostSnapshot(snapshotCtx); snapshotErr == nil {
		acr.hub.upsertHostSnapshot(agentRec.Id, snapshot)
	} else {
		slog.Warn("Failed to fetch host snapshot", "agent", agentRec.logName(), "id", agentRec.Id, "err", snapshotErr)
	}

	// Collect initial lightweight host metrics so the monitoring views populate immediately.
	metricsCtx, metricsCancel := context.WithTimeout(acr.hub.agentCtx, hostMetricsRequestTimeout)
	defer metricsCancel()
	if metrics, metricsErr := wsConn.GetHostMetrics(metricsCtx); metricsErr == nil {
		acr.hub.persistHostMetrics(agentRec.Id, metrics)
	} else {
		slog.Warn("Failed to fetch host metrics", "agent", agentRec.logName(), "id", agentRec.Id, "err", metricsErr)
	}

	containerMetricsCtx, containerMetricsCancel := context.WithTimeout(acr.hub.agentCtx, containerMetricsRequestTimeout)
	defer containerMetricsCancel()
	if metrics, metricsErr := wsConn.GetContainerMetrics(containerMetricsCtx); metricsErr == nil {
		acr.hub.insertContainerMetricSample(agentRec.Id, metrics)
	} else {
		slog.Warn("Failed to fetch container metrics", "agent", agentRec.logName(), "id", agentRec.Id, "err", metricsErr)
	}

	// Keep the connection alive and detect disconnection.
	acr.hub.goAgent(func() { acr.hub.manageAgentLifecycle(wsConn, agentRec.Id) })
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
		// The client may already be gone; nothing to do about it.
		_, _ = res.Write([]byte(message))
	}
	return nil
}

// getAgentsByToken retrieves all agent records for a given token.
func getAgentsByToken(token string, h *Hub) []AgentRecord {
	var records []AgentRecord
	_ = h.DB().NewQuery("SELECT id, token, fingerprint, status, version, name FROM agents WHERE token = {:token}").
		Bind(dbx.Params{"token": token}).
		All(&records)
	return records
}

// findOrUpsertAgent validates an agent fingerprint or creates a new agent record.
// The returned bool reports whether this is the agent's *first enrollment* — a
// brand-new record (enrollment token) OR a pre-created record associating its
// fingerprint for the first time. The caller uses it to seed agent-declared tags
// once at enrollment, never on a reconnect.
func (acr *agentConnectRequest) findOrUpsertAgent(agentRecords []AgentRecord, fingerprint string) (AgentRecord, bool, error) {
	version := acr.agentSemVer.String()

	// Match existing agent by fingerprint
	for _, rec := range agentRecords {
		if rec.Fingerprint == fingerprint {
			// Matching fingerprint - update status, version, and last_seen
			if err := acr.hub.UpdateAgent(&rec, fingerprint, "connected", version); err != nil {
				return rec, false, err
			}
			return rec, false, nil
		}
		if rec.Fingerprint == "" {
			// First connection of a pre-created agent: store the fingerprint. This is
			// still a first enrollment, so env-declared tags may be seeded.
			if err := acr.hub.UpdateAgent(&rec, fingerprint, "connected", version); err != nil {
				return rec, false, err
			}
			rec.Fingerprint = fingerprint
			return rec, true, nil
		}
	}

	// Enrollment token path - create new agent
	if acr.isEnrollmentToken && acr.userId != "" {
		newRec := AgentRecord{Token: acr.token}
		if err := acr.hub.CreateAgent(&newRec, fingerprint, acr.userId, version); err != nil {
			return newRec, false, err
		}
		newRec.Fingerprint = fingerprint
		return newRec, true, nil
	}

	if len(agentRecords) == 1 {
		return agentRecords[0], false, errors.New("fingerprint mismatch")
	}

	return AgentRecord{}, false, errors.New("no matching agent record")
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
		evt := notifications.Event{
			Kind:       notifications.KindForAgent(status),
			OccurredAt: time.Now(),
			Resource: notifications.ResourceRef{
				ID:   rec.Id,
				Name: rec.GetString("name"),
				Type: "agent",
			},
			Previous: previous,
			Current:  status,
		}
		h.emitNotification(evt)
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

// agentPingInterval is how often the hub pings each connected agent (a var so tests can
// shorten it).
var agentPingInterval = 30 * time.Second

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
		case <-h.agentCtx.Done():
			// The hub is stopping: the agent did not go offline, the hub did. Close the
			// connection here too, in case it was stored after stopAgentConnections
			// closed the others.
			h.agentConns.CompareAndDelete(agentId, wsConn)
			wsConn.Close([]byte("hub shutting down"))
			return
		case <-wsConn.DownChan:
			// select picks randomly among ready cases: never write a status because of
			// a shutdown that is already under way.
			if h.agentCtx.Err() != nil {
				return
			}
			// CompareAndDelete ensures we only remove this specific WsConn pointer.
			// A rapid restart may have already stored a new WsConn for the same
			// agentId — a plain Delete would evict it and leave the hub blind.
			h.agentConns.CompareAndDelete(agentId, wsConn)
			slog.Info("Agent disconnected", "id", agentId)
			select {
			case <-time.After(agentOfflineGracePeriod):
			case <-h.agentCtx.Done():
				return
			}
			h.markAgentOfflineUnlessReconnected(agentId)
			return
		case <-ticker.C:
			if h.agentCtx.Err() != nil {
				return
			}
			if err := wsConn.Ping(); err != nil {
				// Only the current connection speaks for the agent: after a restart the
				// old connection's ticker can fire once the new one is already stored.
				if h.agentConns.CompareAndDelete(agentId, wsConn) {
					slog.Warn("Agent ping failed", "id", agentId, "err", err)
					h.markAgentOfflineUnlessReconnected(agentId)
				} else {
					slog.Debug("Ping failed on a replaced agent connection", "id", agentId, "err", err)
				}
				wsConn.Close(nil)
				return
			}
		}
	}
}

// agentBootReconcileDelay is how long the hub gives agents to reconnect after it starts
// before it marks the ones still recorded as connected offline. Agents retry every 5–10s.
var agentBootReconcileDelay = 60 * time.Second

// startAgentStatusReconciler runs reconcileAgentStatuses once, agentBootReconcileDelay
// after boot. The status in the database survives a hub restart, but the lifecycle
// goroutine that would have marked a dead host offline does not: without this, a host that
// died while the hub was down (or during the offline grace period) stays connected forever.
func (h *Hub) startAgentStatusReconciler() {
	bootedAt := time.Now()
	delay := agentBootReconcileDelay
	h.goAgent(func() {
		select {
		case <-time.After(delay):
			h.reconcileAgentStatuses(bootedAt)
		case <-h.agentCtx.Done():
		}
	})
}

// reconcileAgentStatuses marks offline (with the usual notification) every agent still
// recorded as connected that has not connected since bootedAt and has no live connection.
// last_seen is written by every handshake before the connection is stored in agentConns,
// so an agent seen since boot is reconnecting (or inside its offline grace period) and is
// left to its own lifecycle.
func (h *Hub) reconcileAgentStatuses(bootedAt time.Time) {
	boot := bootedAt.UTC().Format(types.DefaultDateLayout)
	records, err := h.FindRecordsByFilter("agents", "status = 'connected' && (last_seen = '' || last_seen < {:boot})", "", 0, 0, dbx.Params{"boot": boot})
	if err != nil {
		slog.Warn("Agent status reconciliation failed", "err", err)
		return
	}
	for _, rec := range records {
		if h.agentCtx.Err() != nil {
			return
		}
		if _, connected := h.agentConns.Load(rec.Id); connected {
			continue
		}
		// Re-check inside the write transaction: a handshake may have saved the agent
		// between the query and now.
		marked := h.setAgentStatusIf(rec.Id, "offline", func(current *core.Record) bool {
			lastSeen := current.GetDateTime("last_seen")
			_, connected := h.agentConns.Load(current.Id)
			return !connected && (lastSeen.IsZero() || lastSeen.Time().Before(bootedAt))
		})
		if marked {
			slog.Info("Agent did not reconnect after hub start", "id", rec.Id, "name", rec.GetString("name"))
		}
	}
}

// registerAgentConn stores the agent's live connection, then re-asserts status=connected.
// The handshake wrote connected before this point, and an offline write from an older
// connection (failed ping, end of a grace period) can land in between, when that older
// connection was still the stored one; the re-assert repairs it.
func (h *Hub) registerAgentConn(agentId string, wsConn *ws.WsConn) {
	h.agentConns.Store(agentId, wsConn)
	h.setAgentStatusIf(agentId, "connected", func(*core.Record) bool {
		current, _ := h.agentConns.Load(agentId)
		return current == wsConn
	})
}

// markAgentOfflineUnlessReconnected writes status=offline unless a newer connection for the
// agent is registered in agentConns; the check runs in the write transaction. A reconnection
// still between its handshake and registerAgentConn can be overwritten, and
// registerAgentConn then restores connected.
func (h *Hub) markAgentOfflineUnlessReconnected(agentId string) {
	h.setAgentStatusIf(agentId, "offline", func(*core.Record) bool {
		_, reconnected := h.agentConns.Load(agentId)
		return !reconnected
	})
}

// setAgentStatusIf updates the status field of an agent record and emits a notification on
// transition, if cond (nil = always) holds for the current record in the same transaction as
// the write. It reports whether the status changed. Going offline also
// refreshes last_seen (otherwise only written at connection), so the offline-agent purge
// counts from the moment the host was lost rather than from its last handshake.
func (h *Hub) setAgentStatusIf(agentId, status string, cond func(*core.Record) bool) bool {
	var rec *core.Record
	var previous string
	err := h.RunInTransaction(func(txApp core.App) error {
		var err error
		rec, err = txApp.FindRecordById("agents", agentId)
		if err != nil {
			return err
		}
		previous = rec.GetString("status")
		if previous == status || (cond != nil && !cond(rec)) {
			rec = nil
			return nil
		}
		rec.Set("status", status)
		if status == "offline" {
			rec.Set("last_seen", time.Now())
		}
		return txApp.SaveNoValidate(rec)
	})
	if err != nil || rec == nil {
		return false
	}
	evt := notifications.Event{
		Kind:       notifications.KindForAgent(status),
		OccurredAt: time.Now(),
		Resource: notifications.ResourceRef{
			ID:   agentId,
			Name: rec.GetString("name"),
			Type: "agent",
		},
		Previous: previous,
		Current:  status,
	}
	h.emitNotification(evt)
	return true
}

// updateAgentInfo persists capabilities and metadata returned by GetAgentInfo, and
// (at first enrollment only) seeds the agent's tags from the TAGS env it declared.
func (h *Hub) updateAgentInfo(agentId string, info common.AgentInfoResponse, firstEnroll bool) {
	rec, err := h.FindRecordById("agents", agentId)
	if err != nil {
		return
	}
	if hostname, ok := info.Metadata["hostname"].(string); ok && hostname != "" {
		rec.Set("name", hostname)
	}
	rec.Set("capabilities", info.Capabilities)
	rec.Set("metadata", info.Metadata)
	// Seed agent-declared tags only at first enrollment, and only when the host has
	// none yet — so the UI stays the source of truth on reconnects and pre-set tags
	// (e.g. assigned in the UI before the agent first connected) are never clobbered.
	if firstEnroll && len(info.Tags) > 0 && len(rec.GetStringSlice("tags")) == 0 {
		rec.Set("tags", info.Tags)
	}
	_ = h.SaveNoValidate(rec)
}

// goAgent runs fn as a per-connection goroutine tracked by stopAgentConnections. It
// returns false, without running fn, once the hub is stopping.
func (h *Hub) goAgent(fn func()) bool {
	h.agentMu.Lock()
	defer h.agentMu.Unlock()
	if h.agentsStopped {
		return false
	}
	h.agentWG.Add(1)
	go func() {
		defer h.agentWG.Done()
		fn()
	}()
	return true
}

// stopAgentConnections refuses new agent connections, cancels the in-flight agent
// requests, closes the live connections and waits for their goroutines to return, so
// none of them writes to the database once it is closed. Safe to call more than once.
func (h *Hub) stopAgentConnections() {
	h.agentMu.Lock()
	h.agentsStopped = true
	h.agentMu.Unlock()
	h.stopAgents()
	h.agentConns.Range(func(_, v any) bool {
		if wsConn, ok := v.(*ws.WsConn); ok {
			wsConn.Close([]byte("hub shutting down"))
		}
		return true
	})
	h.agentWG.Wait()
}
