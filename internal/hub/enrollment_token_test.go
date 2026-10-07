//go:build testing

package hub

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/apis"
	"github.com/pocketbase/pocketbase/core"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func enrollmentCall(t *testing.T, hub *Hub, method, authToken string, body ...string) (int, enrollmentState) {
	t.Helper()
	res := performNotificationRequest(t, hub, hub, method, "/api/app/agent-enrollment-token", authToken, body...)
	var state enrollmentState
	if res.Code == http.StatusOK {
		require.NoError(t, json.Unmarshal(res.Body.Bytes(), &state), res.Body.String())
	}
	return res.Code, state
}

func permanentTokenOf(t *testing.T, hub *Hub, userID string) string {
	t.Helper()
	rec, err := hub.FindFirstRecordByFilter("agent_enrollment_tokens", "created_by = {:u}", dbx.Params{"u": userID})
	if err != nil {
		return ""
	}
	return rec.GetString("token")
}

func TestEnrollmentTokenLifecycle(t *testing.T) {
	hub, testApp, err := createTestHub(t)
	require.NoError(t, err)
	defer cleanupTestHub(hub, testApp)
	user, err := createTestUser(hub)
	require.NoError(t, err)
	auth, err := user.NewAuthToken()
	require.NoError(t, err)

	// GET is read-only, whatever the query says, and nothing is enabled yet.
	res := performNotificationRequest(t, hub, hub, http.MethodGet,
		"/api/app/agent-enrollment-token?enable=1&permanent=1&token=chosen-by-client", auth)
	require.Equal(t, http.StatusOK, res.Code)
	assert.NotContains(t, res.Body.String(), "chosen-by-client")
	assert.Empty(t, permanentTokenOf(t, hub, user.Id), "a GET must not create a token")
	_, state := enrollmentCall(t, hub, http.MethodGet, auth)
	assert.False(t, state.Active)

	// Enabling mints the token server-side; a client-supplied value is ignored.
	code, enabled := enrollmentCall(t, hub, http.MethodPost, auth, `{"enable":true,"token":"a"}`)
	require.Equal(t, http.StatusOK, code)
	assert.True(t, enabled.Active)
	assert.False(t, enabled.Permanent)
	assert.NotEqual(t, "a", enabled.Token)
	assert.Len(t, enabled.Token, 40)
	_, state = enrollmentCall(t, hub, http.MethodGet, auth)
	assert.Equal(t, enabled, state)

	// Making it permanent keeps the value, so commands already copied keep working.
	_, permanent := enrollmentCall(t, hub, http.MethodPost, auth, `{"enable":true,"permanent":true}`)
	assert.Equal(t, enrollmentState{Token: enabled.Token, Active: true, Permanent: true}, permanent)
	assert.Equal(t, enabled.Token, permanentTokenOf(t, hub, user.Id))
	_, ok := enrollmentTokenMap.GetMap().GetOk(enabled.Token)
	assert.False(t, ok, "the ephemeral copy is dropped once the token is permanent")

	// Regenerating revokes the old value.
	_, regenerated := enrollmentCall(t, hub, http.MethodPost, auth, `{"enable":true,"permanent":true,"regenerate":true}`)
	assert.NotEqual(t, enabled.Token, regenerated.Token)
	assert.Equal(t, regenerated.Token, permanentTokenOf(t, hub, user.Id))

	// Back to ephemeral: the permanent record goes away, the value stays.
	_, ephemeral := enrollmentCall(t, hub, http.MethodPost, auth, `{"enable":true,"permanent":false}`)
	assert.Equal(t, enrollmentState{Token: regenerated.Token, Active: true}, ephemeral)
	assert.Empty(t, permanentTokenOf(t, hub, user.Id))

	// Disabling revokes everything.
	_, disabled := enrollmentCall(t, hub, http.MethodPost, auth, `{"enable":false}`)
	assert.False(t, disabled.Active)
	_, ok = enrollmentTokenMap.GetMap().GetOk(regenerated.Token)
	assert.False(t, ok)
	_, state = enrollmentCall(t, hub, http.MethodGet, auth)
	assert.False(t, state.Active)
}

func TestEnrollmentTokenPostForbiddenForReadonly(t *testing.T) {
	hub, testApp, err := createTestHub(t)
	require.NoError(t, err)
	defer cleanupTestHub(hub, testApp)
	readonly, err := createTestRecord(hub, "users", map[string]any{"email": "ro@test.com", "password": "testtesttest", "role": "readonly"})
	require.NoError(t, err)
	auth, err := readonly.NewAuthToken()
	require.NoError(t, err)

	code, _ := enrollmentCall(t, hub, http.MethodPost, auth, `{"enable":true}`)
	assert.Equal(t, http.StatusForbidden, code)
}

func TestEnrollmentTokenTransitions(t *testing.T) {
	hub, testApp, err := createTestHub(t)
	require.NoError(t, err)
	defer cleanupTestHub(hub, testApp)
	user, err := createTestUser(hub)
	require.NoError(t, err)
	auth, err := user.NewAuthToken()
	require.NoError(t, err)

	// straight to permanent from nothing
	_, permanent := enrollmentCall(t, hub, http.MethodPost, auth, `{"enable":true,"permanent":true}`)
	assert.True(t, permanent.Permanent)
	assert.Equal(t, permanent.Token, permanentTokenOf(t, hub, user.Id))

	// regenerate while ephemeral revokes every in-memory copy, including a stray duplicate
	_, ephemeral := enrollmentCall(t, hub, http.MethodPost, auth, `{"enable":true}`)
	enrollmentTokenMap.GetMap().Set("stray-duplicate", user.Id, time.Hour)
	_, regenerated := enrollmentCall(t, hub, http.MethodPost, auth, `{"enable":true,"regenerate":true}`)
	assert.NotEqual(t, ephemeral.Token, regenerated.Token)
	for _, old := range []string{ephemeral.Token, "stray-duplicate"} {
		_, ok := enrollmentTokenMap.GetMap().GetOk(old)
		assert.False(t, ok, "%s must be revoked", old)
	}
	_, ok := enrollmentTokenMap.GetMap().GetOk(regenerated.Token)
	assert.True(t, ok)
}

func TestEnrollmentTokenPostRequiresJSON(t *testing.T) {
	hub, testApp, err := createTestHub(t)
	require.NoError(t, err)
	defer cleanupTestHub(hub, testApp)
	user, err := createTestUser(hub)
	require.NoError(t, err)
	auth, err := user.NewAuthToken()
	require.NoError(t, err)

	baseRouter, err := apis.NewRouter(hub)
	require.NoError(t, err)
	serveEvent := &core.ServeEvent{App: hub, Router: baseRouter}
	hub.registerMiddlewares(serveEvent)
	require.NoError(t, hub.registerApiRoutes(serveEvent))
	mux, err := serveEvent.Router.BuildMux()
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodPost, "/api/app/agent-enrollment-token", strings.NewReader("enable=true&permanent=true"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Authorization", auth)
	res := httptest.NewRecorder()
	mux.ServeHTTP(res, req)

	assert.Equal(t, http.StatusBadRequest, res.Code)
	assert.Empty(t, permanentTokenOf(t, hub, user.Id))
}
