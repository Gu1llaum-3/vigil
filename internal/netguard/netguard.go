// Package netguard provides a shared SSRF guard for hub-originated outbound HTTP/TCP
// connections to user-supplied targets (uptime monitors and notification webhooks/chat
// providers). It refuses to connect to dangerous resolved addresses, evaluated at dial
// time so it also covers HTTP redirects and defeats DNS-rebinding (every dial re-checks).
//
// Policy:
//   - loopback, link-local (incl. 169.254.169.254 / fd00:ec2::254 cloud metadata), and the
//     unspecified address are ALWAYS blocked;
//   - private/ULA ranges are blocked only when blockPrivate is set;
//   - allowAll disables the guard entirely (trusted single-tenant deployments).
//
// The env knobs MONITOR_BLOCK_PRIVATE_TARGETS / MONITOR_ALLOW_PRIVATE_TARGETS feed Policy().
package netguard

import (
	"fmt"
	"net"
	"net/http"
	"syscall"
	"time"

	"github.com/Gu1llaum-3/vigil/internal/hub/utils"
)

// metadataIPv6 is the well-known IPv6 cloud-metadata address (ULA, so it would otherwise
// only be caught by the private check; block it unconditionally like its IPv4 counterpart).
var metadataIPv6 = net.ParseIP("fd00:ec2::254")

// IPBlocked reports whether ip is a forbidden outbound target under the given policy.
func IPBlocked(ip net.IP, blockPrivate, allowAll bool) bool {
	if allowAll || ip == nil {
		return false
	}
	if ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsUnspecified() {
		return true
	}
	if ip.Equal(metadataIPv6) {
		return true
	}
	if blockPrivate && ip.IsPrivate() {
		return true
	}
	return false
}

// Policy reads the env-configured outbound guard policy shared across subsystems.
func Policy() (blockPrivate, allowAll bool) {
	if v, ok := utils.GetEnv("MONITOR_BLOCK_PRIVATE_TARGETS"); ok && (v == "true" || v == "1") {
		blockPrivate = true
	}
	if v, ok := utils.GetEnv("MONITOR_ALLOW_PRIVATE_TARGETS"); ok && (v == "true" || v == "1") {
		allowAll = true
	}
	return
}

// NewGuardedDialer returns a net.Dialer that refuses to connect to blocked addresses
// (see IPBlocked), evaluated against the resolved IP at dial time. blockPrivate is the
// caller's per-subsystem choice; the global allowAll kill-switch (MONITOR_ALLOW_PRIVATE_TARGETS)
// is read live at dial time so it can be toggled without rebuilding long-lived clients.
func NewGuardedDialer(blockPrivate bool) *net.Dialer {
	return &net.Dialer{
		Control: func(_, address string, _ syscall.RawConn) error {
			host, _, err := net.SplitHostPort(address)
			if err != nil {
				host = address
			}
			_, allowAll := Policy()
			if ip := net.ParseIP(host); ip != nil && IPBlocked(ip, blockPrivate, allowAll) {
				return fmt.Errorf("blocked target %s (loopback/link-local/metadata/private address; SSRF guard)", host)
			}
			return nil
		},
	}
}

// NewGuardedClient returns an *http.Client whose transport refuses to connect to blocked
// addresses (SSRF guard). It clones the default transport so TLS/proxy defaults are kept.
func NewGuardedClient(timeout time.Duration, blockPrivate bool) *http.Client {
	tr := http.DefaultTransport.(*http.Transport).Clone()
	tr.DialContext = NewGuardedDialer(blockPrivate).DialContext
	return &http.Client{Timeout: timeout, Transport: tr}
}
