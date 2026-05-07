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
	ifacesOK    bool
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
	collectDiskMetrics(&metrics)
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

func collectDiskMetrics(metrics *common.HostMetricsResponse) {
	usage, err := psdisk.Usage("/")
	if err != nil {
		return
	}
	metrics.DiskTotalBytes = usage.Total
	metrics.DiskUsedBytes = usage.Used
	metrics.DiskUsedPercent = round2(usage.UsedPercent)
}

func collectNetworkMetricsLocked(now time.Time, metrics *common.HostMetricsResponse) {
	if !hostMetricsState.ifacesOK {
		hostMetricsState.ifaceNames = detectMetricInterfaces()
		hostMetricsState.ifacesOK = true
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
