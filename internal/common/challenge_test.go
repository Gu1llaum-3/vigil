//go:build testing

package common

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestHubChallenge(t *testing.T) {
	nonce := strings.Repeat("ab", 32)
	other := strings.Repeat("cd", 32)

	assert.Equal(t, []byte("token"), HubChallenge("", "token"), "without a nonce the hub signs the token (agents older than the nonce)")
	assert.NotEqual(t, []byte("token"), HubChallenge(nonce, "token"))
	assert.NotEqual(t, HubChallenge(nonce, "token"), HubChallenge(other, "token"), "a signature is only good for its nonce")
	assert.NotEqual(t, HubChallenge(nonce, "token"), HubChallenge(nonce, "other"))
	assert.True(t, strings.HasPrefix(string(HubChallenge(nonce, "token")), "vigil-hub-challenge-v1\x00"))
}

func TestValidChallengeNonce(t *testing.T) {
	assert.True(t, ValidChallengeNonce(strings.Repeat("0f", 32)))
	assert.False(t, ValidChallengeNonce(""))
	assert.False(t, ValidChallengeNonce(strings.Repeat("0f", 31)), "too short")
	assert.False(t, ValidChallengeNonce(strings.Repeat("0f", 33)), "too long")
	assert.False(t, ValidChallengeNonce(strings.Repeat("0F", 32)), "lowercase hex only")
	assert.False(t, ValidChallengeNonce(strings.Repeat("zz", 32)))
}

func TestNewChallengeNonce(t *testing.T) {
	a, b := NewChallengeNonce(), NewChallengeNonce()
	assert.True(t, ValidChallengeNonce(a))
	assert.NotEqual(t, a, b)
}
