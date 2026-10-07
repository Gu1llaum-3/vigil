//go:build testing

package hub

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	appmeta "github.com/Gu1llaum-3/vigil"
	"github.com/pocketbase/pocketbase/core"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newMonitorRecord(t *testing.T, hub *Hub, fields map[string]any) *core.Record {
	t.Helper()
	rec, err := createTestRecord(hub, "monitors", fields)
	require.NoError(t, err)
	// read it back so JSON fields have the type PocketBase gives them after a load
	stored, err := hub.FindRecordById("monitors", rec.Id)
	require.NoError(t, err)
	return stored
}

func checkCtx(t *testing.T) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	t.Cleanup(cancel)
	return ctx
}

func TestCheckHTTP(t *testing.T) {
	t.Setenv("MONITOR_ALLOW_PRIVATE_TARGETS", "true")
	hub, testApp, err := createTestHub(t)
	require.NoError(t, err)
	defer cleanupTestHub(hub, testApp)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		code, _ := strconv.Atoi(r.URL.Query().Get("code"))
		if code == 0 {
			code = http.StatusOK
		}
		w.WriteHeader(code)
		_, _ = w.Write([]byte("service healthy"))
	}))
	defer srv.Close()

	cases := []struct {
		name       string
		fields     map[string]any
		wantStatus int
		wantMsg    string
	}{
		{"200 accepted by default", map[string]any{"url": srv.URL}, monitorStatusUp, "HTTP 200"},
		{"204 refused by default", map[string]any{"url": srv.URL + "?code=204"}, monitorStatusDown, "HTTP 204"},
		{"204 accepted when listed", map[string]any{"url": srv.URL + "?code=204", "http_accepted_codes": []int{200, 204}}, monitorStatusUp, "HTTP 204"},
		{"401 accepted when listed", map[string]any{"url": srv.URL + "?code=401", "http_accepted_codes": []int{401}}, monitorStatusUp, "HTTP 401"},
		{"200 refused when not listed", map[string]any{"url": srv.URL, "http_accepted_codes": []int{204}}, monitorStatusDown, "HTTP 200"},
		{"empty list keeps the default", map[string]any{"url": srv.URL, "http_accepted_codes": []int{}}, monitorStatusUp, "HTTP 200"},
		{"malformed value keeps the default", map[string]any{"url": srv.URL + "?code=204", "http_accepted_codes": "200-299"}, monitorStatusDown, "HTTP 204"},
		{"keyword found", map[string]any{"url": srv.URL, "keyword": "healthy"}, monitorStatusUp, "HTTP 200"},
		{"keyword missing", map[string]any{"url": srv.URL, "keyword": "degraded"}, monitorStatusDown, "Keyword 'degraded' not found"},
		{"inverted keyword found", map[string]any{"url": srv.URL, "keyword": "healthy", "keyword_invert": true}, monitorStatusDown, "Keyword 'healthy' found (inverted match)"},
		{"inverted keyword missing", map[string]any{"url": srv.URL, "keyword": "degraded", "keyword_invert": true}, monitorStatusUp, "HTTP 200"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fields := map[string]any{"name": tc.name, "type": "http"}
			for k, v := range tc.fields {
				fields[k] = v
			}
			status, msg := checkHTTP(checkCtx(t), newMonitorRecord(t, hub, fields))
			assert.Equal(t, tc.wantStatus, status)
			assert.Equal(t, tc.wantMsg, msg)
		})
	}
}

func TestCheckHTTPBlocksLoopbackByDefault(t *testing.T) {
	for _, prefix := range []string{"", appmeta.HubEnvPrefix, "APP_HUB_"} {
		t.Setenv(prefix+"MONITOR_ALLOW_PRIVATE_TARGETS", "")
	}
	hub, testApp, err := createTestHub(t)
	require.NoError(t, err)
	defer cleanupTestHub(hub, testApp)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer srv.Close()

	status, msg := checkHTTP(checkCtx(t), newMonitorRecord(t, hub, map[string]any{"name": "ssrf", "type": "http", "url": srv.URL}))
	assert.Equal(t, monitorStatusDown, status)
	assert.Contains(t, msg, "SSRF guard")
}

func TestCheckTCP(t *testing.T) {
	t.Setenv("MONITOR_ALLOW_PRIVATE_TARGETS", "true")
	hub, testApp, err := createTestHub(t)
	require.NoError(t, err)
	defer cleanupTestHub(hub, testApp)

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	port := ln.Addr().(*net.TCPAddr).Port
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			_ = c.Close()
		}
	}()

	status, msg := checkTCP(checkCtx(t), newMonitorRecord(t, hub, map[string]any{"name": "open", "type": "tcp", "hostname": "127.0.0.1", "port": port}))
	assert.Equal(t, monitorStatusUp, status, msg)

	require.NoError(t, ln.Close())
	status, msg = checkTCP(checkCtx(t), newMonitorRecord(t, hub, map[string]any{"name": "closed", "type": "tcp", "hostname": "127.0.0.1", "port": port}))
	assert.Equal(t, monitorStatusDown, status)
	assert.Contains(t, msg, "Connection failed")
}

func TestValidateAcceptedCodes(t *testing.T) {
	for name, tc := range map[string]struct {
		body  map[string]any
		valid bool
	}{
		"absent":       {map[string]any{}, true},
		"null":         {map[string]any{"http_accepted_codes": nil}, true},
		"empty":        {map[string]any{"http_accepted_codes": []any{}}, true},
		"codes":        {map[string]any{"http_accepted_codes": []any{200.0, 204.0, 401.0}}, true},
		"range string": {map[string]any{"http_accepted_codes": "200-299"}, false},
		"string code":  {map[string]any{"http_accepted_codes": []any{"204"}}, false},
		"out of range": {map[string]any{"http_accepted_codes": []any{99.0}}, false},
		"fractional":   {map[string]any{"http_accepted_codes": []any{200.5}}, false},
		"not a list":   {map[string]any{"http_accepted_codes": 200.0}, false},
	} {
		t.Run(name, func(t *testing.T) {
			err := validateAcceptedCodes(tc.body)
			if tc.valid {
				assert.NoError(t, err)
			} else {
				assert.Error(t, err)
			}
		})
	}
}
