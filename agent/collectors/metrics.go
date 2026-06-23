//go:build linux

package collectors

import (
	"math"
	"net"
	"strings"
	"sync"
	"time"

	"github.com/Gu1llaum-3/vigil/internal/common"
	pscpu "github.com/shirou/gopsutil/v4/cpu"
	psdisk "github.com/shirou/gopsutil/v4/disk"
	psload "github.com/shirou/gopsutil/v4/load"
	psmem "github.com/shirou/gopsutil/v4/mem"
	psnet "github.com/shirou/gopsutil/v4/net"
)

type metricsState struct {
	mu          sync.Mutex
	prevCPU     pscpu.TimesStat
	prevCPUOK   bool
	prevRxBytes uint64
	prevTxBytes uint64
	prevNetAt   time.Time
	ifaceNames  map[string]struct{}
	ifacesAt    time.Time
	diskParts   []psdisk.PartitionStat
	diskPartsAt time.Time
}

// ifaceRedetectInterval bounds how long a detected interface set is reused before
// being re-evaluated, so interfaces that appear after startup (VPN, hot-plugged NIC,
// or a boot race where none were up yet) are eventually picked up. The same interval
// is reused to cache the mounted-filesystem list (cheap to reuse, slow to enumerate).
const ifaceRedetectInterval = 5 * time.Minute

// networkFsTypes are remote/network filesystems excluded from disk metrics: probing
// their usage can block (a slow/unreachable NFS/CIFS server) and they are not local
// capacity. This keeps the high-frequency metrics path from stalling.
var networkFsTypes = map[string]struct{}{
	"nfs": {}, "nfs4": {}, "cifs": {}, "smb": {}, "smbfs": {},
	"fuse.sshfs": {}, "fuse.glusterfs": {}, "glusterfs": {}, "ceph": {},
	"afs": {}, "9p": {},
}

func isNetworkFs(fstype string) bool {
	_, ok := networkFsTypes[strings.ToLower(fstype)]
	return ok
}

var hostMetricsState metricsState

// CollectMetrics gathers lightweight resource metrics suitable for frequent polling.
func CollectMetrics() common.HostMetricsResponse {
	now := time.Now().UTC()
	metrics := common.HostMetricsResponse{
		CollectedAt: now.Format(time.RFC3339),
	}

	hostMetricsState.mu.Lock()
	defer hostMetricsState.mu.Unlock()

	metrics.CPUPercent = collectCPUPercentLocked()
	collectMemoryMetrics(&metrics)
	collectDiskMetricsLocked(now, &metrics)
	collectLoadMetrics(&metrics)
	collectNetworkMetricsLocked(now, &metrics)

	return metrics
}

func collectCPUPercentLocked() float64 {
	times, err := pscpu.Times(false)
	if err != nil || len(times) == 0 {
		return 0
	}
	current := times[0]
	if !hostMetricsState.prevCPUOK {
		hostMetricsState.prevCPU = current
		hostMetricsState.prevCPUOK = true
		return 0
	}
	percent := calculateCPUPercent(hostMetricsState.prevCPU, current)
	hostMetricsState.prevCPU = current
	return round2(percent)
}

func calculateCPUPercent(prev, current pscpu.TimesStat) float64 {
	prevTotal, prevBusy := getCPUAllBusy(prev)
	currTotal, currBusy := getCPUAllBusy(current)
	if currTotal <= prevTotal || currBusy < prevBusy {
		return 0
	}
	return clampPercent((currBusy - prevBusy) / (currTotal - prevTotal) * 100)
}

func getCPUAllBusy(sample pscpu.TimesStat) (total float64, busy float64) {
	total = sample.Total() - sample.Guest - sample.GuestNice
	busy = total - sample.Idle - sample.Iowait
	return total, busy
}

func collectMemoryMetrics(metrics *common.HostMetricsResponse) {
	vm, err := psmem.VirtualMemory()
	if err != nil {
		return
	}
	metrics.MemoryTotalBytes = vm.Total
	metrics.MemoryUsedBytes = vm.Used
	metrics.MemoryUsedPercent = round2(vm.UsedPercent)
}

