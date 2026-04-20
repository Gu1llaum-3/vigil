//go:build linux

package collectors

import (
	"bufio"
	"compress/gzip"
	"context"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/Gu1llaum-3/vigil/internal/common"
)

func collectPackagesDebian(ctx context.Context) (common.PackageInfo, error) {
	info := common.PackageInfo{}

	outdated, err := aptOutdatedPackages(ctx)
	if err == nil {
		info.Outdated = outdated
		info.OutdatedCount = len(outdated)
		for _, p := range outdated {
			if p.IsSecurity {
				info.SecurityCount++
			}
		}
	}

	installed, err := dpkgInstalledCount(ctx)
	if err == nil {
		info.InstalledCount = installed
	}

	lastUpgrade, known, err := aptLastUpgradeTime()
	if err == nil && known {
		info.LastUpgradeAt = lastUpgrade.Format(time.RFC3339)
		info.LastUpgradeAgeDays = int(time.Since(lastUpgrade).Hours() / 24)
		info.LastUpgradeKnown = true
	}

	return info, nil
}

func aptOutdatedPackages(ctx context.Context) ([]common.OutdatedPackage, error) {
	// apt-get -s upgrade lists packages that would be upgraded
	cmd := exec.CommandContext(ctx, "apt-get", "-s", "upgrade")
	out, err := cmd.Output()
	if err != nil {
		return nil, err
	}

	// Also get security packages list
	securityPkgs := aptSecurityPackages(ctx)

	var packages []common.OutdatedPackage
	scanner := bufio.NewScanner(strings.NewReader(string(out)))
	for scanner.Scan() {
		line := scanner.Text()
		// Lines like: "Inst package [installed] (candidate source)"
		if !strings.HasPrefix(line, "Inst ") {
			continue
		}
		parts := strings.Fields(line)
		if len(parts) < 2 {
			continue
		}
		name := parts[1]

		installed := ""
		candidate := ""
		// Parse "[installed_version]" and "(candidate_version ..."
		for i, p := range parts {
			if strings.HasPrefix(p, "[") && strings.HasSuffix(p, "]") {
				installed = strings.Trim(p, "[]")
			}
			if i > 0 && strings.HasPrefix(p, "(") {
				candidate = strings.TrimPrefix(parts[i], "(")
				candidate = strings.TrimSuffix(candidate, ")")
			}
		}

		packages = append(packages, common.OutdatedPackage{
			Name:             name,
			InstalledVersion: installed,
			CandidateVersion: candidate,
			IsSecurity:       securityPkgs[name],
		})
	}
	return packages, nil
}

// aptSecurityPackages returns a set of package names that have security updates.
func aptSecurityPackages(ctx context.Context) map[string]bool {
	result := make(map[string]bool)
	cmd := exec.CommandContext(ctx, "apt-get", "-s", "-o", "APT::Get::Show-Upgraded=true",
		"--just-print", "dist-upgrade")
	out, err := cmd.Output()
	if err != nil {
		return result
	}
	scanner := bufio.NewScanner(strings.NewReader(string(out)))
	inSecurity := false
	for scanner.Scan() {
		line := scanner.Text()
		if strings.Contains(line, "security") {
			inSecurity = true
		}
		if inSecurity && strings.HasPrefix(line, "Inst ") {
			parts := strings.Fields(line)
			if len(parts) >= 2 {
				result[parts[1]] = true
			}
		}
	}
	return result
}

func dpkgInstalledCount(ctx context.Context) (int, error) {
	cmd := exec.CommandContext(ctx, "dpkg", "-l")
	out, err := cmd.Output()
	if err != nil {
		return 0, err
	}
	count := 0
	scanner := bufio.NewScanner(strings.NewReader(string(out)))
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "ii ") {
			count++
		}
	}
	return count, nil
}

func aptLastUpgradeTime() (time.Time, bool, error) {
	pattern := "/var/log/apt/history.log*"
	files, err := filepath.Glob(pattern)
	if err != nil || len(files) == 0 {
		return time.Time{}, false, err
	}

	var lastTime time.Time
	for _, file := range files {
		t, err := parseAptHistoryFile(file)
		if err != nil {
			continue
		}
		if t.After(lastTime) {
			lastTime = t
		}
	}

	if lastTime.IsZero() {
		return time.Time{}, false, nil
	}
	return lastTime, true, nil
}

func parseAptHistoryFile(path string) (time.Time, error) {
	var reader io.Reader
	f, err := os.Open(path)
	if err != nil {
		return time.Time{}, err
	}
	defer f.Close()

	if strings.HasSuffix(path, ".gz") {
		gz, err := gzip.NewReader(f)
		if err != nil {
			return time.Time{}, err
		}
		defer gz.Close()
		reader = gz
	} else {
		reader = f
	}

	var lastTime time.Time
	scanner := bufio.NewScanner(reader)
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "End-Date: ") {
			dateStr := strings.TrimPrefix(line, "End-Date: ")
			// Format: "2024-01-15  10:30:45"
			t, err := time.Parse("2006-01-02  15:04:05", dateStr)
			if err != nil {
				t, err = time.Parse("2006-01-02 15:04:05", dateStr)
			}
			if err == nil && t.After(lastTime) {
				lastTime = t
			}
		}
	}
	return lastTime, nil
}
