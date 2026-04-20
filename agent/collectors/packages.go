//go:build linux

package collectors

import (
	"context"

	"github.com/Gu1llaum-3/vigil/internal/common"
)

// CollectPackages collects package state using the platform-appropriate tool.
func CollectPackages(ctx context.Context, osFamily string) (common.PackageInfo, error) {
	switch osFamily {
	case "Debian":
		return collectPackagesDebian(ctx)
	case "RedHat":
		return collectPackagesRedHat(ctx)
	default:
		return common.PackageInfo{}, nil
	}
}
