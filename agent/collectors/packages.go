//go:build linux

package collectors

import "github.com/Gu1llaum-3/vigil/internal/common"

// CollectPackages collects package state using the platform-appropriate tool.
func CollectPackages(osFamily string) (common.PackageInfo, error) {
	switch osFamily {
	case "Debian":
		return collectPackagesDebian()
	case "RedHat":
		return collectPackagesRedHat()
	default:
		return common.PackageInfo{}, nil
	}
}
