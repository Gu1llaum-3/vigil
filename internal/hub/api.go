package hub

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"time"

	app "github.com/Gu1llaum-3/vigil"
	"github.com/Gu1llaum-3/vigil/internal/ghupdate"
	"github.com/Gu1llaum-3/vigil/internal/hub/utils"
	"github.com/blang/semver"
	"github.com/google/uuid"
	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/apis"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/security"
)

// UpdateInfo holds information about the latest update check.
type UpdateInfo struct {
	lastCheck time.Time
	Version   string `json:"v"`
	Url       string `json:"url"`
}

// Middleware to allow only admin role users.
var requireAdminRole = customAuthMiddleware(func(e *core.RequestEvent) bool {
	return e.Auth.GetString("role") == "admin"
})

// Middleware to exclude readonly users.
var excludeReadOnlyRole = customAuthMiddleware(func(e *core.RequestEvent) bool {
	return e.Auth.GetString("role") != "readonly"
})

// customAuthMiddleware handles boilerplate for custom authentication middlewares.
func customAuthMiddleware(fn func(*core.RequestEvent) bool) func(*core.RequestEvent) error {
	return func(e *core.RequestEvent) error {
		if e.Auth == nil {
			return e.UnauthorizedError("The request requires valid record authorization token.", nil)
		}
		if !fn(e) {
			return e.ForbiddenError("The authorized record is not allowed to perform this action.", nil)
		}
		return e.Next()
	}
}

// registerMiddlewares registers custom middlewares.
func (h *Hub) registerMiddlewares(se *core.ServeEvent) {
	authorizeRequestWithEmail := func(e *core.RequestEvent, email string) (err error) {
		if e.Auth != nil || email == "" {
			return e.Next()
		}
		isAuthRefresh := e.Request.URL.Path == "/api/collections/users/auth-refresh" && e.Request.Method == http.MethodPost
		e.Auth, err = e.App.FindAuthRecordByEmail("users", email)
		if err != nil || !isAuthRefresh {
			return e.Next()
		}
		token, _ := e.Auth.NewAuthToken()
		e.Request.Header.Set("Authorization", token)
		return e.Next()
	}
	// authenticate with a Vigil API key (Authorization: Bearer vk_...) for non-browser
	// clients (scripts, the MCP server). Runs before the other auth middlewares; it is a
	// no-op for normal JWT requests.
	se.Router.BindFunc(h.authenticateApiKey)
	// authenticate with trusted header (AUTO_LOGIN)
	if autoLogin, _ := utils.GetEnv("AUTO_LOGIN"); autoLogin != "" {
		se.Router.BindFunc(func(e *core.RequestEvent) error {
			return authorizeRequestWithEmail(e, autoLogin)
		})
	}
	// authenticate with trusted header (TRUSTED_AUTH_HEADER)
	//
	// security: the header value is fully attacker-controlled, so it may only be honored
	// for requests that actually originate from a trusted reverse proxy. We gate it on the
	// real TCP peer (RemoteAddr — not X-Forwarded-For, which is itself spoofable) being in
	// the TRUSTED_PROXY_IPS allowlist. Fail-safe: if the allowlist is empty (or unset), the
	// header is ignored entirely, so a misconfiguration can never open an auth bypass.
	if trustedHeader, _ := utils.GetEnv("TRUSTED_AUTH_HEADER"); trustedHeader != "" {
		rawAllow, _ := utils.GetEnv("TRUSTED_PROXY_IPS")
		allowed, err := parseTrustedProxies(rawAllow)
		if err != nil {
			slog.Warn("TRUSTED_PROXY_IPS has invalid entries; they were ignored", "err", err)
		}
		if len(allowed) == 0 {
			slog.Warn("TRUSTED_AUTH_HEADER is set but TRUSTED_PROXY_IPS is empty; the trusted header will be IGNORED (fail-safe). Set TRUSTED_PROXY_IPS to your reverse proxy IP/CIDR to enable header auth.")
		}
		se.Router.BindFunc(func(e *core.RequestEvent) error {
			if !remoteIPAllowed(allowed, e.Request.RemoteAddr) {
				return e.Next()
			}
			return authorizeRequestWithEmail(e, e.Request.Header.Get(trustedHeader))
		})
	}
}

