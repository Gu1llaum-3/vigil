//go:build testing

package heartbeat_test

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/Gu1llaum-3/vigil/internal/hub/heartbeat"
	appTests "github.com/Gu1llaum-3/vigil/internal/tests"
)

func TestNew(t *testing.T) {
	t.Run("returns nil when app is missing", func(t *testing.T) {
		hb := heartbeat.New(nil, envGetter(map[string]string{
			"HEARTBEAT_URL": "https://heartbeat.example.com/ping",
		}))
		assert.Nil(t, hb)
	})

	t.Run("returns nil when URL is missing", func(t *testing.T) {
		app := newTestHub(t)
		hb := heartbeat.New(app.App, func(string) (string, bool) {
			return "", false
		})
		assert.Nil(t, hb)
	})

	t.Run("parses and normalizes config values", func(t *testing.T) {
		app := newTestHub(t)
		env := map[string]string{
			"HEARTBEAT_URL":      "  https://heartbeat.example.com/ping  ",
			"HEARTBEAT_INTERVAL": "90",
			"HEARTBEAT_METHOD":   "head",
		}
		getEnv := func(key string) (string, bool) {
			v, ok := env[key]
			return v, ok
		}

		hb := heartbeat.New(app.App, getEnv)
		require.NotNil(t, hb)
		cfg := hb.GetConfig()
		assert.Equal(t, "https://heartbeat.example.com/ping", cfg.URL)
		assert.Equal(t, 90, cfg.Interval)
		assert.Equal(t, http.MethodHead, cfg.Method)
	})
}

func TestSendGETDoesNotRequireAppOrDB(t *testing.T) {
	app := newTestHub(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodGet, r.Method)
		assert.Equal(t, "App-Heartbeat", r.Header.Get("User-Agent"))
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	hb := heartbeat.New(app.App, envGetter(map[string]string{
		"HEARTBEAT_URL":    server.URL,
		"HEARTBEAT_METHOD": "GET",
	}))
	require.NotNil(t, hb)

	require.NoError(t, hb.Send())
}

func TestSendReturnsErrorOnHTTPFailureStatus(t *testing.T) {
	app := newTestHub(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	hb := heartbeat.New(app.App, envGetter(map[string]string{
		"HEARTBEAT_URL":    server.URL,
		"HEARTBEAT_METHOD": "GET",
	}))
	require.NotNil(t, hb)

	err := hb.Send()
	require.Error(t, err)
	assert.ErrorContains(t, err, "heartbeat endpoint returned status 500")
}

func TestSendPOSTBuildsPayload(t *testing.T) {
	app := newTestHub(t)

	type requestCapture struct {
		method      string
		userAgent   string
		contentType string
		payload     heartbeat.Payload
	}

	captured := make(chan requestCapture, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer r.Body.Close()
		body, err := io.ReadAll(r.Body)
		require.NoError(t, err)

		var payload heartbeat.Payload
		require.NoError(t, json.Unmarshal(body, &payload))
		captured <- requestCapture{
			method:      r.Method,
			userAgent:   r.Header.Get("User-Agent"),
			contentType: r.Header.Get("Content-Type"),
			payload:     payload,
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	hb := heartbeat.New(app.App, envGetter(map[string]string{
		"HEARTBEAT_URL":    server.URL,
		"HEARTBEAT_METHOD": "POST",
	}))
	require.NotNil(t, hb)
	require.NoError(t, hb.Send())

	req := <-captured
	assert.Equal(t, http.MethodPost, req.method)
	assert.Equal(t, "App-Heartbeat", req.userAgent)
	assert.Equal(t, "application/json", req.contentType)
	assert.Equal(t, "ok", req.payload.Status)
	assert.NotEmpty(t, req.payload.Timestamp)
	assert.NotEmpty(t, req.payload.Version)
}

func newTestHub(t *testing.T) *appTests.TestHub {
	t.Helper()
	app, err := appTests.NewTestHub(t.TempDir())
	require.NoError(t, err)
	t.Cleanup(app.Cleanup)
	return app
}

func envGetter(values map[string]string) func(string) (string, bool) {
	return func(key string) (string, bool) {
		v, ok := values[key]
		return v, ok
	}
}
