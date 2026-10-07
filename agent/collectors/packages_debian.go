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
	"regexp"
	"strings"
	"time"

	"github.com/Gu1llaum-3/vigil/internal/common"
)

func collectPackagesDebian(ctx context.Context) (common.PackageInfo, error) {
	info := common.PackageInfo{}

	outdated, err := aptOutdatedPackages(ctx)
	if err != nil {
		logPendingUpdatesError("apt", err)
	} else {
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

	var packages []common.OutdatedPackage
	scanner := bufio.NewScanner(strings.NewReader(string(out)))
	for scanner.Scan() {
		if pkg, ok := parseAptInstLine(scanner.Text()); ok {
			packages = append(packages, pkg)
		}
	}
	return packages, nil
}

// aptInstLine matches a simulated upgrade line:
//
//	Inst <name> [<installed>] (<candidate> <origin>[, <origin>...] [<arch>]) [<deps> ]
//
// The installed version is absent for a newly installed package.
var aptInstLine = regexp.MustCompile(`^Inst (\S+) (?:\[([^\]]*)\] )?\((\S+) ([^)]*)\)`)

// parseAptInstLine parses one "Inst" line.
func parseAptInstLine(line string) (common.OutdatedPackage, bool) {
	m := aptInstLine.FindStringSubmatch(line)
	if m == nil {
		return common.OutdatedPackage{}, false
	}
	return common.OutdatedPackage{
		Name:             m[1],
		InstalledVersion: m[2],
		CandidateVersion: m[3],
		IsSecurity:       aptOriginsIncludeSecurity(m[4]),
	}, true
}

// aptOriginsIncludeSecurity reports whether one of the comma-separated
// "<label>:<version>/<archive>" origins of an Inst line is a security archive:
// a -security archive (Debian-Security:12/stable-security, Ubuntu:22.04/jammy-security,
// UbuntuESM:22.04/jammy-infra-security) or the Debian-Security label, whose Debian 10
// archives have no suffix (Debian-Security:10/oldoldstable).
func aptOriginsIncludeSecurity(origins string) bool {
	// Drop the trailing " [<arch>]".
	if i := strings.LastIndex(origins, " ["); i != -1 {
		origins = origins[:i]
	}
	for origin := range strings.SplitSeq(origins, ", ") {
		label, rest, _ := strings.Cut(strings.TrimSpace(origin), ":")
		_, archive, _ := strings.Cut(rest, "/")
		if strings.HasSuffix(archive, "-security") || strings.EqualFold(label, "Debian-Security") {
			return true
		}
	}
	return false
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
	defer func() { _ = f.Close() }() // read-only

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
