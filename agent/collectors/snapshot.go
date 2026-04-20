//go:build linux

package collectors

import (
	"context"
	"log/slog"
	"time"

	"github.com/Gu1llaum-3/vigil/internal/common"
)

// CollectSnapshot assembles a full HostSnapshotResponse from all collectors.
// Each domain is collected independently — a failure in one does not block others.
// A 45s timeout is applied to subprocess-based collectors (packages, reboot, docker)
// so that a blocked apt/dnf/needs-restarting call does not hang the agent indefinitely.
// The hub's WebSocket timeout for GetHostSnapshot is 60s; the 15s margin allows
// the response to be transmitted before the hub gives up.
func CollectSnapshot() common.HostSnapshotResponse {
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()

	hostname, primaryIP, osInfo, kernel, arch, uptimeSecs, resources, network, sysErr := CollectSystem()
	if sysErr != nil {
		slog.Warn("system collector failed", "err", sysErr)
	}

	storage, err := CollectStorage()
	if err != nil {
		slog.Warn("storage collector failed", "err", err)
	}

	packages, err := CollectPackages(ctx, osInfo.Family)
	if err != nil {
		slog.Warn("packages collector failed", "err", err)
	}

	repositories, err := CollectRepositories(osInfo.Family)
	if err != nil {
		slog.Warn("repositories collector failed", "err", err)
	}

	reboot, err := CollectReboot(ctx, osInfo.Family)
	if err != nil {
		slog.Warn("reboot collector failed", "err", err)
	}

	docker, err := CollectDocker(ctx)
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