// parseTrustedProxies parses a comma/whitespace separated list of IPs and CIDRs into
// networks. A bare IP becomes a /32 (IPv4) or /128 (IPv6) host network. Invalid entries
// are skipped (and reported via the returned error) so a single typo does not silently
// disable the whole allowlist — the valid entries are still returned.
func parseTrustedProxies(raw string) ([]*net.IPNet, error) {
	var nets []*net.IPNet
	var bad []string
	for _, field := range strings.FieldsFunc(raw, func(r rune) bool { return r == ',' || r == ' ' || r == '\t' || r == '\n' }) {
		entry := strings.TrimSpace(field)
		if entry == "" {
			continue
		}
		if _, ipNet, err := net.ParseCIDR(entry); err == nil {
			nets = append(nets, ipNet)
			continue
		}
		if ip := net.ParseIP(entry); ip != nil {
			bits := 32
			if ip.To4() == nil {
				bits = 128
			}
			nets = append(nets, &net.IPNet{IP: ip, Mask: net.CIDRMask(bits, bits)})
			continue
		}
		bad = append(bad, entry)
	}
	if len(bad) > 0 {
		return nets, fmt.Errorf("invalid trusted proxy entries: %s", strings.Join(bad, ", "))
	}
	return nets, nil
}

// remoteIPAllowed reports whether the connecting peer (RemoteAddr, either "ip:port" or a
// bare "ip") falls within any of the allowed networks. An empty allowlist denies all.
func remoteIPAllowed(allowed []*net.IPNet, remoteAddr string) bool {
	if len(allowed) == 0 || remoteAddr == "" {
		return false
	}
	host := remoteAddr
	if h, _, err := net.SplitHostPort(remoteAddr); err == nil {
		host = h
	}
	ip := net.ParseIP(strings.TrimSpace(host))
	if ip == nil {
		return false
	}
	for _, n := range allowed {
		if n.Contains(ip) {
			return true
		}
	}
	return false
}

