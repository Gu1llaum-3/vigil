//go:build !linux

package collectors

import "github.com/Gu1llaum-3/vigil/internal/common"

// CollectMetrics returns an empty metrics payload on non-Linux platforms.
func CollectMetrics() common.HostMetricsResponse {
	return common.HostMetricsResponse{}
}
