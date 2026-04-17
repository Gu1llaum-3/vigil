//go:build testing && linux

package collectors

import (
	"os"
	"testing"

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
