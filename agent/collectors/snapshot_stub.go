//go:build !linux

package collectors

import "github.com/Gu1llaum-3/vigil/internal/common"

// CollectSnapshot returns an empty snapshot on non-Linux platforms.
// The agent is only fully functional on Linux.
func CollectSnapshot() common.HostSnapshotResponse {
	return common.HostSnapshotResponse{}
}

// DockerAvailable always returns false on non-Linux platforms.
func DockerAvailable() bool {
	return false
}