// registerApiRoutes registers custom API routes under /api/app/*.
func (h *Hub) registerApiRoutes(se *core.ServeEvent) error {
	apiAuth := se.Router.Group("/api/app")
	apiAuth.Bind(apis.RequireAuth())
	apiNoAuth := se.Router.Group("/api/app")

	// create first user endpoint (only available when no users exist)
	if totalUsers, _ := se.App.CountRecords("users"); totalUsers == 0 {
		apiNoAuth.POST("/create-user", h.um.CreateFirstUser)
	}
	// check if first time setup on login page
	apiNoAuth.GET("/first-run", func(e *core.RequestEvent) error {
		total, err := e.App.CountRecords("users")
		return e.JSON(http.StatusOK, map[string]bool{"firstRun": err == nil && total == 0})
	})
	// per-user API keys for programmatic / MCP access (managed only via a real user session,
	// never via an API key itself)
	apiAuth.GET("/api-keys", h.listApiKeys)
	apiAuth.POST("/api-keys", h.createApiKey)
	apiAuth.DELETE("/api-keys/{id}", h.deleteApiKey)

	// MCP endpoint (Streamable HTTP) at /api/mcp — a top-level public integration surface,
	// authenticated by an API key (the global authenticateApiKey middleware + RequireAuth).
	// Delegates to the MCP SDK's http.Handler. MCP uses GET (SSE), POST (messages) and
	// DELETE (session end) on the same path.
	mcpHandler := h.mcpHandler()
	mcpRoute := func(e *core.RequestEvent) error {
		// Carry the API-key scope so mcpHandler serves the matching (read-only vs read-write)
		// tool set — the per-tool scope gate.
		scope, _ := e.Get(apiKeyScopeContextKey).(string)
		mcpHandler.ServeHTTP(e.Response, mcpRequestWithScope(e.Request, scope))
		return nil
	}
	apiMcp := se.Router.Group("/api")
	apiMcp.Bind(apis.RequireAuth())
	apiMcp.GET("/mcp", mcpRoute)
	apiMcp.POST("/mcp", mcpRoute)
	apiMcp.DELETE("/mcp", mcpRoute)
	// get version and public key
	apiAuth.GET("/info", h.getInfo)
	// check for updates
	if optIn, _ := utils.GetEnv("CHECK_UPDATES"); optIn == "true" {
		var updateInfo UpdateInfo
		apiAuth.GET("/update", updateInfo.getUpdate)
	}
	// get or manage agent enrollment tokens
	apiAuth.GET("/agent-enrollment-token", h.getAgentEnrollmentToken).BindFunc(excludeReadOnlyRole)
	// per-agent tokens for the admin/operator agents UI (the token field is hidden on the
	// collection so it is not exposed fleet-wide); readonly users are excluded.
	apiAuth.GET("/agent-tokens", h.getAgentTokens).BindFunc(excludeReadOnlyRole)
	// rotate a per-agent token server-side (cryptographically strong, replaces the old
	// client-generated token).
	apiAuth.POST("/agents/{id}/rotate-token", h.rotateAgentToken).BindFunc(excludeReadOnlyRole)
	// handle agent websocket connection
	apiNoAuth.GET("/agent-connect", h.handleAgentConnect)
	// fleet patch audit dashboard
	apiAuth.GET("/dashboard", h.getDashboard)
	// lightweight host monitoring overview and detail
	apiAuth.GET("/hosts-overview", h.getHostsOverview)
	apiAuth.GET("/hosts/{id}", h.getHostDetail)
	apiAuth.GET("/hosts/{id}/metrics", h.getHostMetricsHistory)
	apiAuth.GET("/fleet-metrics", h.getFleetMetrics)
	apiAuth.GET("/hosts/{id}/container-metrics", h.getHostContainerMetricsHistory)
	apiAuth.GET("/hosts/{id}/container-metrics/latest", h.getHostContainerMetricsLatest)
	apiAuth.GET("/hosts/{id}/container-metrics/by-name/{name}", h.getContainerMetricsHistoryByName)
	apiAuth.GET("/hosts/{id}/container-metrics/by-name/{name}/latest", h.getContainerMetricsLatestByName)
	// trigger immediate snapshot refresh for all connected agents
	apiAuth.POST("/refresh-snapshots", h.refreshSnapshots).BindFunc(excludeReadOnlyRole)

	// monitors
	apiAuth.GET("/monitors", h.getMonitors)
	apiAuth.GET("/monitors/{id}", h.getMonitor)
	apiAuth.POST("/monitors", h.createMonitor).BindFunc(excludeReadOnlyRole)
	apiAuth.PUT("/monitors/{id}", h.updateMonitor).BindFunc(excludeReadOnlyRole)
	apiAuth.POST("/monitors/{id}/move", h.moveMonitor).BindFunc(excludeReadOnlyRole)
	apiAuth.DELETE("/monitors/{id}", h.deleteMonitor).BindFunc(excludeReadOnlyRole)
	apiAuth.GET("/monitors/{id}/events", h.getMonitorEvents)
	apiAuth.GET("/monitors/{id}/series", h.getMonitorSeries)
	// monitor groups
	apiAuth.GET("/monitor-groups", h.getMonitorGroups)
	apiAuth.POST("/monitor-groups", h.createMonitorGroup).BindFunc(excludeReadOnlyRole)
	apiAuth.PUT("/monitor-groups/{id}", h.updateMonitorGroup).BindFunc(excludeReadOnlyRole)
	apiAuth.DELETE("/monitor-groups/{id}", h.deleteMonitorGroup).BindFunc(excludeReadOnlyRole)
	// push heartbeat endpoint (unauthenticated)
	apiNoAuth.GET("/push/{pushToken}", h.pushHeartbeat)
	apiNoAuth.POST("/push/{pushToken}", h.pushHeartbeat)

	// notification channels (admin only)
	apiAuth.GET("/notifications/channels", h.getNotificationChannels).BindFunc(requireAdminRole)
	apiAuth.POST("/notifications/channels", h.createNotificationChannel).BindFunc(requireAdminRole)
	apiAuth.PATCH("/notifications/channels/{id}", h.updateNotificationChannel).BindFunc(requireAdminRole)
	apiAuth.DELETE("/notifications/channels/{id}", h.deleteNotificationChannel).BindFunc(requireAdminRole)
	apiAuth.POST("/notifications/channels/{id}/test", h.testNotificationChannel).BindFunc(requireAdminRole)
	// notification rules (admin only)
	apiAuth.GET("/notifications/rules", h.getNotificationRules).BindFunc(requireAdminRole)
	apiAuth.POST("/notifications/rules", h.createNotificationRule).BindFunc(requireAdminRole)
	apiAuth.PATCH("/notifications/rules/{id}", h.updateNotificationRule).BindFunc(requireAdminRole)
	apiAuth.DELETE("/notifications/rules/{id}", h.deleteNotificationRule).BindFunc(requireAdminRole)
	// notification logs (admin only)
	apiAuth.GET("/notifications/logs", h.getNotificationLogs).BindFunc(requireAdminRole)
	// metric alert thresholds (admin only)
	apiAuth.GET("/metric-alerts", h.listMetricAlerts).BindFunc(requireAdminRole)
	apiAuth.PUT("/metric-alerts", h.upsertMetricAlert).BindFunc(requireAdminRole)
	apiAuth.DELETE("/metric-alerts/{id}", h.deleteMetricAlert).BindFunc(requireAdminRole)
	// system notifications (authenticated users)
	apiAuth.GET("/system-notifications", h.getSystemNotifications)
	apiAuth.GET("/system-notifications/unread", h.getUnreadSystemNotifications)
	apiAuth.POST("/system-notifications/read-all", h.markSystemNotificationsRead)
	apiAuth.GET("/system-notifications/preferences", h.getSystemNotificationPreferences)
	apiAuth.PATCH("/system-notifications/preferences", h.updateSystemNotificationPreferences)
	// scheduled jobs (admin only)
	apiAuth.GET("/jobs", h.getScheduledJobs).BindFunc(requireAdminRole)
	apiAuth.PATCH("/jobs/{key}", h.updateScheduledJob).BindFunc(requireAdminRole)
	apiAuth.POST("/jobs/{key}/run", h.runScheduledJobNow).BindFunc(requireAdminRole)
	// registry credentials (admin only)
	apiAuth.GET("/registry-credentials", h.listRegistryCredentials).BindFunc(requireAdminRole)
	apiAuth.POST("/registry-credentials", h.createRegistryCredential).BindFunc(requireAdminRole)
	apiAuth.PATCH("/registry-credentials/{id}", h.updateRegistryCredential).BindFunc(requireAdminRole)
	apiAuth.DELETE("/registry-credentials/{id}", h.deleteRegistryCredential).BindFunc(requireAdminRole)
	// container audit overrides (admin only)
	apiAuth.GET("/container-audit-overrides", h.listContainerAuditOverrides).BindFunc(requireAdminRole)
	apiAuth.PUT("/container-audit-overrides", h.upsertContainerAuditOverride).BindFunc(requireAdminRole)
	apiAuth.DELETE("/container-audit-overrides/{id}", h.deleteContainerAuditOverride).BindFunc(requireAdminRole)
	// maintenance windows (admin CRUD; active list readable by all authenticated users)
	apiAuth.GET("/maintenance-windows", h.listMaintenanceWindows).BindFunc(requireAdminRole)
	apiAuth.POST("/maintenance-windows", h.createMaintenanceWindow).BindFunc(requireAdminRole)
	apiAuth.PUT("/maintenance-windows/{id}", h.updateMaintenanceWindow).BindFunc(requireAdminRole)
	apiAuth.DELETE("/maintenance-windows/{id}", h.deleteMaintenanceWindow).BindFunc(requireAdminRole)
	apiAuth.GET("/maintenance/active", h.getActiveMaintenance)
	// purge settings and execution (admin only)
	apiAuth.GET("/purge/settings", h.getPurgeSettings).BindFunc(requireAdminRole)
	apiAuth.PATCH("/purge/settings", h.updatePurgeSettings).BindFunc(requireAdminRole)
	apiAuth.POST("/purge/run", h.runPurge).BindFunc(requireAdminRole)

	return nil
}

