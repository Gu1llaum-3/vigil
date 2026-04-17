//go:build linux

package collectors

import (
	"bufio"
	"os/exec"
	"strings"
	"time"

	"github.com/Gu1llaum-3/vigil/internal/common"
)

func collectPackagesRedHat() (common.PackageInfo, error) {
	info := common.PackageInfo{}

	outdated, err := dnfOutdatedPackages()
	if err == nil {
		info.Outdated = outdated
		info.OutdatedCount = len(outdated)
		for _, p := range outdated {
			if p.IsSecurity {
				info.SecurityCount++
			}
		}
	}

	installed, err := rpmInstalledCount()
	if err == nil {
		info.InstalledCount = installed
	}

	lastUpgrade, known, err := dnfLastUpgradeTime()
	if err == nil && known {
		info.LastUpgradeAt = lastUpgrade.Format(time.RFC3339)
		info.LastUpgradeAgeDays = int(time.Since(lastUpgrade).Hours() / 24)
		info.LastUpgradeKnown = true
	}

	return info, nil
}

func dnfOutdatedPackages() ([]common.OutdatedPackage, error) {
	// dnf check-update exits with code 100 when updates are available, 0 when none
	cmd := exec.Command("dnf", "check-update", "--quiet")
	out, _ := cmd.Output() // ignore error: exit 100 is normal

	securityPkgs := dnfSecurityPackages()

	var packages []common.OutdatedPackage
	scanner := bufio.NewScanner(strings.NewReader(string(out)))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "Last metadata") || strings.HasPrefix(line, "Obsoleting") {
			continue
		}
		parts := strings.Fields(line)
		if len(parts) < 2 {
			continue
		}
		// Lines: "package.arch  new_version  repo"
		namearch := parts[0]
		candidate := parts[1]
		name := namearch
		if idx := strings.LastIndex(namearch, "."); idx != -1 {
			name = namearch[:idx]
		}

		packages = append(packages, common.OutdatedPackage{
			Name:             name,
			InstalledVersion: "",
			CandidateVersion: candidate,
			IsSecurity:       securityPkgs[name],
		})
	}
	return packages, nil
}

// dnfSecurityPackages returns a set of package names with security updates.
func dnfSecurityPackages() map[string]bool {
	result := make(map[string]bool)
	cmd := exec.Command("dnf", "updateinfo", "list", "security", "--quiet")
	out, err := cmd.Output()
	if err != nil {
		return result
	}
	scanner := bufio.NewScanner(strings.NewReader(string(out)))
	for scanner.Scan() {
		parts := strings.Fields(scanner.Text())
		if len(parts) >= 3 {
			namearch := parts[2]
			name := namearch
			if idx := strings.LastIndex(namearch, "-"); idx != -1 {
				// strip version-release.arch suffix
				name = namearch[:idx]
				if idx2 := strings.LastIndex(name, "-"); idx2 != -1 {
					name = name[:idx2]
				}
			}
			result[name] = true
		}
	}
	return result
}

func rpmInstalledCount() (int, error) {
	cmd := exec.Command("rpm", "-qa")
	out, err := cmd.Output()
	if err != nil {
		return 0, err
	}
	count := 0
	scanner := bufio.NewScanner(strings.NewReader(string(out)))
	for scanner.Scan() {
		if strings.TrimSpace(scanner.Text()) != "" {
			count++
		}
	}
	return count, nil
}

func dnfLastUpgradeTime() (time.Time, bool, error) {
	// dnf history gives last transaction times
	cmd := exec.Command("dnf", "history", "--quiet")
	out, err := cmd.Output()
	if err != nil {
		return time.Time{}, false, err
	}

	scanner := bufio.NewScanner(strings.NewReader(string(out)))
	scanner.Scan() // skip header
	scanner.Scan() // skip separator
	if !scanner.Scan() {
		return time.Time{}, false, nil
	}

	// First data line is the most recent transaction
	// Format: "   4 | ... | 2024-01-15 10:30 | Upgrade | ..."
	line := scanner.Text()
	parts := strings.Split(line, "|")
	if len(parts) < 3 {
		return time.Time{}, false, nil
	}
	dateStr := strings.TrimSpace(parts[2])
	t, err := time.Parse("2006-01-02 15:04", dateStr)
	if err != nil {
		return time.Time{}, false, err
	}
	return t, true, nil
}
