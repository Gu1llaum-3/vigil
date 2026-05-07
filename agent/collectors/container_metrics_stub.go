package collectors

import "github.com/Gu1llaum-3/vigil/internal/common"

// CollectContainerMetrics returns an empty payload on non-Linux platforms.
func CollectContainerMetrics() common.ContainerMetricsSnapshotResponse {
	return common.ContainerMetricsSnapshotResponse{}
}
