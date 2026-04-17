//go:build linux

package collectors

import "github.com/Gu1llaum-3/vigil/internal/common"

// CollectRepositories collects repository configuration using the platform-appropriate tool.
func CollectRepositories(osFamily string) ([]common.RepositoryInfo, error) {
	switch osFamily {
	case "Debian":
		return collectRepositoriesDebian()
	case "RedHat":
		return collectRepositoriesRedHat()
	default:
		return nil, nil
	}
}
