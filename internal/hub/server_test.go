//go:build testing

package hub

import (
	"strings"
	"testing"
)

func TestAddScriptNonce(t *testing.T) {
	html := `<script>theme()</script><script>globalThis.APP = {}</script><script type="module" src="/assets/x.js"></script>`
	out := addScriptNonce(html, "ABC123")

	if strings.Count(out, `<script nonce="ABC123">`) != 2 {
		t.Fatalf("expected both inline scripts to get the nonce, got: %s", out)
	}
	// the bundled module script must NOT be rewritten
	if !strings.Contains(out, `<script type="module" src="/assets/x.js"></script>`) {
		t.Fatalf("module script tag should be left untouched, got: %s", out)
	}
}

func TestDefaultContentSecurityPolicy(t *testing.T) {
	csp := defaultContentSecurityPolicy("N0NCE")
	for _, want := range []string{
		"default-src 'self'",
		"script-src 'self' 'nonce-N0NCE'",
		"object-src 'none'",
		"frame-ancestors 'self'",
		"base-uri 'self'",
	} {
		if !strings.Contains(csp, want) {
			t.Errorf("default CSP missing %q; got %q", want, csp)
		}
	}
	// the inline bootstrap scripts must NOT rely on 'unsafe-inline' for scripts
	if strings.Contains(csp, "script-src") && strings.Contains(csp, "'unsafe-inline'") {
		// unsafe-inline is only acceptable for style-src; make sure it's not in script-src
		scriptPart := csp[strings.Index(csp, "script-src"):]
		scriptPart = scriptPart[:strings.Index(scriptPart, ";")]
		if strings.Contains(scriptPart, "'unsafe-inline'") {
			t.Errorf("script-src must not allow 'unsafe-inline'; got %q", scriptPart)
		}
	}
}

func TestSecurityNonceUnique(t *testing.T) {
	a, err := securityNonce()
	if err != nil {
		t.Fatal(err)
	}
	b, err := securityNonce()
	if err != nil {
		t.Fatal(err)
	}
	if a == "" || a == b {
		t.Fatalf("nonces must be non-empty and unique, got %q and %q", a, b)
	}
}
