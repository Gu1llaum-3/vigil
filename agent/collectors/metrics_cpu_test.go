//go:build testing && linux

package collectors

import (
	"testing"

	pscpu "github.com/shirou/gopsutil/v4/cpu"
	"github.com/stretchr/testify/assert"
)

// TestGetCPUAllBusy pins the CPU accounting: guest time is already part of user/nice,
// so it must not be counted twice in the total.
func TestGetCPUAllBusy(t *testing.T) {
	s := pscpu.TimesStat{
		User: 100, System: 50, Idle: 800, Nice: 10, Iowait: 20,
		Irq: 5, Softirq: 7, Steal: 3, Guest: 30, GuestNice: 4,
	}
	total, busy := getCPUAllBusy(s)
	// Same as gopsutil's Total() minus guest time.
	assert.InDelta(t, s.User+s.System+s.Idle+s.Nice+s.Iowait+s.Irq+s.Softirq+s.Steal, total, 1e-9)
	assert.InDelta(t, total-s.Idle-s.Iowait, busy, 1e-9)
	assert.InDelta(t, 995.0, total, 1e-9)
	assert.InDelta(t, 175.0, busy, 1e-9)
}
