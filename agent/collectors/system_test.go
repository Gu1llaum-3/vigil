//go:build testing && linux

package collectors

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCollectOSInfo(t *testing.T) {
	// Write a temp os-release file
	content := `ID=ubuntu
ID_LIKE=debian
NAME="Ubuntu"
VERSION_ID="22.04"
`
	f, err := os.CreateTemp("", "os-release")
	require.NoError(t, err)
	defer os.Remove(f.Name())
	_, err = f.WriteString(content)
	require.NoError(t, err)
	f.Close()

	// We can't easily override /etc/os-release path, so test the parsing logic indirectly
	// by calling collectOSInfo which reads /etc/os-release on the real system.
	// On CI (Linux), this should return a valid OS family.
	info := collectOSInfo()
	assert.NotEmpty(t, info.Family)
}

func TestCollectUptime(t *testing.T) {
	uptime := collectUptime()
	// On Linux, /proc/uptime always exists and has a positive value
	assert.Greater(t, uptime, uint64(0))
}

func TestCollectResources(t *testing.T) {
	info := collectResources()
	assert.Greater(t, info.RAMMB, uint64(0), "RAM should be > 0")
	assert.Greater(t, info.CPUCores, 0, "CPU cores should be > 0")
}

func TestCollectPrimaryIP(t *testing.T) {
	ip := collectPrimaryIP()
	// May be empty in isolated environments, so just verify format if set
	if ip != "" {
		assert.Contains(t, ip, ".")
	}
}

func TestCollectGateway(t *testing.T) {
	// Gateway may not exist in all environments; just ensure no panic
	_ = collectGateway()
}

func TestCollectDNSServers(t *testing.T) {
	// /etc/resolv.conf may not have nameserver entries in all environments
	_ = collectDNSServers()
}

func TestCollectKernel(t *testing.T) {
	kernel := collectKernel()
	assert.NotEmpty(t, kernel)
}
