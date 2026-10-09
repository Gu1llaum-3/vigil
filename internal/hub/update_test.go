//go:build testing

package hub

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

// fakeServices replaces the system calls of restartService; running lists the
// "tool name" pairs whose status succeeds, and the calls are recorded.
func fakeServices(t *testing.T, goos string, euid int, tools []string, running ...string) *[]string {
	t.Helper()
	calls := &[]string{}
	saved := []any{serviceGOOS, serviceGeteuid, serviceLookPath, serviceRun}
	t.Cleanup(func() {
		serviceGOOS = saved[0].(string)
		serviceGeteuid = saved[1].(func() int)
		serviceLookPath = saved[2].(func(string) (string, error))
		serviceRun = saved[3].(func(string, ...string) error)
	})
	serviceGOOS = goos
	serviceGeteuid = func() int { return euid }
	serviceLookPath = func(tool string) (string, error) {
		for _, t := range tools {
			if t == tool {
				return "/usr/bin/" + tool, nil
			}
		}
		return "", errors.New("not found")
	}
	serviceRun = func(name string, args ...string) error {
		call := name + " " + strings.Join(args, " ")
		*calls = append(*calls, call)
		for _, r := range running {
			if strings.HasPrefix(call, r) && (strings.Contains(call, "status") || strings.Contains(call, "is-active")) {
				return nil
			}
		}
		if strings.Contains(call, "restart") {
			return nil
		}
		return errors.New("inactive")
	}
	return calls
}

func TestRestartServiceUnprivileged(t *testing.T) {
	t.Run("systemd leaves the marker", func(t *testing.T) {
		dir := t.TempDir()
		calls := fakeServices(t, "linux", 1000, []string{"systemctl"})
		restartService(dir)
		assert.FileExists(t, filepath.Join(dir, RestartPendingFile))
		assert.Empty(t, *calls, "no restart attempted without root")
	})
	t.Run("no systemd, no marker", func(t *testing.T) {
		for _, goos := range []string{"linux", "darwin", "freebsd"} {
			dir := t.TempDir()
			tools := []string{"service"}
			if goos == "darwin" {
				tools = nil
			}
			fakeServices(t, goos, 1000, tools)
			restartService(dir)
			_, err := os.Stat(filepath.Join(dir, RestartPendingFile))
			assert.True(t, os.IsNotExist(err), goos)
		}
	})
}

func TestRestartServiceAsRoot(t *testing.T) {
	t.Run("vigil-hub under systemd", func(t *testing.T) {
		dir := t.TempDir()
		calls := fakeServices(t, "linux", 0, []string{"systemctl"}, "systemctl is-active --quiet vigil-hub")
		restartService(dir)
		assert.Contains(t, *calls, "systemctl restart vigil-hub.service")
		assert.NoFileExists(t, filepath.Join(dir, RestartPendingFile))
	})
	t.Run("older vigil service", func(t *testing.T) {
		calls := fakeServices(t, "linux", 0, []string{"systemctl"}, "systemctl is-active --quiet vigil.service")
		restartService(t.TempDir())
		assert.Contains(t, *calls, "systemctl restart vigil.service")
		assert.NotContains(t, *calls, "systemctl restart vigil-hub.service")
	})
	t.Run("FreeBSD rc", func(t *testing.T) {
		calls := fakeServices(t, "freebsd", 0, []string{"service"}, "service vigil-hub status")
		restartService(t.TempDir())
		assert.Contains(t, *calls, "service vigil-hub restart")
	})
	t.Run("no service running", func(t *testing.T) {
		calls := fakeServices(t, "linux", 0, []string{"systemctl"})
		restartService(t.TempDir())
		for _, call := range *calls {
			assert.NotContains(t, call, "restart")
		}
	})
}
