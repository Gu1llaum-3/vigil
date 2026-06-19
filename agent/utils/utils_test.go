package utils

import (
	"testing"

	app "github.com/Gu1llaum-3/vigil"
	"github.com/stretchr/testify/assert"
)

func TestGetEnv(t *testing.T) {
	key := "TEST_VAR"
	prefixedKey := app.AgentEnvPrefix + key

	t.Run("prefixed variable exists", func(t *testing.T) {
		t.Setenv(prefixedKey, "prefixed_val")
		t.Setenv(key, "unprefixed_val")

		val, exists := GetEnv(key)
		assert.True(t, exists)
		assert.Equal(t, "prefixed_val", val)
	})

	t.Run("only unprefixed variable exists", func(t *testing.T) {
		t.Setenv(key, "unprefixed_val")

		val, exists := GetEnv(key)
		assert.True(t, exists)
		assert.Equal(t, "unprefixed_val", val)
	})

	t.Run("neither variable exists", func(t *testing.T) {
		val, exists := GetEnv(key)
		assert.False(t, exists)
		assert.Empty(t, val)
	})
}
