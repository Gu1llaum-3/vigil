//go:build testing && linux

package collectors

import (
	"context"
	"os"
	"testing"

	"github.com/Gu1llaum-3/vigil/internal/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseAptHistoryFile(t *testing.T) {
	content := `Start-Date: 2024-01-10  08:00:00
Commandline: apt-get upgrade
Upgrade: curl:amd64 (7.81.0, 7.88.1)
End-Date: 2024-01-10  08:01:00

Start-Date: 2024-01-15  10:30:00
Commandline: apt-get upgrade
Upgrade: vim:amd64 (2:8.2.3995, 2:9.0.0)
End-Date: 2024-01-15  10:31:00
`
	f, err := os.CreateTemp("", "apt-history*.log")
	require.NoError(t, err)
	defer os.Remove(f.Name())
	_, err = f.WriteString(content)
	require.NoError(t, err)
	f.Close()

	ts, err := parseAptHistoryFile(f.Name())
	require.NoError(t, err)
	assert.False(t, ts.IsZero(), "should parse at least one End-Date")
	assert.Equal(t, 2024, ts.Year())
	assert.Equal(t, 15, ts.Day())
}

func TestAptOutdatedPackagesDebian(t *testing.T) {
	// Real `apt-get -s upgrade` output of a Debian 12.0 image with pending updates.
	fakeCommand(t, "apt-get", "-s upgrade", "apt-get-upgrade-debian12.txt", 0)

	pkgs, err := aptOutdatedPackages(context.Background())
	require.NoError(t, err)
	require.Len(t, pkgs, 45)

	byName := map[string]common.OutdatedPackage{}
	security := 0
	for _, p := range pkgs {
		byName[p.Name] = p
		if p.IsSecurity {
			security++
		}
	}
	// Only the packages whose own origin is a -security suite.
	assert.Equal(t, 7, security)

	assert.Equal(t, common.OutdatedPackage{
		Name: "perl-base", InstalledVersion: "5.36.0-7", CandidateVersion: "5.36.0-7+deb12u4", IsSecurity: true,
	}, byName["perl-base"])
	// Several origins, one of them security.
	assert.True(t, byName["libgcrypt20"].IsSecurity)
	// Listed after a security package, but not one itself.
	assert.Equal(t, common.OutdatedPackage{
		Name: "sed", InstalledVersion: "4.9-1", CandidateVersion: "4.9-1+deb12u1",
	}, byName["sed"])
	// Trailing "[libstdc++6:arm64 libgcc-s1:arm64 ]" dependency list must not be read as a version.
	assert.Equal(t, common.OutdatedPackage{
		Name: "gcc-12-base", InstalledVersion: "12.2.0-14", CandidateVersion: "12.2.0-14+deb12u1",
	}, byName["gcc-12-base"])
	// Epochs are kept.
	assert.Equal(t, "1:2.38.1-5+deb12u3", byName["bsdutils"].CandidateVersion)
}

func TestAptOutdatedPackagesUbuntu(t *testing.T) {
	// Real `apt-get -s upgrade` output of an ubuntu:jammy-20230126 image.
	fakeCommand(t, "apt-get", "-s upgrade", "apt-get-upgrade-ubuntu2204.txt", 0)

	pkgs, err := aptOutdatedPackages(context.Background())
	require.NoError(t, err)
	require.Len(t, pkgs, 60)

	byName := map[string]common.OutdatedPackage{}
	security := 0
	for _, p := range pkgs {
		byName[p.Name] = p
		if p.IsSecurity {
			security++
		}
	}
	assert.Equal(t, 45, security)
	assert.Equal(t, common.OutdatedPackage{
		Name: "libssl3", InstalledVersion: "3.0.2-0ubuntu1.7", CandidateVersion: "3.0.2-0ubuntu1.30", IsSecurity: true,
	}, byName["libssl3"])
	assert.Equal(t, common.OutdatedPackage{
		Name: "base-files", InstalledVersion: "12ubuntu4.2", CandidateVersion: "12ubuntu4.7",
	}, byName["base-files"])
}

func TestAptOutdatedPackagesOrigins(t *testing.T) {
	// Synthetic lines covering origin edge cases not present in the captured outputs.
	fakeCommand(t, "apt-get", "-s upgrade", "apt-get-upgrade-origins-synthetic.txt", 0)

	pkgs, err := aptOutdatedPackages(context.Background())
	require.NoError(t, err)
	assert.Equal(t, []common.OutdatedPackage{
		{Name: "esm-app-pkg", InstalledVersion: "1.0-1", CandidateVersion: "1.0-1ubuntu0.1+esm1", IsSecurity: true},
		{Name: "esm-infra-pkg", InstalledVersion: "2.0-1", CandidateVersion: "2.0-1ubuntu0.1+esm1", IsSecurity: true},
		// Debian 10 security archive names have no -security suffix.
		{Name: "buster-pkg", InstalledVersion: "3.0-1", CandidateVersion: "3.0-1+deb10u1", IsSecurity: true},
		// A third-party suite that merely contains "-security" is not a security archive.
		{Name: "vendor-tool", InstalledVersion: "4.0", CandidateVersion: "4.1"},
		{Name: "plain-pkg", InstalledVersion: "5.0", CandidateVersion: "5.1"},
		// A newly installed package has no installed version.
		{Name: "new-pkg", CandidateVersion: "6.0-1"},
	}, pkgs)
}

func TestAptOutdatedPackagesUpToDate(t *testing.T) {
	fakeCommand(t, "apt-get", "-s upgrade", "apt-get-upgrade-none.txt", 0)

	pkgs, err := aptOutdatedPackages(context.Background())
	require.NoError(t, err)
	assert.Empty(t, pkgs)
}

func TestAptOutdatedPackagesFailure(t *testing.T) {
	// apt-get exits 100 on errors such as a locked or broken package database.
	fakeCommand(t, "apt-get", "-s upgrade", "apt-get-upgrade-none.txt", 100)

	_, err := aptOutdatedPackages(context.Background())
	assert.Error(t, err)
}

func TestAptOutdatedPackagesMissing(t *testing.T) {
	withoutCommand(t)

	_, err := aptOutdatedPackages(context.Background())
	assert.Error(t, err)
}

func TestDpkgInstalledCount(t *testing.T) {
	count, err := dpkgInstalledCount(context.Background())
	if err != nil {
		t.Skip("dpkg not available")
	}
	assert.GreaterOrEqual(t, count, 0)
}