// getInfo returns data needed by authenticated users (version, public key).
func (h *Hub) getInfo(e *core.RequestEvent) error {
	type infoResponse struct {
		Key         string `json:"key"`
		Version     string `json:"v"`
		CheckUpdate bool   `json:"cu"`
	}
	info := infoResponse{
		Key:     h.pubKey,
		Version: app.Version,
	}
	if optIn, _ := utils.GetEnv("CHECK_UPDATES"); optIn == "true" {
		info.CheckUpdate = true
	}
	return e.JSON(http.StatusOK, info)
}

// getUpdate checks for the latest release on GitHub and returns update info if a newer version is available.
func (info *UpdateInfo) getUpdate(e *core.RequestEvent) error {
	if time.Since(info.lastCheck) < 6*time.Hour {
		return e.JSON(http.StatusOK, info)
	}
	info.lastCheck = time.Now()
	latestRelease, err := ghupdate.FetchLatestRelease(context.Background(), http.DefaultClient, "")
	if err != nil {
		return err
	}
	currentVersion, err := semver.Parse(strings.TrimPrefix(app.Version, "v"))
	if err != nil {
		return err
	}
	latestVersion, err := semver.Parse(strings.TrimPrefix(latestRelease.Tag, "v"))
	if err != nil {
		return err
	}
	if latestVersion.GT(currentVersion) {
		info.Version = strings.TrimPrefix(latestRelease.Tag, "v")
		info.Url = latestRelease.Url
	}
	return e.JSON(http.StatusOK, info)
}

