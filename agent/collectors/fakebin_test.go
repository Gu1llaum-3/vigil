//go:build testing && linux

package collectors

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// fakeCommand installs an executable named name at the front of PATH. When invoked with
// exactly args, it prints the fixture file and exits with exitCode; with any other
// arguments it fails, so an unexpected invocation shows up in the test.
func fakeCommand(t *testing.T, name, args, fixture string, exitCode int) {
	t.Helper()
	dir := fakeBinDir(t)
	fixturePath, err := filepath.Abs(filepath.Join("testdata", fixture))
	require.NoError(t, err)
	script := fmt.Sprintf("#!/bin/sh\nif [ \"$*\" = %q ]; then cat %q; exit %d; fi\necho \"unexpected: %s $*\" >&2\nexit 99\n",
		args, fixturePath, exitCode, name)
	require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte(script), 0o755))
}

// fakeBinDir returns a fresh directory put at the front of PATH, so it shadows a real
// apt-get or dnf on the host while /bin/sh and cat stay reachable.
func fakeBinDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	return dir
}

// withoutCommand makes sure name cannot be found in PATH.
func withoutCommand(t *testing.T) {
	t.Helper()
	t.Setenv("PATH", t.TempDir())
}
