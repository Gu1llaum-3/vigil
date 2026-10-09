//go:build linux

package collectors

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"regexp"
	"strings"
	"time"

	"github.com/Gu1llaum-3/vigil/internal/common"
)

func collectPackagesRedHat(ctx context.Context) (common.PackageInfo, error) {
	info := common.PackageInfo{}

	tool, err := redhatPackageTool()
	var outdated []common.OutdatedPackage
	if err == nil {
		outdated, err = rpmOutdatedPackages(ctx, tool)
	}
	if err != nil {
		label := tool
		if label == "" {
			label = "dnf or yum"
		}
		info.OutdatedError = pendingUpdatesError(ctx, label, err)
	} else {
		info.Outdated = outdated
		info.OutdatedCount = len(outdated)
		for _, p := range outdated {
			if p.IsSecurity {
				info.SecurityCount++
			}
		}
	}

	installed, err := rpmInstalledCount(ctx)
	if err == nil {
		info.InstalledCount = installed
	}

	if tool != "" {
		lastUpgrade, known, err := rpmLastTransactionTime(ctx, tool)
		if err == nil && known {
			info.LastUpgradeAt = lastUpgrade.Format(time.RFC3339)
			info.LastUpgradeAgeDays = int(time.Since(lastUpgrade).Hours() / 24)
			info.LastUpgradeKnown = true
		}
	}

	return info, nil
}

// redhatPackageTool returns the package manager to query: dnf (dnf4 or dnf5), or yum on
// hosts that only have yum (Amazon Linux 2, CentOS/RHEL 7). Both answer check-update,
// updateinfo and history alike, output aside.
func redhatPackageTool() (string, error) {
	for _, tool := range []string{"dnf", "yum"} {
		if _, err := exec.LookPath(tool); err == nil {
			return tool, nil
		}
	}
	return "", fmt.Errorf("no dnf or yum: %w", exec.ErrNotFound)
}

// rpmOutdatedPackages lists the pending updates with `<tool> check-update`.
func rpmOutdatedPackages(ctx context.Context, tool string) ([]common.OutdatedPackage, error) {
	// check-update exits with 100 when updates are available, 0 when there are none, and
	// 1 on errors (e.g. no reachable repository).
	cmd := exec.CommandContext(ctx, tool, "check-update", "--quiet")
	out, err := cmd.Output()
	if err != nil {
		var exitErr *exec.ExitError
		if !errors.As(err, &exitErr) || exitErr.ExitCode() != 100 {
			return nil, err
		}
	}
	packages := parseCheckUpdate(string(out))
	security := rpmSecurityPackages(ctx, tool)
	for i := range packages {
		packages[i].IsSecurity = security[packages[i].Name]
	}
	return packages, nil
}

// parseCheckUpdate parses check-update's "<name>.<arch>  <version>  <repo>" lines.
//
// Under "Obsoleting Packages", each entry is followed by an indented line for the
// installed package it replaces (repo "@<origin>", or "<unknown>"): not a pending update.
// A package updated while it obsoletes another is listed in both sections: counted once.
// yum, writing to a pipe, moves what does not fit its columns to indented lines of their
// own: a long name is followed by "version repo", or by the version then the repository
// on two lines; a long version by the repository alone.
func parseCheckUpdate(out string) []common.OutdatedPackage {
	var packages []common.OutdatedPackage
	seen := map[string]bool{}
	lines := strings.Split(out, "\n")
	indented := func(l string) bool { return l != strings.TrimLeft(l, " \t") && strings.TrimSpace(l) != "" }
	obsoleting := false
	for i := 0; i < len(lines); i++ {
		raw := lines[i]
		line := strings.TrimSpace(raw)
		switch {
		case line == "" || strings.HasPrefix(line, "Last metadata"):
			continue
		case strings.HasPrefix(line, "Obsoleting"):
			obsoleting = true
			continue
		case obsoleting && raw != strings.TrimLeft(raw, " \t"):
			continue // the installed package being replaced
		}
		parts := strings.Fields(line)
		for len(parts) < 3 && i+1 < len(lines) && indented(lines[i+1]) {
			parts = append(parts, strings.Fields(lines[i+1])...)
			i++
		}
		if len(parts) < 2 {
			continue
		}
		if len(parts) >= 3 && strings.HasPrefix(parts[2], "@") {
			continue
		}
		name := parts[0]
		if idx := strings.LastIndex(name, "."); idx != -1 {
			name = name[:idx]
		}
		if seen[name] {
			continue
		}
		seen[name] = true
		packages = append(packages, common.OutdatedPackage{Name: name, CandidateVersion: parts[1]})
	}
	return packages
}

