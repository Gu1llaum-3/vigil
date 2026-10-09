//go:build testing && linux

package collectors

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/Gu1llaum-3/vigil/internal/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeDnf serves `dnf check-update` (with exitCode) and `dnf updateinfo list security`.
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
  "updateinfo list security --quiet") cat %q; exit 0 ;;
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

	pkgs, err := dnfOutdatedPackages(context.Background())
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

	pkgs, err := dnfOutdatedPackages(context.Background())
	require.NoError(t, err)
	assert.Empty(t, pkgs)
}

func TestDnfOutdatedPackagesFailure(t *testing.T) {
	// Exit code 1 is an error (e.g. no reachable repository), not "no updates".
	fakeCommand(t, "dnf", "check-update --quiet", "empty.txt", 1)

	_, err := dnfOutdatedPackages(context.Background())
	assert.Error(t, err)
}

func TestDnfOutdatedPackagesMissing(t *testing.T) {
	withoutCommand(t)

	_, err := dnfOutdatedPackages(context.Background())
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
	_, _, _ = dnfLastUpgradeTime(context.Background())
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
	assert.Equal(t, "dnf not found", info.OutdatedError)
}
