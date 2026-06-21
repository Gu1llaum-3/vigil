//go:build testing

package hub

import "testing"

func TestParseTrustedProxies(t *testing.T) {
	t.Run("bare IPs, CIDRs and IPv6, comma and space separated", func(t *testing.T) {
		nets, err := parseTrustedProxies("10.0.0.5, 192.168.1.0/24  172.16.0.0/12,::1,fd00::/8")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(nets) != 5 {
			t.Fatalf("got %d networks, want 5", len(nets))
		}
	})

	t.Run("empty input yields no networks and no error", func(t *testing.T) {
		nets, err := parseTrustedProxies("   ")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(nets) != 0 {
			t.Fatalf("got %d networks, want 0", len(nets))
		}
	})

	t.Run("a bad entry is skipped but valid ones are kept and error is reported", func(t *testing.T) {
		nets, err := parseTrustedProxies("10.0.0.1, not-an-ip, 192.168.0.0/16")
		if err == nil {
			t.Fatal("expected an error reporting the bad entry")
		}
		if len(nets) != 2 {
			t.Fatalf("got %d valid networks, want 2 (one typo must not disable the allowlist)", len(nets))
		}
	})
}

func TestRemoteIPAllowed(t *testing.T) {
	nets, err := parseTrustedProxies("10.0.0.5, 192.168.1.0/24, ::1")
	if err != nil {
		t.Fatalf("setup: %v", err)
	}

	cases := []struct {
		name       string
		remoteAddr string
		want       bool
	}{
		{"exact bare-IP match with port", "10.0.0.5:54321", true},
		{"exact bare-IP match without port", "10.0.0.5", true},
		{"inside CIDR", "192.168.1.200:1024", true},
		{"outside CIDR", "192.168.2.1:1024", false},
		{"unrelated IP", "8.8.8.8:443", false},
		{"IPv6 loopback in allowlist", "[::1]:1234", true},
		{"empty remote addr", "", false},
		{"garbage remote addr", "not-an-addr", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := remoteIPAllowed(nets, tc.remoteAddr); got != tc.want {
				t.Fatalf("remoteIPAllowed(%q) = %v, want %v", tc.remoteAddr, got, tc.want)
			}
		})
	}

	t.Run("empty allowlist denies all (fail-safe)", func(t *testing.T) {
		if remoteIPAllowed(nil, "10.0.0.5:1234") {
			t.Fatal("an empty allowlist must deny every peer")
		}
	})
}
