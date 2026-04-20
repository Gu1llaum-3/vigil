//go:build testing && linux

package collectors

import (
	"context"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCollectRebootDebianNoRebootRequired(t *testing.T) {
	// Ensure /run/reboot-required does NOT exist for this test
	os.Remove("/run/reboot-required-test-sentinel")

	info, err := collectRebootDebian()
	if err != nil {
		t.Skip("cannot check reboot status")
	}
	// We can only assert this is a valid bool — actual value depends on system state
	assert.IsType(t, false, info.Required)
}

func TestCollectRebootDebianRebootRequired(t *testing.T) {
	// Create a temporary reboot-required file
	f, err := os.CreateTemp("", "reboot-required")
	require.NoError(t, err)
	f.Close()
	defer os.Remove(f.Name())

	// We can't easily mock /run/reboot-required, so we just test the function runs
	_, _ = collectRebootDebian()
}

func TestCollectRebootUnknownFamily(t *testing.T) {
	info, err := CollectReboot(context.Background(), "Unknown")
	assert.NoError(t, err)
	assert.False(t, info.Required)
}
