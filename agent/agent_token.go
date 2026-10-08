package agent

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"log/slog"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/Gu1llaum-3/vigil/internal/common"
	"github.com/fxamacker/cbor/v2"
	"golang.org/x/crypto/ssh"
)

// agentTokenFileName stores the per-agent token the hub issued (SetAgentToken). It replaces
// the shared enrollment token the agent was installed with: with a shared token, the only
// thing telling hosts apart is their fingerprint, a hash of the hostname anyone can compute.
const agentTokenFileName = "agent-token"

// minAgentTokenLen rejects anything weaker than what the hub mints (40 random characters).
const minAgentTokenLen = 32

type storedAgentToken struct {
	// Hub and HubKey identify the hub that issued the token (canonical URL, SHA-256
	// fingerprint of the key it proved its identity with): the token is only sent to that
	// hub, recognised by either, so moving the hub to a new URL keeps working.
	Hub    string `json:"hub"`
	HubKey string `json:"hub_key,omitempty"`
	Token  string `json:"token"`
	// Previous is the token that authenticated the connection the last SetAgentToken came
	// on, kept as a fallback in case the hub did not save the new one (lost acknowledgement,
	// hub restart, failed save). If it is the configured (enrollment) token, it is dropped
	// once Token has authenticated a verified connection (Unconfirmed false).
	Previous string `json:"previous,omitempty"`
	// Unconfirmed: Token has not authenticated a verified connection yet.
	Unconfirmed bool `json:"unconfirmed,omitempty"`
	// Configured is the SHA-256 of the configured TOKEN when the token was issued: if TOKEN
	// changes afterwards, an admin set it on purpose (e.g. after a rotation while the agent
	// was offline) and it is tried first. Otherwise the configured token is never used again.
	Configured string `json:"configured,omitempty"`
}

func tokenDigest(token string) string {
	if token == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

// validAgentToken accepts what the hub mints (security.RandomString: letters and digits).
func validAgentToken(token string) bool {
	if len(token) < minAgentTokenLen || len(token) > 128 {
		return false
	}
	for _, r := range token {
		if (r < 'a' || r > 'z') && (r < 'A' || r > 'Z') && (r < '0' || r > '9') {
			return false
		}
	}
	return true
}

// hubKeyFor returns the canonical form of a hub URL the token file is bound to.
func hubKeyFor(u *url.URL) string {
	scheme := "ws"
	if s := strings.ToLower(u.Scheme); s == "https" || s == "wss" {
		scheme = "wss"
	}
	return scheme + "://" + strings.ToLower(u.Host) + strings.TrimSuffix(u.Path, "/")
}

func keyFingerprints(keys []ssh.PublicKey) []string {
	fps := make([]string, 0, len(keys))
	for _, k := range keys {
		fps = append(fps, ssh.FingerprintSHA256(k))
	}
	return fps
}

func readAgentTokenFile(dataDir string) (storedAgentToken, bool) {
	var stored storedAgentToken
	if dataDir == "" {
		return stored, false
	}
	data, err := os.ReadFile(filepath.Join(dataDir, agentTokenFileName))
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			slog.Warn("Cannot read the stored agent token; using the configured TOKEN", "err", err)
		}
		return stored, false
	}
	if err := json.Unmarshal(data, &stored); err != nil || stored.Token == "" {
		slog.Warn("Ignoring a malformed stored agent token; using the configured TOKEN")
		return storedAgentToken{}, false
	}
	return stored, true
}

// loadAgentToken returns the stored tokens (current, previous) issued by this hub: the one
// at hubURL, or the one whose key is among the configured hub keys.
func loadAgentToken(dataDir, hubURL string, hubKeys []string) (storedAgentToken, bool) {
	stored, ok := readAgentTokenFile(dataDir)
	if !ok {
		return stored, false
	}
	if stored.Hub == hubURL {
		return stored, true
	}
	// Same hub (same key) at another URL — but never over a weaker transport than the
	// token was issued on.
	downgrade := strings.HasPrefix(stored.Hub, "wss://") && !strings.HasPrefix(hubURL, "wss://")
	if stored.HubKey != "" && slices.Contains(hubKeys, stored.HubKey) && !downgrade {
		return stored, true
	}
	return storedAgentToken{}, false
}

