package hub

import (
	"context"
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
	// authenticate with trusted header (AUTO_LOGIN)
	if autoLogin, _ := utils.GetEnv("AUTO_LOGIN"); autoLogin != "" {
		se.Router.BindFunc(func(e *core.RequestEvent) error {
			return authorizeRequestWithEmail(e, autoLogin)
		})
	}
	// authenticate with trusted header (TRUSTED_AUTH_HEADER)
	if trustedHeader, _ := utils.GetEnv("TRUSTED_AUTH_HEADER"); trustedHeader != "" {
		se.Router.BindFunc(func(e *core.RequestEvent) error {
			return authorizeRequestWithEmail(e, e.Request.Header.Get(trustedHeader))
		})
	}
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
	// get version and public key
	apiAuth.GET("/info", h.getInfo)
	// check for updates
	if optIn, _ := utils.GetEnv("CHECK_UPDATES"); optIn == "true" {
		var updateInfo UpdateInfo
		apiAuth.GET("/update", updateInfo.getUpdate)
	}
	// get or manage agent enrollment tokens
	apiAuth.GET("/agent-enrollment-token", h.getAgentEnrollmentToken).BindFunc(excludeReadOnlyRole)
	// handle agent websocket connection
	apiNoAuth.GET("/agent-connect", h.handleAgentConnect)
	// fleet patch audit dashboard
	apiAuth.GET("/dashboard", h.getDashboard)
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
	apiAuth.GET("/notifications/unread", h.getUnreadNotificationLogs).BindFunc(requireAdminRole)
	apiAuth.POST("/notifications/read-all", h.markAllNotificationLogsRead).BindFunc(requireAdminRole)
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
		if token == "" {
			token = uuid.New().String()
		}
		if permanent == "1" {
			tokenMap.RemovebyValue(userID)
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
