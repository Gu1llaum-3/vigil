package ghupdate

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"strings"
)

// verifyAssetChecksum checks that the file at assetPath matches the SHA-256 recorded for
// assetName in a goreleaser-style checksums file. It fails closed: a missing entry or a
// mismatch returns an error so the updater refuses to install an unverified binary.
func verifyAssetChecksum(assetPath string, checksums []byte, assetName string) error {
	want, ok := findChecksum(checksums, assetName)
	if !ok {
		return fmt.Errorf("no checksum entry for %q", assetName)
	}
	got, err := sha256File(assetPath)
	if err != nil {
		return err
	}
	if !strings.EqualFold(got, want) {
		return fmt.Errorf("checksum mismatch for %s: got %s, want %s", assetName, got, want)
	}
	return nil
}

// findChecksum parses a checksums file ("<hex>  <filename>" per line, the goreleaser
// format) and returns the lowercased hex digest recorded for name.
func findChecksum(data []byte, name string) (string, bool) {
	for _, line := range strings.Split(string(data), "\n") {
		fields := strings.Fields(strings.TrimSpace(line))
		if len(fields) < 2 {
			continue
		}
		// the filename is the last field; some tools prefix binary entries with '*'
		fname := strings.TrimPrefix(fields[len(fields)-1], "*")
		if fname == name {
			return strings.ToLower(fields[0]), true
		}
	}
	return "", false
}

// sha256File returns the lowercased hex SHA-256 of the file at path.
func sha256File(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
