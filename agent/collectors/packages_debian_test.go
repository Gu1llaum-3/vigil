//go:build testing && linux

package collectors

import (
	"context"
	"os"
	"testing"

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

func TestAptOutdatedPackagesGraceful(t *testing.T) {
	// Verify the function handles environments without apt gracefully
	result, _ := aptOutdatedPackages(context.Background())
	assert.NotNil(t, result)
}

func TestDpkgInstalledCount(t *testing.T) {
	count, err := dpkgInstalledCount(context.Background())
	if err != nil {
		t.Skip("dpkg not available")
	}
	assert.GreaterOrEqual(t, count, 0)
}
