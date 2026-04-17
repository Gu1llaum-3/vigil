//go:build linux

package collectors

import (
	"bufio"
	"os"
	"strings"
	"syscall"

	"github.com/Gu1llaum-3/vigil/internal/common"
)

var skipFSTypes = map[string]bool{
	"proc": true, "sysfs": true, "devtmpfs": true, "devpts": true,
	"tmpfs": true, "cgroup": true, "cgroup2": true, "pstore": true,
	"securityfs": true, "debugfs": true, "tracefs": true, "hugetlbfs": true,
	"mqueue": true, "fusectl": true, "overlay": true, "squashfs": true,
	"efivarfs": true, "bpf": true, "autofs": true, "configfs": true,
}

// CollectStorage returns a list of mounted physical filesystems with usage stats.
func CollectStorage() ([]common.StorageMount, error) {
	f, err := os.Open("/proc/mounts")
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var mounts []common.StorageMount
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) < 3 {
			continue
		}
		device := fields[0]
		mountpoint := fields[1]
		fsType := fields[2]

		if skipFSTypes[fsType] {
			continue
		}
		if strings.HasPrefix(device, "tmpfs") || strings.HasPrefix(mountpoint, "/sys") ||
			strings.HasPrefix(mountpoint, "/proc") || strings.HasPrefix(mountpoint, "/dev") {
			continue
		}

		var stat syscall.Statfs_t
		if err := syscall.Statfs(mountpoint, &stat); err != nil {
			continue
		}

		total := stat.Blocks * uint64(stat.Bsize)
		available := stat.Bavail * uint64(stat.Bsize)
		used := total - (stat.Bfree * uint64(stat.Bsize))
		var usedPct float64
		if total > 0 {
			usedPct = float64(used) / float64(total) * 100
		}

		mounts = append(mounts, common.StorageMount{
			Device:         device,
			Mountpoint:     mountpoint,
			FSType:         fsType,
			TotalBytes:     total,
			UsedBytes:      used,
			AvailableBytes: available,
			UsedPercent:    usedPct,
		})
	}
	return mounts, nil
}
