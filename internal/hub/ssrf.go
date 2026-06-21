package hub

import (
	"net"

	"github.com/Gu1llaum-3/vigil/internal/netguard"
)

// Monitor SSRF guard.
//
// Monitors are created by authenticated (non-readonly) users and cause the hub to make
// outbound connections to user-supplied targets. To stop the hub being used as an SSRF
// proxy against its own host or cloud metadata, the HTTP and TCP checks dial through a
// guarded net.Dialer that rejects dangerous resolved addresses. The shared policy and
// dialer live in internal/netguard (also used by the notification providers); these thin
// wrappers preserve the monitor-specific call sites and tests.

func monitorIPBlocked(ip net.IP, blockPrivate, allowAll bool) bool {
	return netguard.IPBlocked(ip, blockPrivate, allowAll)
}

func monitorGuardPolicy() (blockPrivate, allowAll bool) {
	return netguard.Policy()
}

// newGuardedDialer returns a dialer that refuses to connect to blocked addresses (see
// netguard.IPBlocked), evaluated against the resolved IP at dial time.
func newGuardedDialer() *net.Dialer {
	blockPrivate, _ := netguard.Policy()
	return netguard.NewGuardedDialer(blockPrivate)
}
