//go:build testing

package hub

import "testing"

func TestBearerToken(t *testing.T) {
	cases := map[string]string{
		"Bearer vk_abc": "vk_abc",
		"bearer vk_abc": "vk_abc", // scheme is case-insensitive
		"BEARER vk_abc": "vk_abc",
		"vk_abc":        "vk_abc", // no scheme
		"":              "",
	}
	for in, want := range cases {
		if got := bearerToken(in); got != want {
			t.Errorf("bearerToken(%q) = %q, want %q", in, got, want)
		}
	}
	// explicit: a trailing-space token is trimmed
	if got := bearerToken("Bearer vk_abc "); got != "vk_abc" {
		t.Errorf("trailing space not trimmed: %q", got)
	}
}

func TestIsMCPPath(t *testing.T) {
	cases := map[string]bool{
		"/api/mcp":       true,
		"/api/mcp/":      true,
		"/api/mcp/x":     true,
		"/api/mcp-admin": false, // boundary: sibling path must NOT match
		"/api/mcpx":      false,
		"/api/app/mcp":   false,
		"/api/app":       false,
	}
	for path, want := range cases {
		if got := isMCPPath(path); got != want {
			t.Errorf("isMCPPath(%q) = %v, want %v", path, got, want)
		}
	}
}

func TestApiKeyAuthAllowedPath(t *testing.T) {
	cases := map[string]bool{
		"/api/app":               true,
		"/api/app/info":          true,
		"/api/app/monitors":      true,
		"/api/mcp":               true,
		"/api/mcp/x":             true,
		"/api/collections/users": false, // generic PB API is NOT key-authenticated
		"/api/realtime":          false,
		"/api/app-evil":          false, // boundary
		"/":                      false,
	}
	for path, want := range cases {
		if got := apiKeyAuthAllowedPath(path); got != want {
			t.Errorf("apiKeyAuthAllowedPath(%q) = %v, want %v", path, got, want)
		}
	}
}

func TestIsReadMethod(t *testing.T) {
	for _, m := range []string{"GET", "HEAD"} {
		if !isReadMethod(m) {
			t.Errorf("isReadMethod(%q) should be true", m)
		}
	}
	for _, m := range []string{"POST", "PUT", "PATCH", "DELETE", "OPTIONS"} {
		if isReadMethod(m) {
			t.Errorf("isReadMethod(%q) should be false", m)
		}
	}
}
