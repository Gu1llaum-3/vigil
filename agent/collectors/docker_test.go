//go:build testing && linux

package collectors

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestDockerAvailable(t *testing.T) {
	// Just verify it returns a bool without panicking
	result := DockerAvailable()
	assert.IsType(t, false, result)
}

func TestCollectDockerUnavailable(t *testing.T) {
	// If docker socket doesn't exist, state should be "not_configured"
	if DockerAvailable() {
		t.Skip("Docker is available on this system — skipping unavailable test")
	}
	info, err := CollectDocker(context.Background())
	assert.NoError(t, err)
	assert.Equal(t, "not_configured", info.State)
	assert.Equal(t, 0, info.ContainerCount)
}

func TestCollectDockerAvailable(t *testing.T) {
	if !DockerAvailable() {
		t.Skip("Docker not available on this system")
	}
	info, err := CollectDocker(context.Background())
	assert.NoError(t, err)
	assert.Equal(t, "available", info.State)
	assert.GreaterOrEqual(t, info.ContainerCount, 0)
	assert.GreaterOrEqual(t, info.RunningCount, 0)
}
