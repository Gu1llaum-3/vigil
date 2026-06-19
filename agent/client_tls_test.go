//go:build testing

package agent

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func tlsEnvFunc(m map[string]string) func(string) (string, bool) {
	return func(k string) (string, bool) {
		v, ok := m[k]
		return v, ok
	}
}

// TestBuildHubTLSConfig locks the #5 fix: the agent verifies the hub certificate by
// default and only skips verification when explicitly opted in.
func TestBuildHubTLSConfig(t *testing.T) {
	// default: verification ON, ServerName set, modern minimum version
	cfg, err := buildHubTLSConfig("hub.example.com", tlsEnvFunc(nil))
	if err != nil {
		t.Fatalf("default: %v", err)
	}
	if cfg.InsecureSkipVerify {
		t.Fatal("default must verify the hub certificate (InsecureSkipVerify=false)")
	}
	if cfg.ServerName != "hub.example.com" {
		t.Fatalf("ServerName = %q, want hub.example.com", cfg.ServerName)
	}
	if cfg.MinVersion < 0x0303 { // TLS 1.2
		t.Fatalf("MinVersion too low: %x", cfg.MinVersion)
	}

	// explicit opt-out
	for _, v := range []string{"true", "1"} {
		c, err := buildHubTLSConfig("h", tlsEnvFunc(map[string]string{"HUB_TLS_INSECURE": v}))
		if err != nil || !c.InsecureSkipVerify {
			t.Fatalf("HUB_TLS_INSECURE=%s must skip verification (err=%v)", v, err)
		}
	}

	// custom CA file (valid PEM) → trusted, still verifying
	caPath := writeTestCACert(t)
	cfg, err = buildHubTLSConfig("h", tlsEnvFunc(map[string]string{"HUB_CA_FILE": caPath}))
	if err != nil {
		t.Fatalf("valid CA: %v", err)
	}
	if cfg.RootCAs == nil || cfg.InsecureSkipVerify {
		t.Fatal("HUB_CA_FILE must set RootCAs and keep verification on")
	}

	// missing CA file → error (fail closed, do not silently fall back to insecure)
	if _, err := buildHubTLSConfig("h", tlsEnvFunc(map[string]string{"HUB_CA_FILE": "/no/such/file"})); err == nil {
		t.Fatal("missing HUB_CA_FILE must error")
	}

	// garbage CA file → error
	bad := filepath.Join(t.TempDir(), "bad.pem")
	if err := os.WriteFile(bad, []byte("not a certificate"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := buildHubTLSConfig("h", tlsEnvFunc(map[string]string{"HUB_CA_FILE": bad})); err == nil {
		t.Fatal("invalid HUB_CA_FILE content must error")
	}
}

// writeTestCACert generates a self-signed cert and returns its PEM file path.
func writeTestCACert(t *testing.T) string {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "test-hub-ca"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		IsCA:         true,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "ca.pem")
	if err := os.WriteFile(path, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}
