//go:build testing

package ghupdate

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"os"
	"path/filepath"
	"testing"
)

// writeTarGz builds a .tar.gz at path from the given entries.
type tarEntry struct {
	name     string
	body     string
	typeflag byte
	linkname string
}

func writeTarGz(t *testing.T, path string, entries []tarEntry) {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for _, e := range entries {
		tf := e.typeflag
		if tf == 0 {
			tf = tar.TypeReg
		}
		hdr := &tar.Header{Name: e.name, Mode: 0o644, Size: int64(len(e.body)), Typeflag: tf, Linkname: e.linkname}
		if err := tw.WriteHeader(hdr); err != nil {
			t.Fatal(err)
		}
		if tf == tar.TypeReg && len(e.body) > 0 {
			if _, err := tw.Write([]byte(e.body)); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
}

// TestExtractTarGzRejectsPathTraversal locks the TarSlip fix: an entry that escapes the
// destination directory must be rejected and must not write outside destDir.
func TestExtractTarGzRejectsPathTraversal(t *testing.T) {
	dir := t.TempDir()
	archive := filepath.Join(dir, "evil.tar.gz")
	writeTarGz(t, archive, []tarEntry{{name: "../escape.txt", body: "pwned"}})

	dest := filepath.Join(dir, "out")
	if err := extractTarGz(archive, dest); err == nil {
		t.Fatal("expected an error for a path-traversal entry, got nil")
	}
	if _, err := os.Stat(filepath.Join(dir, "escape.txt")); err == nil {
		t.Fatal("path-traversal entry escaped destDir and was written")
	}
}

// TestExtractTarGzSkipsSymlinks locks that symlink entries are skipped (not created),
// since they enable arbitrary file links outside destDir.
func TestExtractTarGzSkipsSymlinks(t *testing.T) {
	dir := t.TempDir()
	archive := filepath.Join(dir, "link.tar.gz")
	writeTarGz(t, archive, []tarEntry{
		{name: "vigil", body: "binary"},
		{name: "evil-link", typeflag: tar.TypeSymlink, linkname: "/etc/passwd"},
	})

	dest := filepath.Join(dir, "out")
	if err := extractTarGz(archive, dest); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, err := os.Lstat(filepath.Join(dest, "evil-link")); err == nil {
		t.Fatal("symlink entry should have been skipped, but it was created")
	}
	// the regular file is still extracted
	if b, err := os.ReadFile(filepath.Join(dest, "vigil")); err != nil || string(b) != "binary" {
		t.Fatalf("regular file not extracted correctly: %v", err)
	}
}