// rpmSecurityPackages returns the names of the packages with a pending security advisory.
// `updateinfo list --security` works with yum, dnf4 and dnf5 (dnf5 reads the older
// `list security` as an advisory name and finds none); the package is the field that reads
// as a NEVRA: third with yum/dnf4, fourth after dnf5's Type and Severity columns.
func rpmSecurityPackages(ctx context.Context, tool string) map[string]bool {
	result := make(map[string]bool)
	cmd := exec.CommandContext(ctx, tool, "updateinfo", "list", "--security", "--quiet")
	out, err := cmd.Output()
	if err != nil {
		return result
	}
	for line := range strings.SplitSeq(string(out), "\n") {
		for _, field := range strings.Fields(line) {
			if name, ok := nevraName(field); ok {
				result[name] = true
				break
			}
		}
	}
	return result
}

// nevraArch matches the ".<arch>" suffix (noarch, x86_64, x86_64_v2, aarch64, ppc64le,
// loongarch64...): any word, as the two dashes nevraName also requires already rule out
// the other columns (advisory ids, severities, dates).
var nevraArch = regexp.MustCompile(`\.[A-Za-z0-9_]+$`)

// nevraName returns the name of a "<name>-[<epoch>:]<version>-<release>.<arch>" string.
func nevraName(s string) (string, bool) {
	loc := nevraArch.FindStringIndex(s)
	if loc == nil {
		return "", false
	}
	nvr := s[:loc[0]]
	rel := strings.LastIndex(nvr, "-")
	if rel <= 0 {
		return "", false
	}
	ver := strings.LastIndex(nvr[:rel], "-")
	if ver <= 0 {
		return "", false
	}
	return nvr[:ver], true
}

func rpmInstalledCount(ctx context.Context) (int, error) {
	cmd := exec.CommandContext(ctx, "rpm", "-qa")
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

// historyTime matches a transaction's date in `<tool> history list`: "2026-10-09 08:56" in
// yum/dnf4's "|"-separated table, in local time; "2026-10-09 08:57:33" in dnf5's, in UTC
// whatever the host's time zone.
var historyTime = regexp.MustCompile(`\b(\d{4}-\d{2}-\d{2} \d{2}:\d{2})(:\d{2})?\b`)

// rpmLastTransactionTime returns the time of the most recent transaction (listed first).
// dnf5 has no bare `history`, so `history list`, which all three accept.
func rpmLastTransactionTime(ctx context.Context, tool string) (time.Time, bool, error) {
	out, err := exec.CommandContext(ctx, tool, "history", "list").Output()
	if err != nil {
		return time.Time{}, false, err
	}
	return parseHistoryList(string(out))
}

func parseHistoryList(out string) (time.Time, bool, error) {
	for line := range strings.SplitSeq(out, "\n") {
		m := historyTime.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		loc := time.Local
		if m[2] != "" { // dnf5
			loc = time.UTC
		}
		t, err := time.ParseInLocation("2006-01-02 15:04", m[1], loc)
		if err != nil {
			return time.Time{}, false, err
		}
		return t, true, nil
	}
	return time.Time{}, false, nil
}
