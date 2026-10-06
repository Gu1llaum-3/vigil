//go:build testing && linux

package collectors

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/Gu1llaum-3/vigil/internal/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseAptSourcesFile(t *testing.T) {
	content := `# Ubuntu main
deb https://archive.ubuntu.com/ubuntu jammy main restricted universe
deb http://security.ubuntu.com/ubuntu jammy-security main restricted
# deb-src https://archive.ubuntu.com/ubuntu jammy main
`
	f, err := os.CreateTemp("", "sources*.list")
	require.NoError(t, err)
	defer os.Remove(f.Name())
	_, err = f.WriteString(content)
	require.NoError(t, err)
	f.Close()

	repos, err := parseAptSourcesFile(f.Name())
	require.NoError(t, err)
	assert.Len(t, repos, 2, "should parse 2 deb lines (not deb-src, not commented)")

	assert.Equal(t, "archive.ubuntu.com", repos[0].Name)
	assert.True(t, repos[0].Secure, "https repo should be secure")
	assert.Equal(t, "jammy", repos[0].Distribution)

	assert.False(t, repos[1].Secure, "http repo should not be secure")
}

func TestRepoNameFromURL(t *testing.T) {
	tests := []struct {
		url      string
		expected string
	}{
		{"https://archive.ubuntu.com/ubuntu", "archive.ubuntu.com"},
		{"http://security.ubuntu.com/ubuntu", "security.ubuntu.com"},
	}
	for _, tt := range tests {
		assert.Equal(t, tt.expected, repoNameFromURL(tt.url))
	}
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, os.WriteFile(path, []byte(content), 0o644))
}

// Default /etc/apt/sources.list.d/debian.sources on Debian 13 (trixie).
const debianTrixieSources = `Types: deb
URIs: https://deb.debian.org/debian
Suites: trixie trixie-updates
Components: main non-free-firmware
Signed-By: /usr/share/keyrings/debian-archive-keyring.gpg

Types: deb
URIs: https://security.debian.org/debian-security
Suites: trixie-security
Components: main non-free-firmware
Signed-By: /usr/share/keyrings/debian-archive-keyring.gpg
`

func TestParseDeb822SourcesFile_DebianTrixieDefault(t *testing.T) {
	path := filepath.Join(t.TempDir(), "debian.sources")
	writeFile(t, path, debianTrixieSources)

	repos, err := parseDeb822SourcesFile(path)
	require.NoError(t, err)
	require.Len(t, repos, 3, "one entry per URI x suite")

	assert.Equal(t, common.RepositoryInfo{
		Name: "deb.debian.org", URL: "https://deb.debian.org/debian", Enabled: true, Secure: true,
		Distribution: "trixie", Components: "main non-free-firmware",
	}, repos[0])
	assert.Equal(t, "trixie-updates", repos[1].Distribution)
	assert.Equal(t, "https://deb.debian.org/debian", repos[1].URL)
	assert.Equal(t, "security.debian.org", repos[2].Name)
	assert.Equal(t, "trixie-security", repos[2].Distribution)
}

func TestParseDeb822SourcesFile_EdgeCases(t *testing.T) {
	content := `# Leading comment stanza is ignored

# source-only stanza: not a binary repo
Types: deb-src
URIs: http://deb.debian.org/debian
Suites: trixie
Components: main

types: deb deb-src
uris: http://mirror-a.example.org/debian https://mirror-b.example.org/debian
suites: bookworm
components: main contrib
enabled: no

Types: deb
URIs: https://download.docker.com/linux/debian
Suites: trixie
Components: stable
# a comment inside a stanza
Signed-By:
 -----BEGIN PGP PUBLIC KEY BLOCK-----
 .
 mQINBFit2ioBEADhWpZ8/wvZ6hUTiXOwQHXMAlaFHcPH9hAtr4F1y2+OYdbtMuth
 Types: deb
 -----END PGP PUBLIC KEY BLOCK-----

Types: deb
URIs: https://repo.example.com/flat
Suites: ./
Enabled: yes`
	path := filepath.Join(t.TempDir(), "mixed.sources")
	writeFile(t, path, content)

	repos, err := parseDeb822SourcesFile(path)
	require.NoError(t, err)
	require.Len(t, repos, 4)

	// Lower-case field names, multiple URIs, Enabled: no.
	assert.Equal(t, "http://mirror-a.example.org/debian", repos[0].URL)
	assert.False(t, repos[0].Secure)
	assert.False(t, repos[0].Enabled, "Enabled: no must be reported as disabled")
	assert.Equal(t, "bookworm", repos[0].Distribution)
	assert.Equal(t, "main contrib", repos[0].Components)
	assert.Equal(t, "mirror-b.example.org", repos[1].Name)
	assert.True(t, repos[1].Secure)
	assert.False(t, repos[1].Enabled)

	// Inline Signed-By continuation lines (even one that looks like a field) are not fields.
	assert.Equal(t, "download.docker.com", repos[2].Name)
	assert.Equal(t, "stable", repos[2].Components)
	assert.True(t, repos[2].Enabled)

	// Flat repository (exact path suite, no components), last stanza without trailing newline.
	assert.Equal(t, "./", repos[3].Distribution)
	assert.Equal(t, "", repos[3].Components)
	assert.True(t, repos[3].Enabled)
}

func TestCollectAptRepositories_MixesOneLineAndDeb822(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "sources.list"), "# moved to debian.sources\n")
	writeFile(t, filepath.Join(root, "sources.list.d", "debian.sources"), debianTrixieSources)
	writeFile(t, filepath.Join(root, "sources.list.d", "pgdg.list"),
		"deb [signed-by=/usr/share/keyrings/pgdg.gpg] https://apt.postgresql.org/pub/repos/apt trixie-pgdg main\n")

	repos, err := collectAptRepositories(root)
	require.NoError(t, err)

	var names []string
	for _, r := range repos {
		names = append(names, r.Name+" "+r.Distribution)
	}
	assert.ElementsMatch(t, []string{
		"deb.debian.org trixie", "deb.debian.org trixie-updates", "security.debian.org trixie-security",
		"apt.postgresql.org trixie-pgdg",
	}, names)
}
