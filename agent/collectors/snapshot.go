//go:build linux

package collectors

import (
	"log/slog"
	"time"

	"github.com/Gu1llaum-3/vigil/internal/common"
)

// CollectSnapshot assembles a full HostSnapshotResponse from all collectors.
// Each domain is collected independently — a failure in one does not block others.
func CollectSnapshot() common.HostSnapshotResponse {
	hostname, primaryIP, osInfo, kernel, arch, uptimeSecs, resources, network, sysErr := CollectSystem()
	if sysErr != nil {
		slog.Warn("system collector failed", "err", sysErr)
	}

	storage, err := CollectStorage()
	if err != nil {
		slog.Warn("storage collector failed", "err", err)
	}

	packages, err := CollectPackages(osInfo.Family)
	if err != nil {
		slog.Warn("packages collector failed", "err", err)
	}

	repositories, err := CollectRepositories(osInfo.Family)
	if err != nil {
		slog.Warn("repositories collector failed", "err", err)
	}

	reboot, err := CollectReboot(osInfo.Family)
	if err != nil {
		slog.Warn("reboot collector failed", "err", err)
	}

	docker, err := CollectDocker()
	if err != nil {
		slog.Warn("docker collector failed", "err", err)
	}

	return common.HostSnapshotResponse{
		Hostname:      hostname,
		PrimaryIP:     primaryIP,
		OS:            osInfo,
		Kernel:        kernel,
		Architecture:  arch,
		UptimeSeconds: uptimeSecs,
		Resources:     resources,
		Network:       network,
		Storage:       storage,
		Packages:      packages,
		Repositories:  repositories,
		Reboot:        reboot,
		Docker:        docker,
		CollectedAt:   time.Now().UTC().Format(time.RFC3339),
	}
}
