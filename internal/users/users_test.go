//go:build testing

package users

import "testing"

// TestEnforcedRole locks the privilege-escalation guard: only a trusted creator
// (superuser / admin) keeps the submitted role; everyone else is clamped to "user".
func TestEnforcedRole(t *testing.T) {
	cases := []struct {
		name      string
		trusted   bool
		submitted string
		want      string
	}{
		{"untrusted cannot self-assign admin", false, "admin", "user"},
		{"untrusted readonly is also clamped", false, "readonly", "user"},
		{"untrusted empty stays user", false, "", "user"},
		{"untrusted user stays user", false, "user", "user"},
		{"trusted may set admin", true, "admin", "admin"},
		{"trusted may set readonly", true, "readonly", "readonly"},
		{"trusted keeps submitted user", true, "user", "user"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := enforcedRole(tc.trusted, tc.submitted); got != tc.want {
				t.Fatalf("enforcedRole(%v, %q) = %q, want %q", tc.trusted, tc.submitted, got, tc.want)
			}
		})
	}
}
