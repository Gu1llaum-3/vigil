//go:build testing

package hub

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The push token is enough to send heartbeats: clients that may only read must not get it.
func TestPushTokenHiddenFromReadOnlyUsers(t *testing.T) {
	hub, testApp, err := createTestHub(t)
	require.NoError(t, err)
	defer cleanupTestHub(hub, testApp)

	monitor, err := createTestRecord(hub, "monitors", map[string]any{
		"name": "cron", "type": "push", "push_token": "secret-push-token", "active": false,
	})
	require.NoError(t, err)
	editor, err := createTestRecord(hub, "users", map[string]any{"email": "ed@test.com", "password": "testtesttest", "role": "user"})
	require.NoError(t, err)
	reader, err := createTestRecord(hub, "users", map[string]any{"email": "ro@test.com", "password": "testtesttest", "role": "readonly"})
	require.NoError(t, err)
	editorAuth, err := editor.NewAuthToken()
	require.NoError(t, err)
	readerAuth, err := reader.NewAuthToken()
	require.NoError(t, err)

	for _, url := range []string{"/api/app/monitors", "/api/app/monitors/" + monitor.Id} {
		res := performNotificationRequest(t, hub, hub, http.MethodGet, url, editorAuth)
		require.Equal(t, http.StatusOK, res.Code)
		assert.Contains(t, res.Body.String(), "secret-push-token", "an editor needs the push URL (%s)", url)

		res = performNotificationRequest(t, hub, hub, http.MethodGet, url, readerAuth)
		require.Equal(t, http.StatusOK, res.Code)
		assert.NotContains(t, res.Body.String(), "secret-push-token", "a readonly user must not get it (%s)", url)
	}

	// The generic collection API (and so realtime events) must not carry it either.
	res := performNotificationRequest(t, hub, hub, http.MethodGet, "/api/collections/monitors/records?fields=*,push_token", readerAuth)
	require.Equal(t, http.StatusOK, res.Code, res.Body.String())
	assert.Contains(t, res.Body.String(), monitor.Id)
	assert.NotContains(t, res.Body.String(), "secret-push-token")
}