// collectDiskMetricsLocked sets root usage (DiskUsedPercent) and the highest used%
// across all real local mounted filesystems (DiskMaxUsedPercent/DiskMaxMount), so a
// full secondary partition (e.g. /data) is caught and named, not just root. The
// partition list is cached and root is read once via the same loop. Must be called
// with hostMetricsState.mu held.
func collectDiskMetricsLocked(now time.Time, metrics *common.HostMetricsResponse) {
	parts := diskPartitionsLocked(now)
	rootSeen := false
	for _, p := range parts {
		usage, err := psdisk.Usage(p.Mountpoint)
		if err != nil || usage.Total == 0 {
			continue
		}
		used := round2(usage.UsedPercent)
		if p.Mountpoint == "/" {
			metrics.DiskTotalBytes = usage.Total
			metrics.DiskUsedBytes = usage.Used
			metrics.DiskUsedPercent = used
			rootSeen = true
		}
		if used > metrics.DiskMaxUsedPercent {
			metrics.DiskMaxUsedPercent = used
			metrics.DiskMaxMount = p.Mountpoint
		}
	}

	// Fallback: if root was not in the (filtered/empty) partition list, read it
	// directly so DiskUsedPercent and the max are still populated.
	if !rootSeen {
		if usage, err := psdisk.Usage("/"); err == nil {
			used := round2(usage.UsedPercent)
			metrics.DiskTotalBytes = usage.Total
			metrics.DiskUsedBytes = usage.Used
			metrics.DiskUsedPercent = used
			if used > metrics.DiskMaxUsedPercent {
				metrics.DiskMaxUsedPercent = used
				metrics.DiskMaxMount = "/"
			}
		}
	}
}

// diskPartitionsLocked returns the local mounted filesystems, cached for
// ifaceRedetectInterval and excluding network/remote filesystems. Must be called
// with hostMetricsState.mu held.
func diskPartitionsLocked(now time.Time) []psdisk.PartitionStat {
	if hostMetricsState.diskPartsAt.IsZero() || now.Sub(hostMetricsState.diskPartsAt) >= ifaceRedetectInterval {
		// Partitions(false) already excludes pseudo filesystems (tmpfs, overlay, proc).
		parts, err := psdisk.Partitions(false)
		if err == nil {
			local := make([]psdisk.PartitionStat, 0, len(parts))
			for _, p := range parts {
				// Exclude network filesystems (can block on probe) and pseudo/read-only-image
				// filesystems — notably squashfs snap images which are always ~100% full and
				// would otherwise trigger false disk alerts.
				if isNetworkFs(p.Fstype) || isPseudoFs(p.Fstype) {
					continue
				}
				local = append(local, p)
			}
			hostMetricsState.diskParts = local
			hostMetricsState.diskPartsAt = now
		}
	}
	return hostMetricsState.diskParts
}

func collectLoadMetrics(metrics *common.HostMetricsResponse) {
	avg, err := psload.Avg()
	if err != nil || avg == nil {
		return
	}
	metrics.Load1 = round2(avg.Load1)
	metrics.Load5 = round2(avg.Load5)
	metrics.Load15 = round2(avg.Load15)
}

func collectNetworkMetricsLocked(now time.Time, metrics *common.HostMetricsResponse) {
	if hostMetricsState.ifacesAt.IsZero() ||
		len(hostMetricsState.ifaceNames) == 0 ||
		now.Sub(hostMetricsState.ifacesAt) >= ifaceRedetectInterval {
		hostMetricsState.ifaceNames = detectMetricInterfaces()
		hostMetricsState.ifacesAt = now
	}
	if len(hostMetricsState.ifaceNames) == 0 {
		return
	}

	counters, err := psnet.IOCounters(true)
	if err != nil {
		return
	}

	var rxBytes uint64
	var txBytes uint64
	for _, counter := range counters {
		if _, ok := hostMetricsState.ifaceNames[counter.Name]; !ok {
			continue
		}
		rxBytes += counter.BytesRecv
		txBytes += counter.BytesSent
	}

	if hostMetricsState.prevNetAt.IsZero() {
		hostMetricsState.prevNetAt = now
		hostMetricsState.prevRxBytes = rxBytes
		hostMetricsState.prevTxBytes = txBytes
		return
	}

	elapsed := now.Sub(hostMetricsState.prevNetAt)
	if elapsed <= 0 {
		return
	}

	if rxBytes >= hostMetricsState.prevRxBytes {
		metrics.NetworkRxBps = uint64(float64(rxBytes-hostMetricsState.prevRxBytes) / elapsed.Seconds())
	}
	if txBytes >= hostMetricsState.prevTxBytes {
		metrics.NetworkTxBps = uint64(float64(txBytes-hostMetricsState.prevTxBytes) / elapsed.Seconds())
	}

	hostMetricsState.prevNetAt = now
	hostMetricsState.prevRxBytes = rxBytes
	hostMetricsState.prevTxBytes = txBytes
}

func detectMetricInterfaces() map[string]struct{} {
	result := map[string]struct{}{}
	ifaces, err := net.Interfaces()
	if err != nil {
		return result
	}
	for _, iface := range ifaces {
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 {
			continue
		}
		name := iface.Name
		if strings.HasPrefix(name, "docker") || strings.HasPrefix(name, "br-") || strings.HasPrefix(name, "veth") || strings.HasPrefix(name, "cali") {
			continue
		}
		result[name] = struct{}{}
	}
	return result
}

func clampPercent(value float64) float64 {
	return math.Min(100, math.Max(0, value))
}

func round2(value float64) float64 {
	return math.Round(value*100) / 100
}
