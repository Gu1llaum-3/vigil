package agent

import (
	"encoding/json"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"slices"

	"github.com/Gu1llaum-3/vigil/internal/common"
	"golang.org/x/crypto/ssh"
)

// The hub proves its identity by signing the token together with the nonce the agent sent on
// that connection (common.HubChallenge). Hubs older than the nonce sign the token alone: a
// static signature that anyone who captured it once can replay. The agent accepts it only
// from a hub key that has never signed a nonce; the keys that have are recorded in
// <data-dir>/hub-challenge, so a downgrade to the replayable form is refused for good.
const hubChallengeFileName = "hub-challenge"

type hubChallengeState struct {
	// NonceKeys are the SHA256 fingerprints of the hub keys that have signed a nonce.
	NonceKeys []string `json:"nonce_keys"`
}

func loadNonceKeys(dataDir string) []string {
	if dataDir == "" {
		return nil
	}
	data, err := os.ReadFile(filepath.Join(dataDir, hubChallengeFileName))
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			slog.Error("Cannot read the hub challenge record", "err", err)
		}
		return nil
	}
	var state hubChallengeState
	if err := json.Unmarshal(data, &state); err != nil {
		slog.Error("Ignoring a malformed hub challenge record", "err", err)
		return nil
	}
	return state.NonceKeys
}

// keySignedNonce reports whether the hub key with fingerprint fp has signed a nonce before.
func (client *WebSocketClient) keySignedNonce(fp string) bool {
	client.nonceKeysMu.Lock()
	defer client.nonceKeysMu.Unlock()
	return slices.Contains(client.nonceKeys, fp)
}

// rememberNonceKey records that the hub key with fingerprint fp signs nonces.
func (client *WebSocketClient) rememberNonceKey(fp string) {
	client.nonceKeysMu.Lock()
	defer client.nonceKeysMu.Unlock()
	if slices.Contains(client.nonceKeys, fp) {
		return
	}
	client.nonceKeys = append(client.nonceKeys, fp)
	if client.agent.dataDir == "" {
		return
	}
	data, err := json.Marshal(hubChallengeState{NonceKeys: client.nonceKeys})
	if err == nil {
		err = writeDataFile(client.agent.dataDir, hubChallengeFileName, data)
	}
	if err != nil {
		// Still refused in memory until the agent restarts.
		slog.Warn("Cannot record that the hub signs nonces", "err", err)
	}
}

// verifyHubSignature checks the hub's signature for a connection authenticated with token on
// which the agent sent nonce, and returns the fingerprint of the hub key that verified it.
func (client *WebSocketClient) verifyHubSignature(token, nonce string, signature []byte) (string, error) {
	verify := func(pubKey ssh.PublicKey, message []byte) bool {
		return pubKey.Verify(message, &ssh.Signature{Format: pubKey.Type(), Blob: signature}) == nil
	}
	if nonce != "" {
		message := common.HubChallenge(nonce, token)
		for _, pubKey := range client.agent.keys {
			if verify(pubKey, message) {
				fp := ssh.FingerprintSHA256(pubKey)
				client.rememberNonceKey(fp)
				return fp, nil
			}
		}
	}
	for _, pubKey := range client.agent.keys {
		if !verify(pubKey, []byte(token)) {
			continue
		}
		fp := ssh.FingerprintSHA256(pubKey)
		if client.keySignedNonce(fp) {
			return "", errors.New("the hub signed without this connection's nonce although its key has signed nonces before: refusing a replayable signature")
		}
		if nonce != "" {
			slog.Warn("The hub does not sign the connection nonce: its signature can be replayed. Upgrade the hub.")
		}
		return fp, nil
	}
	return "", errors.New("invalid signature - check KEY value")
}
