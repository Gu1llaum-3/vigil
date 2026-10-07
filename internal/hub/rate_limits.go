package hub

import (
	"log/slog"
	"net"
	"strconv"
	"strings"

	"github.com/Gu1llaum-3/vigil/internal/hub/utils"
	"github.com/pocketbase/pocketbase/apis"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/hook"
)

// vigilRateLimitRules are the rules the hub guarantees on every start. PocketBase's own
// defaults (*:auth, *:create, /api/batch, /api/) stay as they are; these only add what an
// internet-facing hub needs: password/OTP/OAuth2 guessing (*:auth covers every auth
// collection, superusers included), email-sending endpoints, and the unauthenticated agent
// handshake. A rule whose label already exists (with any audience) is left untouched, so
// limits tuned in the PocketBase dashboard survive restarts; a deleted one comes back.
var vigilRateLimitRules = []core.RateLimitRule{
	{Label: "*:auth", MaxRequests: 2, Duration: 3},
	{Label: "*:requestOTP", MaxRequests: 3, Duration: 60},
	{Label: "*:requestPasswordReset", MaxRequests: 3, Duration: 60},
	// Generous: every agent behind one NAT shares the bucket, and agents retry every few
	// seconds while the hub is unreachable.
	{Label: "/api/app/agent-connect", MaxRequests: 60, Duration: 10},
}

// applyRateLimitSettings turns PocketBase's rate limiter on (it ships disabled) unless
// RATE_LIMITS=false, and adds the missing Vigil rules.
func applyRateLimitSettings(settings *core.Settings) {
	settings.RateLimits.Enabled = true
	if raw, ok := utils.GetEnv("RATE_LIMITS"); ok && raw != "" {
		enabled, err := strconv.ParseBool(raw)
		if err != nil {
			slog.Warn("RATE_LIMITS is not a boolean; rate limiting stays enabled", "value", raw)
		} else {
			settings.RateLimits.Enabled = enabled
		}
	}
	if !settings.RateLimits.Enabled {
		slog.Warn("Rate limiting is disabled (RATE_LIMITS=false); auth endpoints accept unlimited attempts")
	}
	for _, rule := range vigilRateLimitRules {
		if !hasRateLimitRule(settings.RateLimits.Rules, rule) {
			settings.RateLimits.Rules = append(settings.RateLimits.Rules, rule)
		}
	}
}

// hasRateLimitRule matches on the label only: PocketBase rejects "*:auth" next to an
// admin's "*:auth"+"@guest" (keys are compared by prefix), and that would stop the hub.
func hasRateLimitRule(rules []core.RateLimitRule, rule core.RateLimitRule) bool {
	for _, r := range rules {
		if r.Label == rule.Label {
			return true
		}
	}
	return false
}

// applyTrustedProxySettings sets the client-IP headers PocketBase reads behind a reverse
// proxy from TRUSTED_PROXY_HEADERS (comma separated). Without it every client behind a
// proxy shares the proxy's address, and so one rate-limit bucket. When the variable is
// unset the stored setting is kept. The rightmost address is always used: it is the one
// the proxy appended, whereas the leftmost entries of X-Forwarded-For are client-supplied.
func applyTrustedProxySettings(settings *core.Settings) {
	raw, ok := utils.GetEnv("TRUSTED_PROXY_HEADERS")
	if !ok {
		return
	}
	headers := []string{}
	for h := range strings.SplitSeq(raw, ",") {
		if h = strings.TrimSpace(h); h != "" {
			headers = append(headers, h)
		}
	}
	settings.TrustedProxy.Headers = headers
	settings.TrustedProxy.UseLeftmostIP = false
}

// bindTrustedProxyHeaderGuard drops the configured client-IP headers from requests whose
// TCP peer is not in TRUSTED_PROXY_IPS, ahead of every PocketBase middleware (CORS is the
// first one), so neither the superuser IP whitelist nor the rate limiter reads them. PocketBase trusts those headers from any peer, which would let a
// client reaching the hub directly choose its own rate-limit key. Fail-safe like
// TRUSTED_AUTH_HEADER: with an empty allowlist the headers are never honored.
func bindTrustedProxyHeaderGuard(se *core.ServeEvent, allowed []*net.IPNet) {
	if len(allowed) == 0 && len(se.App.Settings().TrustedProxy.Headers) > 0 {
		slog.Warn("Trusted proxy headers are configured but TRUSTED_PROXY_IPS is empty; they will be IGNORED (fail-safe). Set TRUSTED_PROXY_IPS to your reverse proxy IP/CIDR.",
			"headers", se.App.Settings().TrustedProxy.Headers)
	}
	se.Router.Bind(&hook.Handler[*core.RequestEvent]{
		Id:       "vigilTrustedProxyHeaderGuard",
		Priority: apis.DefaultCorsMiddlewarePriority - 1,
		Func: func(e *core.RequestEvent) error {
			if !remoteIPAllowed(allowed, e.Request.RemoteAddr) {
				for _, h := range e.App.Settings().TrustedProxy.Headers {
					e.Request.Header.Del(h)
				}
			}
			return e.Next()
		},
	})
}
