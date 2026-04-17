package agent

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"strings"
)

const fingerprintFileName = "fingerprint"

// GetFingerprint returns the agent fingerprint. It first tries to read a saved
// fingerprint from the data directory. If not found, it generates one from the hostname.
// If a new fingerprint is generated and a dataDir is provided, it is saved.
func GetFingerprint(dataDir string) string {
	if dataDir != "" {
		if fp, err := readFingerprint(dataDir); err == nil {
			return fp
		}
	}
	fp := generateFingerprint()
	if dataDir != "" {
		_ = SaveFingerprint(dataDir, fp)
	}
	return fp
}

// generateFingerprint creates a fingerprint from the system hostname.
func generateFingerprint() string {
	hostname, _ := os.Hostname()
	sum := sha256.Sum256([]byte(hostname))
	return hex.EncodeToString(sum[:24])
}

// readFingerprint reads the saved fingerprint from the data directory.
func readFingerprint(dataDir string) (string, error) {
	fp, err := os.ReadFile(filepath.Join(dataDir, fingerprintFileName))
	if err != nil {
		return "", err
	}
	s := strings.TrimSpace(string(fp))
	if s == "" {
		return "", errors.New("fingerprint file is empty")
	}
	return s, nil
}

// SaveFingerprint writes the fingerprint to the data directory.
func SaveFingerprint(dataDir, fingerprint string) error {
	return os.WriteFile(filepath.Join(dataDir, fingerprintFileName), []byte(fingerprint), 0o644)
}

// DeleteFingerprint removes the saved fingerprint file from the data directory.
func DeleteFingerprint(dataDir string) error {
	err := os.Remove(filepath.Join(dataDir, fingerprintFileName))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}
