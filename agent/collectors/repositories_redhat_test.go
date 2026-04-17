//go:build testing && linux

package collectors

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseRepoFile(t *testing.T) {
	content := `[base]
name=CentOS-$releasever - Base
baseurl=https://mirror.centos.org/centos/$releasever/os/$basearch/
enabled=1

[updates]
name=CentOS-$releasever - Updates
baseurl=http://mirror.centos.org/centos/$releasever/updates/$basearch/
enabled=1

[extras]
name=CentOS-$releasever - Extras
baseurl=https://mirror.centos.org/centos/$releasever/extras/$basearch/
enabled=0
`
	f, err := os.CreateTemp("", "centos*.repo")
	require.NoError(t, err)
	defer os.Remove(f.Name())
	_, err = f.WriteString(content)
	require.NoError(t, err)
	f.Close()

	repos, err := parseRepoFile(f.Name())
	require.NoError(t, err)
	assert.Len(t, repos, 3)

	assert.Equal(t, "CentOS-$releasever - Base", repos[0].Name)
	assert.True(t, repos[0].Enabled)
	assert.True(t, repos[0].Secure)

	assert.False(t, repos[1].Secure, "http repo should not be secure")

	assert.False(t, repos[2].Enabled, "disabled repo should be false")
}
