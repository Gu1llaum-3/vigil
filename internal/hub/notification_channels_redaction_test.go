//go:build testing

package hub

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The channel API never returns a secret (the webhook URL of Slack/Teams/GChat, ntfy and
// Gotify tokens, webhook headers), and sending the redacted placeholder back keeps the
// stored value.
func TestNotificationChannelSecretsAreRedacted(t *testing.T) {
	env := newPurgeTestEnv(t)
	admin, err := createTestRecord(env.hub, "users", map[string]any{"email": "admin@example.com", "password": "password123", "role": "admin"})
	require.NoError(t, err)
	token, err := admin.NewAuthToken()
	require.NoError(t, err)
	call := func(method, url string, body ...string) (int, string) {
		res := performNotificationRequest(t, env.app, env.hub, method, url, token, body...)
		return res.Code, res.Body.String()
	}
	const secret = "https://hooks.slack.com/services/T000/B000/very-secret"

	code, body := call(http.MethodPost, "/api/app/notifications/channels", `{"name":"ops","kind":"slack","config":{"url":"`+secret+`","channel":"#ops"}}`)
	require.Equal(t, http.StatusCreated, code, body)
	assert.NotContains(t, body, "very-secret")
	var created struct {
		ID     string         `json:"id"`
		Config map[string]any `json:"config"`
	}
	require.NoError(t, json.Unmarshal([]byte(body), &created))
	assert.Equal(t, map[string]any{"url": "**REDACTED**", "channel": "#ops"}, created.Config)

	for _, kind := range []string{`{"name":"n","kind":"ntfy","config":{"url":"https://ntfy.sh/t","token":"ntfy-secret"}}`,
		`{"name":"g","kind":"gotify","config":{"url":"https://g","token":"gotify-secret"}}`,
		`{"name":"w","kind":"webhook","config":{"url":"https://w","headers":{"Authorization":"Bearer webhook-secret"}}}`} {
		code, body := call(http.MethodPost, "/api/app/notifications/channels", kind)
		require.Equal(t, http.StatusCreated, code, body)
		assert.NotContains(t, body, "-secret")
	}
	code, body = call(http.MethodGet, "/api/app/notifications/channels")
	require.Equal(t, http.StatusOK, code)
	assert.NotContains(t, body, "secret")

	stored := func() string {
		rec, err := env.hub.FindRecordById("notification_channels", created.ID)
		require.NoError(t, err)
		var config map[string]any
		require.NoError(t, rec.UnmarshalJSONField("config", &config))
		return config["url"].(string)
	}
	code, body = call(http.MethodPatch, "/api/app/notifications/channels/"+created.ID, `{"config":{"url":"**REDACTED**","channel":"#alerts"}}`)
	require.Equal(t, http.StatusOK, code, body)
	assert.Equal(t, secret, stored(), "the placeholder keeps the stored secret")
	assert.NotContains(t, body, "very-secret")

	code, body = call(http.MethodPatch, "/api/app/notifications/channels/"+created.ID, `{"config":{"url":"https://hooks.slack.com/services/new","channel":"#alerts"}}`)
	require.Equal(t, http.StatusOK, code, body)
	assert.Equal(t, "https://hooks.slack.com/services/new", stored())
	assert.NotContains(t, body, "services/new")
}

// A malformed filter would read as "no filter" and match every resource: it is refused.
func TestNotificationRuleFilterValidation(t *testing.T) {
	env := newPurgeTestEnv(t)
	admin, err := createTestRecord(env.hub, "users", map[string]any{"email": "admin@example.com", "password": "password123", "role": "admin"})
	require.NoError(t, err)
	token, err := admin.NewAuthToken()
	require.NoError(t, err)
	create := func(filter string) int {
		return performNotificationRequest(t, env.app, env.hub, http.MethodPost, "/api/app/notifications/rules", token,
			`{"name":"r","events":["monitor.down"],"filter":`+filter+`}`).Code
	}
	for _, bad := range []string{`{"agent_ids":"a1"}`, `{"agentids":["a1"]}`, `{"monitor_ids":[1]}`, `{"monitor_ids":[""]}`} {
		assert.Equal(t, http.StatusBadRequest, create(bad), bad)
	}
	for _, good := range []string{`null`, `{}`, `{"agent_ids":["a1"],"monitor_ids":[]}`} {
		assert.Equal(t, http.StatusCreated, create(good), good)
	}
}
