//go:build testing

package hub

import (
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/pocketbase/pocketbase/apis"
	"github.com/pocketbase/pocketbase/core"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func findRule(t *testing.T, s *core.Settings, label string) core.RateLimitRule {
	t.Helper()
	for _, r := range s.RateLimits.Rules {
		if r.Label == label && r.Audience == "" {
			return r
		}
	}
	t.Fatalf("rate limit rule %q not found in %+v", label, s.RateLimits.Rules)
	return core.RateLimitRule{}
}

func TestApplyRateLimitSettingsEnablesByDefault(t *testing.T) {
	hub, testApp, err := createTestHub(t)
	require.NoError(t, err)
	defer cleanupTestHub(hub, testApp)

	settings := testApp.Settings()
	settings.RateLimits.Enabled = false
	settings.RateLimits.Rules = nil

	applyRateLimitSettings(settings)

	assert.True(t, settings.RateLimits.Enabled)
	assert.Equal(t, core.RateLimitRule{Label: "*:auth", MaxRequests: 2, Duration: 3}, findRule(t, settings, "*:auth"))
	findRule(t, settings, "*:requestOTP")
	findRule(t, settings, "*:requestPasswordReset")
	findRule(t, settings, "/api/app/agent-connect")
	require.NoError(t, testApp.Save(settings), "the resulting settings must pass PocketBase validation")
}

func TestApplyRateLimitSettingsKeepsAdminRules(t *testing.T) {
	hub, testApp, err := createTestHub(t)
	require.NoError(t, err)
	defer cleanupTestHub(hub, testApp)

	settings := testApp.Settings()
	settings.RateLimits.Rules = []core.RateLimitRule{
		{Label: "*:auth", MaxRequests: 10, Duration: 60},
		{Label: "/api/", MaxRequests: 1000, Duration: 10},
	}

	applyRateLimitSettings(settings)

	assert.Equal(t, 10, findRule(t, settings, "*:auth").MaxRequests, "an existing rule must not be overwritten")
	assert.Equal(t, 1000, findRule(t, settings, "/api/").MaxRequests)
	findRule(t, settings, "/api/app/agent-connect")
	assert.Len(t, settings.RateLimits.Rules, 5)

	// idempotent across restarts
	applyRateLimitSettings(settings)
	assert.Len(t, settings.RateLimits.Rules, 5)
	require.NoError(t, testApp.Save(settings))
}

func TestApplyRateLimitSettingsKeepsAudienceSplitRules(t *testing.T) {
	hub, testApp, err := createTestHub(t)
	require.NoError(t, err)
	defer cleanupTestHub(hub, testApp)

	settings := testApp.Settings()
	settings.RateLimits.Rules = []core.RateLimitRule{
		{Label: "*:auth", Audience: core.RateLimitRuleAudienceGuest, MaxRequests: 2, Duration: 3},
		{Label: "*:auth", Audience: core.RateLimitRuleAudienceAuth, MaxRequests: 5, Duration: 3},
	}

	applyRateLimitSettings(settings)

	require.NoError(t, testApp.Save(settings), "adding an audience-less *:auth would conflict and stop the hub")
	assert.Len(t, settings.RateLimits.Rules, 5)
}

func TestApplyRateLimitSettingsCanBeDisabled(t *testing.T) {
	hub, testApp, err := createTestHub(t)
	require.NoError(t, err)
	defer cleanupTestHub(hub, testApp)

	t.Setenv("RATE_LIMITS", "false")
	settings := testApp.Settings()
	settings.RateLimits.Enabled = true

	applyRateLimitSettings(settings)

	assert.False(t, settings.RateLimits.Enabled)
}

func TestApplyTrustedProxySettings(t *testing.T) {
	hub, testApp, err := createTestHub(t)
	require.NoError(t, err)
	defer cleanupTestHub(hub, testApp)

	settings := testApp.Settings()
	settings.TrustedProxy.Headers = []string{"X-Admin-Set"}

	applyTrustedProxySettings(settings)
	assert.Equal(t, []string{"X-Admin-Set"}, settings.TrustedProxy.Headers, "unset env keeps the stored value")

	t.Setenv("TRUSTED_PROXY_HEADERS", " X-Real-IP , CF-Connecting-IP,")
	settings.TrustedProxy.UseLeftmostIP = true
	applyTrustedProxySettings(settings)
	assert.Equal(t, []string{"X-Real-IP", "CF-Connecting-IP"}, settings.TrustedProxy.Headers)
	assert.False(t, settings.TrustedProxy.UseLeftmostIP, "the rightmost (proxy-appended) address is the only safe choice")
}

// serveTestRequest routes one request through the hub middlewares and API routes.
func serveTestRequest(t *testing.T, hub *Hub, app core.App, req *http.Request) *httptest.ResponseRecorder {
	t.Helper()
	baseRouter, err := apis.NewRouter(app)
	require.NoError(t, err)
	serveEvent := &core.ServeEvent{App: app, Router: baseRouter}
	hub.registerMiddlewares(serveEvent)
	require.NoError(t, hub.registerApiRoutes(serveEvent))
	// observe the client IP PocketBase resolves after the hub middlewares ran
	serveEvent.Router.GET("/test/real-ip", func(e *core.RequestEvent) error {
		return e.String(http.StatusOK, e.RealIP())
	})
	mux, err := serveEvent.Router.BuildMux()
	require.NoError(t, err)
	res := httptest.NewRecorder()
	mux.ServeHTTP(res, req)
	return res
}

func TestPasswordAuthIsRateLimited(t *testing.T) {
	hub, testApp, err := createTestHub(t)
	require.NoError(t, err)
	defer cleanupTestHub(hub, testApp)
	require.True(t, testApp.Settings().RateLimits.Enabled, "the hub must enable rate limiting at initialization")

	statuses := make([]int, 0, 3)
	for range 3 {
		req := httptest.NewRequest(http.MethodPost, "/api/collections/users/auth-with-password",
			strings.NewReader(`{"identity":"nobody@example.com","password":"wrong-password"}`))
		req.Header.Set("Content-Type", "application/json")
		statuses = append(statuses, serveTestRequest(t, hub, testApp, req).Code)
	}
	assert.Equal(t, []int{http.StatusBadRequest, http.StatusBadRequest, http.StatusTooManyRequests}, statuses)
}

func TestTrustedProxyHeaderOnlyHonoredFromTrustedPeers(t *testing.T) {
	t.Setenv("TRUSTED_PROXY_IPS", "10.0.0.0/8")
	hub, testApp, err := createTestHub(t)
	require.NoError(t, err)
	defer cleanupTestHub(hub, testApp)
	testApp.Settings().TrustedProxy.Headers = []string{"X-Real-IP"}

	fromProxy := httptest.NewRequest(http.MethodGet, "/test/real-ip", nil)
	fromProxy.RemoteAddr = "10.1.2.3:4444"
	fromProxy.Header.Set("X-Real-IP", "203.0.113.7")
	assert.Equal(t, "203.0.113.7", serveTestRequest(t, hub, testApp, fromProxy).Body.String())

	direct := httptest.NewRequest(http.MethodGet, "/test/real-ip", nil)
	direct.RemoteAddr = "198.51.100.9:5555"
	direct.Header.Set("X-Real-IP", "203.0.113.7")
	assert.Equal(t, "198.51.100.9", serveTestRequest(t, hub, testApp, direct).Body.String(), "a client must not pick its own rate-limit key")
}

// A direct client rotating a forged client-IP header must still land in one bucket: the
// guard has to run before the rate limiter, not only before the handler.
func TestForgedClientIPDoesNotEscapeRateLimit(t *testing.T) {
	t.Setenv("TRUSTED_PROXY_IPS", "10.0.0.0/8")
	hub, testApp, err := createTestHub(t)
	require.NoError(t, err)
	defer cleanupTestHub(hub, testApp)
	testApp.Settings().TrustedProxy.Headers = []string{"X-Real-IP"}

	statuses := make([]int, 0, 3)
	for i := range 3 {
		req := httptest.NewRequest(http.MethodPost, "/api/collections/users/auth-with-password",
			strings.NewReader(`{"identity":"nobody@example.com","password":"wrong-password"}`))
		req.Header.Set("Content-Type", "application/json")
		req.RemoteAddr = "198.51.100.9:5555"
		req.Header.Set("X-Real-IP", "203.0.113."+strconv.Itoa(i+1))
		statuses = append(statuses, serveTestRequest(t, hub, testApp, req).Code)
	}
	assert.Equal(t, http.StatusTooManyRequests, statuses[2])
}

func TestAgentConnectIsRateLimited(t *testing.T) {
	hub, testApp, err := createTestHub(t)
	require.NoError(t, err)
	defer cleanupTestHub(hub, testApp)

	last := 0
	for range 61 {
		req := httptest.NewRequest(http.MethodGet, "/api/app/agent-connect", nil)
		last = serveTestRequest(t, hub, testApp, req).Code
	}
	assert.Equal(t, http.StatusTooManyRequests, last, "the 61st handshake within 10s must hit the agent-connect rule")
}

func TestTrustedProxyHeaderIgnoredWithoutAllowlist(t *testing.T) {
	hub, testApp, err := createTestHub(t)
	require.NoError(t, err)
	defer cleanupTestHub(hub, testApp)
	testApp.Settings().TrustedProxy.Headers = []string{"X-Real-IP"}

	req := httptest.NewRequest(http.MethodGet, "/test/real-ip", nil)
	req.RemoteAddr = "10.1.2.3:4444"
	req.Header.Set("X-Real-IP", "203.0.113.7")
	assert.Equal(t, "10.1.2.3", serveTestRequest(t, hub, testApp, req).Body.String())
}

func passwordAuthStatuses(t *testing.T, hub *Hub, app core.App, remoteAddr string, n int) []int {
	t.Helper()
	statuses := make([]int, 0, n)
	for range n {
		req := httptest.NewRequest(http.MethodPost, "/api/collections/users/auth-with-password",
			strings.NewReader(`{"identity":"nobody@example.com","password":"wrong-password"}`))
		req.Header.Set("Content-Type", "application/json")
		req.RemoteAddr = remoteAddr
		statuses = append(statuses, serveTestRequest(t, hub, app, req).Code)
	}
	return statuses
}

func TestRateLimitsDisabledByEnv(t *testing.T) {
	hub, testApp, err := createTestHub(t)
	require.NoError(t, err)
	defer cleanupTestHub(hub, testApp)

	t.Setenv("RATE_LIMITS", "false")
	applyRateLimitSettings(testApp.Settings())
	require.NoError(t, testApp.Save(testApp.Settings()))

	assert.NotContains(t, passwordAuthStatuses(t, hub, testApp, "198.51.100.9:5555", 4), http.StatusTooManyRequests)
}

func TestApplyRateLimitExcludedIPs(t *testing.T) {
	hub, testApp, err := createTestHub(t)
	require.NoError(t, err)
	defer cleanupTestHub(hub, testApp)

	settings := testApp.Settings()
	settings.RateLimits.ExcludedIPs = []string{"192.0.2.1"}
	applyRateLimitSettings(settings)
	assert.Equal(t, []string{"192.0.2.1"}, settings.RateLimits.ExcludedIPs, "unset env keeps the stored value")

	t.Setenv("RATE_LIMIT_EXCLUDED_IPS", " 203.0.113.10, 10.0.0.0/8 2001:db8::/32,not-an-ip,")
	applyRateLimitSettings(settings)
	assert.Equal(t, []string{"203.0.113.10", "10.0.0.0/8", "2001:db8::/32"}, settings.RateLimits.ExcludedIPs,
		"invalid entries are dropped so PocketBase validation cannot stop the hub")
	require.NoError(t, testApp.Save(settings))
}

func TestExcludedIPIsNotRateLimited(t *testing.T) {
	hub, testApp, err := createTestHub(t)
	require.NoError(t, err)
	defer cleanupTestHub(hub, testApp)

	t.Setenv("RATE_LIMIT_EXCLUDED_IPS", "203.0.113.0/24")
	applyRateLimitSettings(testApp.Settings())
	require.NoError(t, testApp.Save(testApp.Settings()))

	assert.NotContains(t, passwordAuthStatuses(t, hub, testApp, "203.0.113.10:4444", 4), http.StatusTooManyRequests,
		"the excluded office IP is never throttled")
	assert.Equal(t, http.StatusTooManyRequests, passwordAuthStatuses(t, hub, testApp, "198.51.100.9:5555", 3)[2],
		"everyone else still is")
}
