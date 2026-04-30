//go:build testing

package hub

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/pocketbase/pocketbase/apis"
	"github.com/pocketbase/pocketbase/core"
	"github.com/stretchr/testify/require"
)

func TestNotificationRuleChannelsFieldSupportsMultipleValues(t *testing.T) {
	hub, testApp, err := createTestHub(t)
	require.NoError(t, err)
	defer cleanupTestHub(hub, testApp)

	collection, err := testApp.FindCollectionByNameOrId("notification_rules")
	require.NoError(t, err)

	field, ok := collection.Fields.GetByName("channels").(*core.RelationField)
	require.True(t, ok)
	require.Greater(t, field.MaxSelect, 1)

	channelOne, err := createTestRecord(testApp, "notification_channels", map[string]any{
		"name":    "channel-one",
		"kind":    "webhook",
		"enabled": true,
		"config":  map[string]any{"url": "https://example.com/one"},
	})
	require.NoError(t, err)

	channelTwo, err := createTestRecord(testApp, "notification_channels", map[string]any{
		"name":    "channel-two",
		"kind":    "webhook",
		"enabled": true,
		"config":  map[string]any{"url": "https://example.com/two"},
	})
	require.NoError(t, err)

	rule, err := createTestRecord(testApp, "notification_rules", map[string]any{
		"name":             "multi-channel-rule",
		"enabled":          true,
		"events":           []string{"monitor.down", "agent.offline"},
		"channels":         []string{channelOne.Id, channelTwo.Id},
		"min_severity":     "info",
		"throttle_seconds": 0,
	})
	require.NoError(t, err)

	stored, err := testApp.FindRecordById("notification_rules", rule.Id)
	require.NoError(t, err)
	require.ElementsMatch(t, []string{channelOne.Id, channelTwo.Id}, stored.GetStringSlice("channels"))
}

func TestNotificationUnreadAndMarkAllRead(t *testing.T) {
	hub, testApp, err := createTestHub(t)
	require.NoError(t, err)
	defer cleanupTestHub(hub, testApp)

	admin, err := createTestRecord(hub, "users", map[string]any{
		"email":    "admin@example.com",
		"password": "password123",
		"role":     "admin",
	})
	require.NoError(t, err)
	_, err = createTestRecord(hub, "user_settings", map[string]any{
		"user":     admin.Id,
		"settings": map[string]any{},
	})
	require.NoError(t, err)
	adminToken, err := admin.NewAuthToken()
	require.NoError(t, err)

	_, err = createTestRecord(hub, "notification_logs", map[string]any{
		"created_by":    admin.Id,
		"event_kind":    "monitor.down",
		"resource_id":   "monitor-1",
		"resource_type": "monitor",
		"status":        "sent",
		"sent_at":       time.Now().UTC(),
	})
	require.NoError(t, err)
	_, err = createTestRecord(hub, "notification_logs", map[string]any{
		"created_by":    admin.Id,
		"event_kind":    "container_image.update_available",
		"resource_id":   "container-1",
		"resource_type": "container_image",
		"status":        "failed",
		"error":         "boom",
		"sent_at":       time.Now().UTC(),
	})
	require.NoError(t, err)
	res := performNotificationRequest(t, testApp, hub, http.MethodGet, "/api/app/notifications/unread?limit=10", adminToken)
	require.Equal(t, http.StatusOK, res.Code)
	require.Contains(t, res.Body.String(), `"count":2`)
	require.Contains(t, res.Body.String(), `"event_kind":"monitor.down"`)
	require.Contains(t, res.Body.String(), `"event_kind":"container_image.update_available"`)

	res = performNotificationRequest(t, testApp, hub, http.MethodPost, "/api/app/notifications/read-all", adminToken)
	require.Equal(t, http.StatusOK, res.Code)
	require.Contains(t, res.Body.String(), `"ok":true`)
	require.Contains(t, res.Body.String(), `"updated":2`)

	settingsRec, err := hub.FindFirstRecordByFilter("user_settings", "user = {:user}", map[string]any{"user": admin.Id})
	require.NoError(t, err)
	var settings map[string]any
	require.NoError(t, settingsRec.UnmarshalJSONField("settings", &settings))
	require.NotEmpty(t, settings["notification_last_read_at"])

	res = performNotificationRequest(t, testApp, hub, http.MethodGet, "/api/app/notifications/unread?limit=10", adminToken)
	require.Equal(t, http.StatusOK, res.Code)
	require.Contains(t, res.Body.String(), `"count":0`)

	readLogs, err := hub.FindRecordsByFilter("notification_logs", "created_by = {:created_by}", "-sent_at", 0, 0, map[string]any{"created_by": admin.Id})
	require.NoError(t, err)
	require.Len(t, readLogs, 2)
}

func performNotificationRequest(t testing.TB, app core.App, hub *Hub, method, targetURL, authToken string) *httptest.ResponseRecorder {
	t.Helper()

	baseRouter, err := apis.NewRouter(app)
	require.NoError(t, err)

	serveEvent := new(core.ServeEvent)
	serveEvent.App = app
	serveEvent.Router = baseRouter
	hub.registerMiddlewares(serveEvent)
	require.NoError(t, hub.registerApiRoutes(serveEvent))

	mux, err := serveEvent.Router.BuildMux()
	require.NoError(t, err)

	req := httptest.NewRequest(method, targetURL, nil)
	req.Header.Set("Authorization", authToken)
	res := httptest.NewRecorder()
	mux.ServeHTTP(res, req)
	return res
}
