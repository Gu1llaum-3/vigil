//go:build testing

package hub

import (
	"net"
	"testing"
)

// TestMonitorIPBlocked locks the SSRF policy: loopback/link-local/metadata/unspecified
// are always blocked; private ranges are opt-in; allowAll disables the guard.
func TestMonitorIPBlocked(t *testing.T) {
	cases := []struct {
		ip           string
		blockPrivate bool
		allowAll     bool
		want         bool
	}{
		{"169.254.169.254", false, false, true}, // AWS/GCP metadata (link-local)
		{"127.0.0.1", false, false, true},       // loopback → hub-local services
		{"::1", false, false, true},             // IPv6 loopback
		{"0.0.0.0", false, false, true},         // unspecified
		{"fe80::1", false, false, true},         // link-local v6
		{"fd00:ec2::254", false, false, true},   // IPv6 metadata (ULA) blocked unconditionally
		{"8.8.8.8", false, false, false},        // public → allowed
		{"10.0.0.5", false, false, false},       // private allowed by default (internal monitoring)
		{"10.0.0.5", true, false, true},         // private blocked when opted in
		{"192.168.1.1", true, false, true},      // RFC1918 blocked when opted in
		{"127.0.0.1", false, true, false},       // allowAll disables the guard
		{"169.254.169.254", true, true, false},  // allowAll wins over everything
	}
	for _, c := range cases {
		ip := net.ParseIP(c.ip)
		if ip == nil {
			t.Fatalf("bad test IP %q", c.ip)
		}
		if got := monitorIPBlocked(ip, c.blockPrivate, c.allowAll); got != c.want {
			t.Errorf("monitorIPBlocked(%s, blockPrivate=%v, allowAll=%v) = %v, want %v",
				c.ip, c.blockPrivate, c.allowAll, got, c.want)
		}
	}
}
