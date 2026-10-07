//go:build testing

package app

import (
	"bufio"
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Upstream MIT notices that every shipped artifact must carry: the project derives from
// Beszel, and internal/ghupdate from PocketBase's ghupdate package.
var upstreamCopyrights = []string{
	"Copyright (c) 2024 henrygd",
	"Copyright (c) 2022 - present, Gani Georgiev",
}

func TestLicenseCarriesUpstreamNotices(t *testing.T) {
	license, err := os.ReadFile("LICENSE")
	require.NoError(t, err)
	assert.Contains(t, string(license), "Copyright (c) 2026 Guillaume Archambault")
	for _, c := range upstreamCopyrights {
		assert.Contains(t, string(license), c)
	}
	assert.Contains(t, string(license), "The above copyright notice and this permission notice shall be included")
}

func TestDebianCopyrightCarriesUpstreamNotices(t *testing.T) {
	copyright, err := os.ReadFile("supplemental/debian/copyright")
	require.NoError(t, err)
	assert.Contains(t, string(copyright), "2024 henrygd")
	assert.Contains(t, string(copyright), "2022 - present, Gani Georgiev")
	assert.Contains(t, string(copyright), "Files: internal/ghupdate/*")
	assert.Contains(t, string(copyright), "Permission is hereby granted")
}

func TestHubImageShipsLicense(t *testing.T) {
	dockerfile, err := os.ReadFile("internal/dockerfile_hub")
	require.NoError(t, err)
	assert.Regexp(t, regexp.MustCompile(`(?m)^COPY (--\S+ )*LICENSE( \S+)* /usr/share/licenses/vigil/`), string(dockerfile))
	assert.Regexp(t, regexp.MustCompile(`(?m)^COPY .*THIRD_PARTY_NOTICE.* /usr/share/licenses/vigil/`), string(dockerfile))

	// The hub image is built from the repository root, so LICENSE must stay in the build context.
	ignore, err := os.Open(".dockerignore")
	require.NoError(t, err)
	defer func() { _ = ignore.Close() }() // read-only
	scanner := bufio.NewScanner(ignore)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		assert.False(t, strings.HasPrefix(line, "LICENSE") || strings.HasPrefix(line, "THIRD_PARTY") || line == "*",
			".dockerignore excludes a license file: %q", line)
	}
	require.NoError(t, scanner.Err())
}

func TestReleasePackagingShipsLicense(t *testing.T) {
	goreleaser, err := os.ReadFile(".goreleaser.yml")
	require.NoError(t, err)
	config := string(goreleaser)
	// Each archive lists its files explicitly (the defaults would drop THIRD_PARTY_NOTICES).
	archives := regexp.MustCompile(`(?ms)^archives:\n(.*?)^\S`).FindStringSubmatch(config)
	require.NotNil(t, archives, "no archives section")
	ids := strings.Count(archives[1], "  - id: ")
	assert.Equal(t, ids, strings.Count(archives[1], "      - LICENSE\n"), "every archive must ship LICENSE")
	assert.Equal(t, ids, strings.Count(archives[1], "      - THIRD_PARTY_NOTICES\n"), "every archive must ship THIRD_PARTY_NOTICES")
	assert.Contains(t, config, "src: ./supplemental/debian/copyright", "the .deb no longer installs the copyright file")
	assert.Contains(t, config, "src: ./THIRD_PARTY_NOTICES", "the .deb no longer installs THIRD_PARTY_NOTICES")
	assert.Contains(t, config, "- sh supplemental/scripts/third-party-notices.sh", "THIRD_PARTY_NOTICES is no longer generated")
}
