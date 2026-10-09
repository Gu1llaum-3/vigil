//go:build testing

package providers

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// captured is one request a provider made.
type captured struct {
	method string
	path   string
	query  string
	header http.Header
	body   []byte
}

// endpoint serves status and records the requests. The SSRF guard refuses loopback; it reads
// MONITOR_ALLOW_PRIVATE_TARGETS at dial time.
func endpoint(t *testing.T, status int) (*httptest.Server, *[]captured) {
	t.Helper()
	t.Setenv("MONITOR_ALLOW_PRIVATE_TARGETS", "true")
	var reqs []captured
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		reqs = append(reqs, captured{r.Method, r.URL.Path, r.URL.RawQuery, r.Header.Clone(), body})
		w.WriteHeader(status)
	}))
	t.Cleanup(srv.Close)
	return srv, &reqs
}

var testMessage = Message{
	Title: "Monitor down", Body: "site is down\nsince 10:00", Severity: "critical",
	EventKind: "monitor.down", ResourceID: "m1", ResourceName: "site", ResourceType: "monitor",
	Previous: "up", Current: "down", Timestamp: time.Date(2026, 10, 9, 10, 0, 0, 0, time.UTC),
}

func send(t *testing.T, p Provider, config map[string]any) (string, error) {
	t.Helper()
	return p.Send(context.Background(), Channel{ID: "c1", Kind: p.Kind(), Config: config}, testMessage)
}

func jsonBody(t *testing.T, raw []byte) map[string]any {
	t.Helper()
	var out map[string]any
	require.NoError(t, json.Unmarshal(raw, &out), string(raw))
	return out
}

func TestWebhookProvider(t *testing.T) {
	srv, reqs := endpoint(t, http.StatusNoContent)
	preview, err := send(t, NewWebhookProvider(), map[string]any{
		"url": srv.URL + "/hook", "method": "put", "headers": map[string]any{"X-Api-Key": "k", "X-Ignored": 3.0},
	})
	require.NoError(t, err)
	require.Len(t, *reqs, 1)
	r := (*reqs)[0]
	assert.Equal(t, http.MethodPut, r.method)
	assert.Equal(t, "/hook", r.path)
	assert.Equal(t, "application/json", r.header.Get("Content-Type"))
	assert.Equal(t, "k", r.header.Get("X-Api-Key"))
	assert.Empty(t, r.header.Get("X-Ignored"), "non-string header values are dropped")
	assert.Equal(t, map[string]any{
		"title": "Monitor down", "body": "site is down\nsince 10:00", "severity": "critical",
		"event_kind": "monitor.down", "resource_id": "m1", "resource_name": "site", "resource_type": "monitor",
		"previous": "up", "current": "down", "timestamp": "2026-10-09T10:00:00Z",
	}, jsonBody(t, r.body))
	assert.Equal(t, "PUT → 204", preview, "the URL (a credential) stays out of the logs")

	_, err = send(t, NewWebhookProvider(), map[string]any{"url": srv.URL})
	require.NoError(t, err)
	assert.Equal(t, http.MethodPost, (*reqs)[1].method, "POST by default")
}

func TestSlackProvider(t *testing.T) {
	srv, reqs := endpoint(t, http.StatusOK)
	_, err := send(t, NewSlackProvider(), map[string]any{"url": srv.URL, "username": "vigil", "channel": "#ops", "icon_emoji": ":bell:"})
	require.NoError(t, err)
	r := (*reqs)[0]
	assert.Equal(t, http.MethodPost, r.method)
	assert.Equal(t, "application/json", r.header.Get("Content-Type"))
	body := jsonBody(t, r.body)
	assert.Equal(t, "Monitor down", body["text"])
	assert.Equal(t, "vigil", body["username"])
	assert.Equal(t, "#ops", body["channel"])
	assert.Equal(t, ":bell:", body["icon_emoji"])
	attachment := body["attachments"].([]any)[0].(map[string]any)
	assert.Equal(t, "#d73a49", attachment["color"], "critical")
	text := attachment["blocks"].([]any)[0].(map[string]any)["text"].(map[string]any)["text"]
	assert.Equal(t, "*Monitor down*\nsite is down\nsince 10:00", text)
}

