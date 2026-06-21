//go:build testing

package netguard

import (
	"net"
	"net/http"
	"strings"
	"testing"
	"time"
)

// TestIPBlocked locks the shared SSRF policy: loopback/link-local/metadata/unspecified are
// always blocked; private ranges are opt-in; allowAll disables the guard.
func TestIPBlocked(t *testing.T) {
	cases := []struct {
		ip           string
		blockPrivate bool
		allowAll     bool
		want         bool
	}{
		{"169.254.169.254", false, false, true}, // cloud metadata (link-local)
		{"127.0.0.1", false, false, true},       // loopback
		{"::1", false, false, true},              // IPv6 loopback
		{"0.0.0.0", false, false, true},          // unspecified
		{"fe80::1", false, false, true},          // link-local v6
		{"fd00:ec2::254", false, false, true},    // IPv6 metadata
		{"8.8.8.8", false, false, false},         // public
		{"10.0.0.5", false, false, false},        // private allowed by default
		{"10.0.0.5", true, false, true},          // private blocked when opted in
		{"127.0.0.1", false, true, false},        // allowAll disables the guard
	}
	for _, tc := range cases {
		ip := net.ParseIP(tc.ip)
		if got := IPBlocked(ip, tc.blockPrivate, tc.allowAll); got != tc.want {
			t.Errorf("IPBlocked(%s, bp=%v, all=%v) = %v, want %v", tc.ip, tc.blockPrivate, tc.allowAll, got, tc.want)
		}
	}
}

// TestGuardedClientRefusesLoopback verifies the guard fires on the actual dial path: a
// request to a loopback address is refused with the SSRF-guard error before connecting.
func TestGuardedClientRefusesLoopback(t *testing.T) {
	client := NewGuardedClient(2*time.Second, false)
	req, err := http.NewRequest(http.MethodGet, "http://127.0.0.1:9/whatever", nil)
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.Do(req)
	if err == nil {
		t.Fatal("expected the guarded client to refuse a loopback target, got nil error")
	}
	if !strings.Contains(err.Error(), "SSRF guard") {
		t.Fatalf("expected an SSRF-guard error, got: %v", err)
	}
}
