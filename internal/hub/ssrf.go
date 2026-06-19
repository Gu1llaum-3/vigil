package hub

import (
	"fmt"
	"net"
	"syscall"

	"github.com/Gu1llaum-3/vigil/internal/hub/utils"
)

// Monitor SSRF guard.
//
// Monitors are created by authenticated (non-readonly) users and cause the hub to make
// outbound connections to user-supplied targets. To stop the hub being used as an SSRF
// proxy against its own host or cloud metadata, the HTTP and TCP checks dial through a
// guarded net.Dialer that rejects dangerous resolved addresses.
//
// Policy (the hub is also a legitimate internal-infra monitor, so private LAN ranges are
// allowed by default):
//   - loopback, link-local (incl. 169.254.169.254 cloud metadata), and the unspecified
//     address are ALWAYS blocked;
//   - private/ULA ranges are blocked only when MONITOR_BLOCK_PRIVATE_TARGETS is set;
//   - MONITOR_ALLOW_PRIVATE_TARGETS disables the guard entirely (restores legacy behavior
//     for fully-trusted, single-tenant deployments).
//
// The check runs in Dialer.Control on the post-DNS-resolution address, so it also covers
// HTTP redirects and defeats DNS-rebinding (every dial is re-checked).

// metadataIPv6 is the well-known IPv6 cloud-metadata address (it is ULA, so it would only
// be caught by the private check; block it unconditionally like its IPv4 counterpart).
var metadataIPv6 = net.ParseIP("fd00:ec2::254")

func monitorIPBlocked(ip net.IP, blockPrivate, allowAll bool) bool {
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

func monitorGuardPolicy() (blockPrivate, allowAll bool) {
	if v, ok := utils.GetEnv("MONITOR_BLOCK_PRIVATE_TARGETS"); ok && (v == "true" || v == "1") {
		blockPrivate = true
	}
	if v, ok := utils.GetEnv("MONITOR_ALLOW_PRIVATE_TARGETS"); ok && (v == "true" || v == "1") {
		allowAll = true
	}
	return
}

// newGuardedDialer returns a dialer that refuses to connect to blocked addresses (see
// monitorIPBlocked), evaluated against the resolved IP at dial time.
func newGuardedDialer() *net.Dialer {
	blockPrivate, allowAll := monitorGuardPolicy()
	return &net.Dialer{
		Control: func(_, address string, _ syscall.RawConn) error {
			host, _, err := net.SplitHostPort(address)
			if err != nil {
				host = address
			}
			if ip := net.ParseIP(host); ip != nil && monitorIPBlocked(ip, blockPrivate, allowAll) {
				return fmt.Errorf("blocked monitor target %s (loopback/link-local/metadata/private address; SSRF guard)", host)
			}
			return nil
		},
	}
}
