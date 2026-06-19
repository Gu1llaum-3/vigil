//go:build testing

package ghupdate

import (
	"os"
	"path/filepath"
	"testing"
)

// TestVerifyAssetChecksum locks the #6 integrity check: a matching digest passes, while
// a mismatch or a missing entry fails closed (so a trojaned download is rejected).
func TestVerifyAssetChecksum(t *testing.T) {
	dir := t.TempDir()
	asset := filepath.Join(dir, "vigil_1.2.3_linux_amd64.tar.gz")
	if err := os.WriteFile(asset, []byte("release-binary-bytes"), 0o644); err != nil {
		t.Fatal(err)
	}
	sum, err := sha256File(asset)
	if err != nil {
		t.Fatal(err)
	}

	// matching checksum (goreleaser format, plus an unrelated line) → ok
	good := []byte("deadbeef  some_other_file\n" + sum + "  vigil_1.2.3_linux_amd64.tar.gz\n")
	if err := verifyAssetChecksum(asset, good, "vigil_1.2.3_linux_amd64.tar.gz"); err != nil {
		t.Fatalf("valid checksum rejected: %v", err)
	}

	// tampered binary / wrong digest → error
	bad := []byte("0000000000000000000000000000000000000000000000000000000000000000  vigil_1.2.3_linux_amd64.tar.gz\n")
	if err := verifyAssetChecksum(asset, bad, "vigil_1.2.3_linux_amd64.tar.gz"); err == nil {
		t.Fatal("checksum mismatch must error")
	}

	// no entry for the asset → error (fail closed)
	if err := verifyAssetChecksum(asset, good, "vigil_9.9.9_windows_amd64.zip"); err == nil {
		t.Fatal("missing checksum entry must error")
	}
}

func TestFindChecksum(t *testing.T) {
	data := []byte("abc123  file-a.tar.gz\nDEF456 *file-b.zip\n")
	if got, ok := findChecksum(data, "file-a.tar.gz"); !ok || got != "abc123" {
		t.Fatalf("file-a: got %q ok=%v", got, ok)
	}
	// '*' binary-mode prefix is stripped; digest is lowercased
	if got, ok := findChecksum(data, "file-b.zip"); !ok || got != "def456" {
		t.Fatalf("file-b: got %q ok=%v", got, ok)
	}
	if _, ok := findChecksum(data, "missing"); ok {
		t.Fatal("missing file must not be found")
	}
}
