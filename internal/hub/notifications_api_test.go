//go:build testing

package hub

import (
	"net/http"
	"net/http/httptest"
	"strings"
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

func TestSystemNotificationsUnreadPreferencesAndMarkRead(t *testing.T) {
	hub, testApp, err := createTestHub(t)
	require.NoError(t, err)
	defer cleanupTestHub(hub, testApp)

	user, err := createTestRecord(hub, "users", map[string]any{
		"email":    "user@example.com",
		"password": "password123",
	})
	require.NoError(t, err)
	_, err = createTestRecord(hub, "user_settings", map[string]any{
		"user":     user.Id,
		"settings": map[string]any{},
	})
	require.NoError(t, err)
	userToken, err := user.NewAuthToken()
	require.NoError(t, err)

	_, err = createTestRecord(hub, systemNotificationsCollection, map[string]any{
		"event_kind":    "monitor.down",
		"category":      "monitors",
		"severity":      "critical",
		"resource_id":   "monitor-1",
		"resource_type": "monitor",
		"title":         "Monitor down",
		"message":       "Monitor api is down",
		"occurred_at":   time.Now().UTC(),
	})
	require.NoError(t, err)
	_, err = createTestRecord(hub, systemNotificationsCollection, map[string]any{
		"event_kind":    "container_image.update_available",
		"category":      "container_images",
		"severity":      "warning",
		"resource_id":   "audit-1",
		"resource_type": "container_image",
		"title":         "Docker image updates",
		"message":       "2 Docker image updates are available",
		"occurred_at":   time.Now().UTC(),
	})
	require.NoError(t, err)
	res := performNotificationRequest(t, testApp, hub, http.MethodGet, "/api/app/system-notifications/unread?limit=10", userToken)
	require.Equal(t, http.StatusOK, res.Code)
	require.Contains(t, res.Body.String(), `"count":2`)
	require.Contains(t, res.Body.String(), `"event_kind":"monitor.down"`)
	require.Contains(t, res.Body.String(), `"event_kind":"container_image.update_available"`)

	res = performNotificationRequest(t, testApp, hub, http.MethodPatch, "/api/app/system-notifications/preferences", userToken, `{"enabled_events":{"monitor.down":false}}`)
	require.Equal(t, http.StatusOK, res.Code)
	require.Contains(t, res.Body.String(), `"monitor.down":false`)
	require.Contains(t, res.Body.String(), `"enabled_events"`)

	res = performNotificationRequest(t, testApp, hub, http.MethodGet, "/api/app/system-notifications/unread?limit=10", userToken)
	require.Equal(t, http.StatusOK, res.Code)
	require.Contains(t, res.Body.String(), `"count":1`)
	require.NotContains(t, res.Body.String(), `"event_kind":"monitor.down"`)

	res = performNotificationRequest(t, testApp, hub, http.MethodPatch, "/api/app/system-notifications/preferences", userToken, `{"enabled_categories":{"container_images":false},"enabled_events":{"monitor.down":true}}`)
	require.Equal(t, http.StatusOK, res.Code)
	require.Contains(t, res.Body.String(), `"container_images":false`)

	res = performNotificationRequest(t, testApp, hub, http.MethodGet, "/api/app/system-notifications/unread?limit=10", userToken)
	require.Equal(t, http.StatusOK, res.Code)
	require.Contains(t, res.Body.String(), `"count":1`)
	require.NotContains(t, res.Body.String(), `"event_kind":"container_image.update_available"`)

	res = performNotificationRequest(t, testApp, hub, http.MethodPost, "/api/app/system-notifications/read-all", userToken)
	require.Equal(t, http.StatusOK, res.Code)
	require.Contains(t, res.Body.String(), `"ok":true`)

	settingsRec, err := hub.FindFirstRecordByFilter("user_settings", "user = {:user}", map[string]any{"user": user.Id})
	require.NoError(t, err)
	var settings map[string]any
	require.NoError(t, settingsRec.UnmarshalJSONField("settings", &settings))
	require.NotEmpty(t, settings["system_notifications_last_read_at_by_category"])

	res = performNotificationRequest(t, testApp, hub, http.MethodGet, "/api/app/system-notifications/unread?limit=10", userToken)
	require.Equal(t, http.StatusOK, res.Code)
	require.Contains(t, res.Body.String(), `"count":0`)

	notifications, err := hub.FindRecordsByFilter(systemNotificationsCollection, "", "-occurred_at", 0, 0)
	require.NoError(t, err)
	require.Len(t, notifications, 2)
}

func performNotificationRequest(t testing.TB, app core.App, hub *Hub, method, targetURL, authToken string, body ...string) *httptest.ResponseRecorder {
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

	var req *http.Request
	if len(body) > 0 {
		req = httptest.NewRequest(method, targetURL, strings.NewReader(body[0]))
		req.Header.Set("Content-Type", "application/json")
	} else {
		req = httptest.NewRequest(method, targetURL, nil)
	}
	req.Header.Set("Authorization", authToken)
	res := httptest.NewRecorder()
	mux.ServeHTTP(res, req)
	return res
}