// saveAgentToken writes the token file (0600, through a temporary file renamed into place
// so a crash never leaves a truncated credential).
func saveAgentToken(dataDir string, stored storedAgentToken) error {
	if dataDir == "" {
		return errors.New("no data directory to store the agent token in")
	}
	data, err := json.Marshal(stored)
	if err != nil {
		return err
	}
	return writeDataFile(dataDir, agentTokenFileName, data)
}

// writeDataFile durably replaces <dataDir>/<name> with data (0600, through a temporary file
// renamed into place so a crash never leaves a truncated file).
func writeDataFile(dataDir, name string, data []byte) error {
	tmp, err := os.CreateTemp(dataDir, name+".*")
	if err != nil {
		return err
	}
	defer func() { _ = os.Remove(tmp.Name()) }() // no-op once renamed
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	// On disk before the agent acknowledges (the hub switches to an issued token right after).
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmp.Name(), filepath.Join(dataDir, name)); err != nil {
		return err
	}
	// Make the rename itself durable (best effort: not every platform syncs directories).
	if dir, err := os.Open(dataDir); err == nil {
		_ = dir.Sync()
		_ = dir.Close() // read-only
	}
	return nil
}

// confirmAgentToken runs once a connection authenticated with sent has been verified: the
// hub accepts that token. A confirmed issued token needs no fallback any more; a fallback
// that worked (the hub never saved the token it issued) becomes the token, so later
// connections do not spend three rejections on the unsaved one first.
func confirmAgentToken(dataDir, sent, configured string) {
	stored, ok := readAgentTokenFile(dataDir)
	if !ok || sent == "" || sent == configured {
		return
	}
	switch {
	case sent == stored.Token && stored.Unconfirmed:
	case sent == stored.Previous:
		stored.Token = sent
	default:
		return
	}
	stored.Unconfirmed = false
	stored.Previous = ""
	if err := saveAgentToken(dataDir, stored); err != nil {
		slog.Warn("Failed to record the agent token as confirmed", "err", err)
	}
}

// DeleteAgentToken removes the stored agent token (fingerprint reset: the host gets a new
// identity, so it enrolls again with the configured token).
func DeleteAgentToken(dataDir string) error {
	err := os.Remove(filepath.Join(dataDir, agentTokenFileName))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}

// SetAgentTokenHandler stores the per-agent token issued by the (verified) hub and uses it
// for every later connection.
type SetAgentTokenHandler struct{}

func (h *SetAgentTokenHandler) Handle(hctx *HandlerContext) error {
	var req common.SetAgentTokenRequest
	if err := cbor.Unmarshal(hctx.Request.Data, &req); err != nil {
		return err
	}
	if !validAgentToken(req.Token) {
		return errors.New("refusing a malformed agent token")
	}
	client := hctx.Client
	stored := storedAgentToken{
		Hub:         client.hubKey,
		HubKey:      client.verifiedKeyFingerprint(hctx.Conn),
		Token:       req.Token,
		Configured:  tokenDigest(client.configuredToken),
		Unconfirmed: true,
	}
	// Keep as fallback the token this very connection authenticated with: the hub accepts
	// it until it has saved the new one.
	if sent := client.sentToken(hctx.Conn); sent != "" && sent != req.Token {
		stored.Previous = sent
	} else if old, ok := readAgentTokenFile(hctx.Agent.dataDir); ok && old.Previous != req.Token {
		stored.Previous = old.Previous
	}
	if err := saveAgentToken(hctx.Agent.dataDir, stored); err != nil {
		return err
	}
	client.setToken(req.Token)
	slog.Info("Stored the agent token issued by the hub")
	return hctx.SendResponse(map[string]bool{"ok": true}, hctx.RequestID)
}
