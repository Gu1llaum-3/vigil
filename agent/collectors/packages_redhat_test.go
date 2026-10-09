//go:build testing && linux

package collectors

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/Gu1llaum-3/vigil/internal/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeDnf serves `dnf check-update` (with exitCode) and `dnf updateinfo list --security`.
func fakeDnf(t *testing.T, checkUpdateFixture string, exitCode int) {
	t.Helper()
	dir := fakeBinDir(t)
	abs := func(f string) string {
		p, err := filepath.Abs(filepath.Join("testdata", f))
		require.NoError(t, err)
		return p
	}
	script := fmt.Sprintf(`#!/bin/sh
case "$*" in
  "check-update --quiet") cat %q; exit %d ;;
  "updateinfo list --security --quiet") cat %q; exit 0 ;;
esac
echo "unexpected: dnf $*" >&2
exit 99
`, abs(checkUpdateFixture), exitCode, abs("dnf-updateinfo-security-rocky9.txt"))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "dnf"), []byte(script), 0o755))
}

func TestDnfOutdatedPackagesRocky(t *testing.T) {
	// Real output of a Rocky Linux 9.0 image with pending updates (exit code 100),
	// plus an "Obsoleting Packages" section.
	fakeDnf(t, "dnf-check-update-rocky9.txt", 100)

	pkgs, err := rpmOutdatedPackages(context.Background(), "dnf")
	require.NoError(t, err)
	require.Len(t, pkgs, 125)

	byName := map[string]common.OutdatedPackage{}
	security := 0
	for _, p := range pkgs {
		byName[p.Name] = p
		if p.IsSecurity {
			security++
		}
	}
	assert.Equal(t, 73, security)
	assert.Equal(t, common.OutdatedPackage{Name: "openssl-libs", CandidateVersion: "1:3.5.8-1.el9_8", IsSecurity: true}, byName["openssl-libs"])
	assert.Equal(t, common.OutdatedPackage{Name: "grub2-tools-efi", CandidateVersion: "1:2.06-104.el9_8"}, byName["grub2-tools-efi"])
	// The installed package an obsoleting one replaces is not a pending update.
	assert.NotContains(t, byName, "grub2-tools")
}

func TestDnfOutdatedPackagesUpToDate(t *testing.T) {
	// dnf check-update exits 0 with no output when nothing is pending.
	fakeDnf(t, "empty.txt", 0)

	pkgs, err := rpmOutdatedPackages(context.Background(), "dnf")
	require.NoError(t, err)
	assert.Empty(t, pkgs)
}

func TestDnfOutdatedPackagesFailure(t *testing.T) {
	// Exit code 1 is an error (e.g. no reachable repository), not "no updates".
	fakeCommand(t, "dnf", "check-update --quiet", "empty.txt", 1)

	_, err := rpmOutdatedPackages(context.Background(), "dnf")
	assert.Error(t, err)
}

func TestDnfOutdatedPackagesMissing(t *testing.T) {
	withoutCommand(t)

	_, err := rpmOutdatedPackages(context.Background(), "dnf")
	assert.Error(t, err)
}

func TestRpmInstalledCountGraceful(t *testing.T) {
	count, err := rpmInstalledCount(context.Background())
	if err != nil {
		t.Skip("rpm not available")
	}
	assert.GreaterOrEqual(t, count, 0)
}

func TestDnfLastUpgradeTimeGraceful(t *testing.T) {
	_, _, _ = rpmLastTransactionTime(context.Background(), "dnf")
}

// A failed query leaves the counts unknown, not zero: the snapshot says so.
func TestCollectPackagesRedHatReportsQueryFailure(t *testing.T) {
	fakeCommand(t, "dnf", "check-update --quiet", "empty.txt", 1)

	info, err := collectPackagesRedHat(context.Background())
	require.NoError(t, err)
	assert.Contains(t, info.OutdatedError, "dnf pending updates query failed")
	assert.Zero(t, info.OutdatedCount)

	fakeDnf(t, "empty.txt", 0)
	info, err = collectPackagesRedHat(context.Background())
	require.NoError(t, err)
	assert.Empty(t, info.OutdatedError)

	withoutCommand(t)
	info, err = collectPackagesRedHat(context.Background())
	require.NoError(t, err)
	assert.Equal(t, "dnf or yum not found", info.OutdatedError)
}