func TestTeamsProvider(t *testing.T) {
	srv, reqs := endpoint(t, http.StatusOK)
	_, err := send(t, NewTeamsProvider(), map[string]any{"url": srv.URL})
	require.NoError(t, err)
	body := jsonBody(t, (*reqs)[0].body)
	assert.Equal(t, "MessageCard", body["@type"])
	assert.Equal(t, "Monitor down", body["summary"])
	assert.Equal(t, "d73a49", body["themeColor"])
	section := body["sections"].([]any)[0].(map[string]any)
	assert.Equal(t, "Monitor down", section["activityTitle"])
	assert.Equal(t, "site is down\nsince 10:00", section["activityText"])
}

func TestGChatProvider(t *testing.T) {
	srv, reqs := endpoint(t, http.StatusOK)
	_, err := send(t, NewGChatProvider(), map[string]any{"url": srv.URL})
	require.NoError(t, err)
	card := jsonBody(t, (*reqs)[0].body)["cardsV2"].([]any)[0].(map[string]any)["card"].(map[string]any)
	assert.Equal(t, map[string]any{"title": "Monitor down", "subtitle": "critical"}, card["header"])
	widget := card["sections"].([]any)[0].(map[string]any)["widgets"].([]any)[0].(map[string]any)
	assert.Equal(t, "site is down\nsince 10:00", widget["textParagraph"].(map[string]any)["text"])
}

func TestNtfyProvider(t *testing.T) {
	srv, reqs := endpoint(t, http.StatusOK)
	_, err := send(t, NewNtfyProvider(), map[string]any{"url": srv.URL + "/alerts", "token": "tk", "priority": 5.0})
	require.NoError(t, err)
	r := (*reqs)[0]
	assert.Equal(t, "/alerts", r.path)
	assert.Equal(t, "text/plain", r.header.Get("Content-Type"))
	assert.Equal(t, "Monitor down", r.header.Get("X-Title"))
	assert.Equal(t, "5", r.header.Get("X-Priority"))
	assert.Equal(t, "Bearer tk", r.header.Get("Authorization"))
	assert.Equal(t, "site is down\nsince 10:00", string(r.body))

	_, err = send(t, NewNtfyProvider(), map[string]any{"url": srv.URL})
	require.NoError(t, err)
	assert.Equal(t, "3", (*reqs)[1].header.Get("X-Priority"), "default priority")
	assert.Empty(t, (*reqs)[1].header.Get("Authorization"))
}

func TestGotifyProvider(t *testing.T) {
	srv, reqs := endpoint(t, http.StatusOK)
	preview, err := send(t, NewGotifyProvider(), map[string]any{"url": srv.URL + "/", "token": "app-token", "priority": 8.0})
	require.NoError(t, err)
	r := (*reqs)[0]
	assert.Equal(t, "/message", r.path)
	assert.Equal(t, "app-token", r.header.Get("X-Gotify-Key"))
	assert.Empty(t, r.query, "the token is not put in the URL")
	assert.Equal(t, map[string]any{"title": "Monitor down", "message": "site is down\nsince 10:00", "priority": 8.0}, jsonBody(t, r.body))
	assert.NotContains(t, preview, "app-token")
}

func TestInAppProvider(t *testing.T) {
	preview, err := send(t, NewInAppProvider(), nil)
	require.NoError(t, err)
	assert.Equal(t, "Monitor down\n\nsite is down\nsince 10:00", preview)
}

