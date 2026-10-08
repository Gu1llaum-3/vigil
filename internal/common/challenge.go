package common

import (
	"crypto/rand"
	"encoding/hex"
)

// ChallengeNonceHeader carries the nonce an agent sends when it connects. The hub signs it
// with the token (HubChallenge), so a captured signature cannot be replayed on another
// connection.
const ChallengeNonceHeader = "X-Nonce"

const (
	challengeNonceBytes  = 32
	hubChallengePrefixV1 = "vigil-hub-challenge-v1\x00"
)

// NewChallengeNonce returns a fresh random nonce (64 lowercase hex characters).
func NewChallengeNonce() string {
	b := make([]byte, challengeNonceBytes)
	// crypto/rand.Read never fails (it crashes the program instead).
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// ValidChallengeNonce reports whether nonce has the form NewChallengeNonce produces.
func ValidChallengeNonce(nonce string) bool {
	if len(nonce) != 2*challengeNonceBytes {
		return false
	}
	for _, c := range nonce {
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return false
		}
	}
	return true
}

// HubChallenge returns the message the hub signs to prove its identity to an agent. With a
// nonce it is domain separated and bound to that connection; without one (agents older than
// the nonce) it is the token alone, a static signature.
func HubChallenge(nonce, token string) []byte {
	if nonce == "" {
		return []byte(token)
	}
	return []byte(hubChallengePrefixV1 + nonce + "\x00" + token)
}
