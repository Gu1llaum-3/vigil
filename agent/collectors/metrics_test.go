//go:build testing && linux

package collectors

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

// TestCollectMetricsSanity runs the real collector on the test host and checks that the
// values feeding the dashboard and the metric alerts are coherent.
func TestCollectMetricsSanity(t *testing.T) {
	first := CollectMetrics()
	// The CPU percentage and network rates are deltas: the second call has a baseline.
	time.Sleep(200 * time.Millisecond)
	m := CollectMetrics()

	_, err := time.Parse(time.RFC3339, first.CollectedAt)
	assert.NoError(t, err, "CollectedAt must be RFC3339")

	assert.GreaterOrEqual(t, m.CPUPercent, 0.0)
	assert.LessOrEqual(t, m.CPUPercent, 100.0)

	assert.Positive(t, m.MemoryTotalBytes)
	assert.LessOrEqual(t, m.MemoryUsedBytes, m.MemoryTotalBytes)
	assert.GreaterOrEqual(t, m.MemoryUsedPercent, 0.0)
	assert.LessOrEqual(t, m.MemoryUsedPercent, 100.0)

	assert.Positive(t, m.DiskTotalBytes, "root filesystem size")
	assert.LessOrEqual(t, m.DiskUsedBytes, m.DiskTotalBytes)
	assert.GreaterOrEqual(t, m.DiskUsedPercent, 0.0)
	assert.LessOrEqual(t, m.DiskUsedPercent, 100.0)
	assert.GreaterOrEqual(t, m.DiskMaxUsedPercent, 0.0)
	assert.LessOrEqual(t, m.DiskMaxUsedPercent, 100.0)
	for _, mount := range m.DiskMounts {
		assert.NotEmpty(t, mount.Mountpoint)
		assert.GreaterOrEqual(t, mount.UsedPercent, 0.0, mount.Mountpoint)
		assert.LessOrEqual(t, mount.UsedPercent, 100.0, mount.Mountpoint)
		assert.LessOrEqual(t, mount.UsedPercent, m.DiskMaxUsedPercent, "max covers every mount")
	}

	assert.GreaterOrEqual(t, m.Load1, 0.0)
	assert.GreaterOrEqual(t, m.Load5, 0.0)
	assert.GreaterOrEqual(t, m.Load15, 0.0)
}
