//go:build testing

package ghupdate

import "testing"

// TestArchiveSuffix locks the asset names to what .goreleaser.yml actually publishes,
// independently of the libc of the machine running the update.
func TestArchiveSuffix(t *testing.T) {
	cases := []struct {
		binary, goos, goarch, want string
	}{
		{"vigil-agent", "linux", "amd64", "vigil-agent_linux_amd64.tar.gz"},
		{"vigil-agent", "linux", "arm64", "vigil-agent_linux_arm64.tar.gz"},
		{"vigil", "linux", "amd64", "vigil_linux_amd64.tar.gz"},
		{"vigil", "linux", "arm", "vigil_linux_arm.tar.gz"},
		{"vigil", "darwin", "arm64", "vigil_darwin_arm64.tar.gz"},
		{"vigil", "windows", "amd64", "vigil_windows_amd64.zip"},
	}
	for _, c := range cases {
		if got := archiveSuffix(c.binary, c.goos, c.goarch); got != c.want {
			t.Errorf("archiveSuffix(%q, %q, %q) = %q, want %q", c.binary, c.goos, c.goarch, got, c.want)
		}
	}
}