// fakeRpmTool installs tool serving check-update, updateinfo list --security and
// history list from fixtures.
func fakeRpmTool(t *testing.T, tool, checkUpdate string, exitCode int, security, history string) {
	t.Helper()
	dir := fakeBinDir(t)
	abs := func(f string) string {
		p, err := filepath.Abs(filepath.Join("testdata", f))
		require.NoError(t, err)
		return p
	}
	script := fmt.Sprintf(`#!/bin/sh
case "$*" in
  "check-update --quiet") cat %q; exit %d ;;
  "updateinfo list --security --quiet") cat %q; exit 0 ;;
  "history list") cat %q; exit 0 ;;
esac
echo "unexpected: %s $*" >&2
exit 99
`, abs(checkUpdate), exitCode, abs(security), abs(history), tool)
	writeScript(t, dir, tool, script)
}

// Amazon Linux 2 / CentOS 7 have yum only: real output of an amazonlinux:2 image.
func TestCollectPackagesYumOnly(t *testing.T) {
	fakeRpmTool(t, "yum", "yum-check-update-amzn2.txt", 100, "yum-updateinfo-security-amzn2.txt", "yum-history-amzn2.txt")
	isolatePath(t, "yum") // no real dnf (e.g. /bin/dnf on a Fedora machine)

	info, err := collectPackagesRedHat(context.Background())
	require.NoError(t, err)
	assert.Empty(t, info.OutdatedError)
	assert.Equal(t, 11, info.OutdatedCount)
	assert.Equal(t, 5, info.SecurityCount)
	byName := map[string]common.OutdatedPackage{}
	for _, p := range info.Outdated {
		byName[p.Name] = p
	}
	assert.Equal(t, common.OutdatedPackage{Name: "openssl-libs", CandidateVersion: "1:1.0.2k-24.amzn2.0.22", IsSecurity: true}, byName["openssl-libs"])
	assert.False(t, byName["rpm"].IsSecurity)
	assert.True(t, info.LastUpgradeKnown, "yum history list")
}

// isolatePath leaves in PATH only the fake commands and sh/cat, so a real dnf or yum on the
// test machine cannot be picked instead.
func isolatePath(t *testing.T, fakes ...string) {
	t.Helper()
	dir := t.TempDir()
	for _, name := range append(fakes, "sh", "cat", "sleep") {
		path, err := exec.LookPath(name)
		require.NoError(t, err, name)
		require.NoError(t, os.Symlink(path, filepath.Join(dir, name)))
	}
	t.Setenv("PATH", dir)
}

// dnf is preferred when both exist (RHEL 8+, where yum is dnf).
func TestRedhatPackageToolPrefersDnf(t *testing.T) {
	dir := fakeBinDir(t)
	writeScript(t, dir, "dnf", "#!/bin/sh\nexit 0\n")
	writeScript(t, dir, "yum", "#!/bin/sh\nexit 0\n")
	isolatePath(t, "dnf", "yum")
	tool, err := redhatPackageTool()
	require.NoError(t, err)
	assert.Equal(t, "dnf", tool)
}

// Fedora 41+ ship dnf5: security advisories need `--security`, with an extra column;
// history needs `history list`. Real output of a fedora:42 image.
func TestCollectPackagesDnf5(t *testing.T) {
	fakeRpmTool(t, "dnf", "dnf5-check-update-fedora42.txt", 100, "dnf5-updateinfo-security-fedora42.txt", "dnf5-history-fedora42.txt")

	info, err := collectPackagesRedHat(context.Background())
	require.NoError(t, err)
	assert.Empty(t, info.OutdatedError)
	assert.Equal(t, 23, info.OutdatedCount)
	var security []string
	for _, p := range info.Outdated {
		if p.IsSecurity {
			security = append(security, p.Name)
		}
	}
	assert.ElementsMatch(t, []string{"glib2", "vim-data", "vim-minimal", "krb5-libs", "openssl-libs", "rpm-sequoia", "curl", "libcurl"}, security)
	require.True(t, info.LastUpgradeKnown)
	// dnf5 prints its history in UTC whatever the host's time zone.
	assert.True(t, time.Date(2026, 10, 9, 8, 57, 0, 0, time.UTC).Equal(mustParseRFC3339(t, info.LastUpgradeAt)))
}