// Every HTTP provider fails on a non-2xx answer and on a missing url, and its errors never
// carry the configured URL or token: they end up in notification_logs.
func TestHTTPProviderFailures(t *testing.T) {
	type httpProvider struct {
		p      Provider
		config func(url string) map[string]any
	}
	all := map[string]httpProvider{
		"webhook": {NewWebhookProvider(), func(u string) map[string]any { return map[string]any{"url": u} }},
		"slack":   {NewSlackProvider(), func(u string) map[string]any { return map[string]any{"url": u} }},
		"teams":   {NewTeamsProvider(), func(u string) map[string]any { return map[string]any{"url": u} }},
		"gchat":   {NewGChatProvider(), func(u string) map[string]any { return map[string]any{"url": u} }},
		"ntfy":    {NewNtfyProvider(), func(u string) map[string]any { return map[string]any{"url": u, "token": "secret-token"} }},
		"gotify":  {NewGotifyProvider(), func(u string) map[string]any { return map[string]any{"url": u, "token": "secret-token"} }},
	}
	for name, hp := range all {
		t.Run(name, func(t *testing.T) {
			srv, _ := endpoint(t, http.StatusInternalServerError)
			_, err := send(t, hp.p, hp.config(srv.URL+"/secret-path"))
			require.Error(t, err)
			assert.Contains(t, err.Error(), "500")

			_, err = send(t, hp.p, map[string]any{})
			require.ErrorContains(t, err, `"url"`)

			// An unparsable URL: the error must not echo it.
			_, err = send(t, hp.p, hp.config("https://hooks.example.com/secret-path\x7f"))
			require.Error(t, err)
			assert.NotContains(t, err.Error(), "secret-path")

			// A closed server: the transport error must not echo the URL.
			closed, _ := endpoint(t, http.StatusOK)
			url := closed.URL + "/secret-path"
			closed.Close()
			_, err = send(t, hp.p, hp.config(url))
			require.Error(t, err)
			assert.NotContains(t, err.Error(), "secret-path")
			assert.NotContains(t, err.Error(), "secret-token")
		})
	}
}

// Providers do not follow redirects: Go would forward custom headers (a webhook's, Gotify's
// token) to the target, even on another host.
func TestProvidersDoNotFollowRedirects(t *testing.T) {
	target, hits := endpoint(t, http.StatusOK)
	redirect := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL+"/elsewhere", http.StatusTemporaryRedirect)
	}))
	t.Cleanup(redirect.Close)
	for _, c := range []struct {
		p      Provider
		config map[string]any
	}{
		{NewGotifyProvider(), map[string]any{"url": redirect.URL, "token": "secret-token"}},
		{NewWebhookProvider(), map[string]any{"url": redirect.URL, "headers": map[string]any{"Authorization": "secret"}}},
		{NewSlackProvider(), map[string]any{"url": redirect.URL}},
	} {
		_, err := send(t, c.p, c.config)
		require.ErrorContains(t, err, "307", c.p.Kind())
	}
	assert.Empty(t, *hits, "nothing reaches the redirect target")
}

func TestValidateConfig(t *testing.T) {
	assert.Error(t, NewGotifyProvider().ValidateConfig(map[string]any{"url": "https://g"}), "gotify needs a token")
	assert.NoError(t, NewGotifyProvider().ValidateConfig(map[string]any{"url": "https://g", "token": "t"}))
	for _, p := range []Provider{NewWebhookProvider(), NewSlackProvider(), NewTeamsProvider(), NewGChatProvider(), NewNtfyProvider()} {
		assert.Error(t, p.ValidateConfig(map[string]any{}), p.Kind())
		assert.NoError(t, p.ValidateConfig(map[string]any{"url": "https://x"}), p.Kind())
	}
	assert.Error(t, (&EmailProvider{}).ValidateConfig(map[string]any{"to": ""}))
	assert.NoError(t, NewInAppProvider().ValidateConfig(nil))
	assert.True(t, strings.HasPrefix(severityColor("unknown"), "#"))
}