// getAgentEnrollmentToken handles enrollment token management (create, read, delete).
// getAgentTokens returns a map of agent id → token for the agents UI. The token field is
// hidden on the agents collection (so it is not exposed via the generic collection API to
// every authenticated user); this endpoint re-exposes it only to non-readonly users.
func (h *Hub) getAgentTokens(e *core.RequestEvent) error {
	records, err := h.FindAllRecords("agents")
	if err != nil {
		return err
	}
	tokens := make(map[string]string, len(records))
	for _, rec := range records {
		tokens[rec.Id] = rec.GetString("token")
	}
	return e.JSON(http.StatusOK, tokens)
}

// rotateAgentToken generates a new cryptographically strong token for an agent and saves
// it server-side, returning the new value. Replaces the previous client-generated token.
func (h *Hub) rotateAgentToken(e *core.RequestEvent) error {
	id := e.Request.PathValue("id")
	rec, err := h.FindRecordById("agents", id)
	if err != nil {
		return e.NotFoundError("Agent not found", err)
	}
	token := security.RandomString(40)
	rec.Set("token", token)
	if err := h.SaveNoValidate(rec); err != nil {
		return err
	}
	return e.JSON(http.StatusOK, map[string]string{"token": token})
}

func (h *Hub) getAgentEnrollmentToken(e *core.RequestEvent) error {
	if e.Auth.IsSuperuser() {
		return e.ForbiddenError("Superusers cannot use enrollment tokens", nil)
	}

	tokenMap := enrollmentTokenMap.GetMap()
	userID := e.Auth.Id
	query := e.Request.URL.Query()
	token := query.Get("token")
	enable := query.Get("enable")
	permanent := query.Get("permanent")

	deletePermanent := func() error {
		rec, err := h.FindFirstRecordByFilter("agent_enrollment_tokens", "created_by = {:user}", dbx.Params{"user": userID})
		if err != nil {
			return nil
		}
		return h.Delete(rec)
	}

	upsertPermanent := func(token string) error {
		rec, err := h.FindFirstRecordByFilter("agent_enrollment_tokens", "created_by = {:user}", dbx.Params{"user": userID})
		if err == nil {
			rec.Set("token", token)
			return h.Save(rec)
		}
		col, err := h.FindCachedCollectionByNameOrId("agent_enrollment_tokens")
		if err != nil {
			return err
		}
		newRec := core.NewRecord(col)
		newRec.Set("created_by", userID)
		newRec.Set("token", token)
		return h.Save(newRec)
	}

	if enable == "0" {
		tokenMap.RemovebyValue(userID)
		_ = deletePermanent()
		return e.JSON(http.StatusOK, map[string]any{"token": token, "active": false, "permanent": false})
	}

	if enable == "1" {
		// Always invalidate any prior in-memory token for this user first, so re-issuing
		// (e.g. the "Regenerate" action with an empty token) revokes a leaked value rather
		// than leaving the old ephemeral token valid until it expires.
		tokenMap.RemovebyValue(userID)
		if token == "" {
			token = uuid.New().String()
		}
		if permanent == "1" {
			if err := upsertPermanent(token); err != nil {
				return err
			}
			return e.JSON(http.StatusOK, map[string]any{"token": token, "active": true, "permanent": true})
		}
		_ = deletePermanent()
		tokenMap.Set(token, userID, time.Hour)
		return e.JSON(http.StatusOK, map[string]any{"token": token, "active": true, "permanent": false})
	}

	if rec, err := h.FindFirstRecordByFilter("agent_enrollment_tokens", "created_by = {:user}", dbx.Params{"user": userID}); err == nil {
		dbToken := rec.GetString("token")
		if token == "" || token == dbToken {
			return e.JSON(http.StatusOK, map[string]any{"token": dbToken, "active": true, "permanent": true})
		}
		return e.JSON(http.StatusOK, map[string]any{"token": token, "active": false, "permanent": false})
	}

	if token == "" {
		if token, _, ok := tokenMap.GetByValue(userID); ok {
			return e.JSON(http.StatusOK, map[string]any{"token": token, "active": true, "permanent": false})
		}
		token = uuid.New().String()
	}

	activeUser, ok := tokenMap.GetOk(token)
	active := ok && activeUser == userID
	return e.JSON(http.StatusOK, map[string]any{"token": token, "active": active, "permanent": false})
}