func mustParseRFC3339(t *testing.T, s string) time.Time {
	t.Helper()
	v, err := time.Parse(time.RFC3339, s)
	require.NoError(t, err)
	return v.In(time.Local)
}

// yum moves what does not fit its columns to indented lines: a long name is followed by
// "version repo", or (long name and long version) by the version then the repository on
// two lines; a long version by the repository alone. Shapes seen on amazonlinux:2's yum
// writing to a pipe.
func TestParseCheckUpdateWrappedLines(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("testdata", "yum-check-update-wrapped-synthetic.txt"))
	require.NoError(t, err)
	assert.Equal(t, []common.OutdatedPackage{
		{Name: "NetworkManager-dispatcher-routing-rules-with-a-very-long-name", CandidateVersion: "1:1.18.8-1.amzn2.0.1"},
		{Name: "curl", CandidateVersion: "8.3.0-1.amzn2.0.13"},
		{Name: "a-package-with-a-very-long-name-and-a-long-version-both", CandidateVersion: "1.0.0-1.amzn2.0.1.with.a.long.release"},
		{Name: "long-version-only", CandidateVersion: "2.0.0-1.amzn2.0.1.with.a.long.release.tag"},
	}, parseCheckUpdate(string(raw)))
}

// A package updated while it obsoletes another is listed in both sections: counted once.
func TestParseCheckUpdateObsoletingDuplicate(t *testing.T) {
	out := "zz.noarch   2.0-1   local\n\nObsoleting packages\nzz.noarch   2.0-1   local\n    old.noarch   1.0-1   <unknown>\n"
	assert.Equal(t, []common.OutdatedPackage{{Name: "zz", CandidateVersion: "2.0-1"}}, parseCheckUpdate(out))
}

func TestNevraName(t *testing.T) {
	for in, want := range map[string]string{
		"openssl-libs-1:1.0.2k-24.amzn2.0.22.aarch64": "openssl-libs",
		"vim-data-2:9.2.390-1.fc42.noarch":            "vim-data",
		"acl-2.4.0-1.el9_8.x86_64":                    "acl",
		"glibc-2.39-1.el10.x86_64_v2":                 "glibc",
		"python3.11-libs-3.11.9-1.el9.loongarch64":    "python3.11-libs",
		"RLSA-2026:42736":                             "",
		"Moderate/Sec.":                               "",
		"FEDORA-2025-16acfe9927":                      "",
		"2025-08-13":                                  "",
		"Package":                                     "",
	} {
		got, ok := nevraName(in)
		assert.Equal(t, want, got, in)
		assert.Equal(t, want != "", ok, in)
	}
}

func TestParseHistoryList(t *testing.T) {
	for fixture, want := range map[string]time.Time{
		"yum-history-amzn2.txt":     time.Date(2026, 10, 9, 8, 56, 0, 0, time.Local),
		"dnf5-history-fedora42.txt": time.Date(2026, 10, 9, 8, 57, 0, 0, time.UTC),
	} {
		raw, err := os.ReadFile(filepath.Join("testdata", fixture))
		require.NoError(t, err)
		got, known, err := parseHistoryList(string(raw))
		require.NoError(t, err, fixture)
		assert.True(t, known, fixture)
		assert.Equal(t, want, got, fixture)
	}
	_, known, err := parseHistoryList("Loaded plugins: ovl\nNo transactions\n")
	require.NoError(t, err)
	assert.False(t, known)
}
