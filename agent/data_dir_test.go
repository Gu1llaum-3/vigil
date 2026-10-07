//go:build testing

package agent

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	app "github.com/Gu1llaum-3/vigil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// uncreatableDir returns a path no user can create, root included: its parent is a
// regular file. (A path like /invalid/path is creatable by root, e.g. in a CI container.)
func uncreatableDir(t *testing.T, name string) string {
	t.Helper()
	file := filepath.Join(t.TempDir(), "regular-file")
	require.NoError(t, os.WriteFile(file, nil, 0o644))
	return filepath.Join(file, name)
}

func TestGetDataDir(t *testing.T) {
	// Test with explicit dataDir parameter
	t.Run("explicit data dir", func(t *testing.T) {
		tempDir := t.TempDir()
		result, err := GetDataDir(tempDir)
		require.NoError(t, err)
		assert.Equal(t, tempDir, result)
	})

	// Test with explicit non-existent dataDir that can be created
	t.Run("explicit data dir - create new", func(t *testing.T) {
		tempDir := t.TempDir()
		newDir := filepath.Join(tempDir, "new-data-dir")
		result, err := GetDataDir(newDir)
		require.NoError(t, err)
		assert.Equal(t, newDir, result)

		// Verify directory was created
		stat, err := os.Stat(newDir)
		require.NoError(t, err)
		assert.True(t, stat.IsDir())
	})

	// Test with DATA_DIR environment variable
	t.Run("DATA_DIR environment variable", func(t *testing.T) {
		tempDir := t.TempDir()

		t.Setenv(app.AgentEnvPrefix+"DATA_DIR", tempDir)

		result, err := GetDataDir()
		require.NoError(t, err)
		assert.Equal(t, tempDir, result)
	})

	// Test with invalid explicit dataDir
	t.Run("invalid explicit data dir", func(t *testing.T) {
		_, err := GetDataDir(uncreatableDir(t, "data"))
		assert.Error(t, err)
	})

	// Test fallback behavior (empty dataDir, no env var): creating /var/lib/vigil-agent
	// fails for a regular user, so the agent falls back to ~/.config.
	t.Run("fallback to the user config directory", func(t *testing.T) {
		if runtime.GOOS == "windows" {
			t.Skip("Windows uses app-data directories")
		}
		if os.Geteuid() == 0 {
			t.Skip("as root the fallback would create the real /var/lib data directory")
		}
		if _, err := os.Stat(filepath.Join("/var/lib", app.AgentDataDirName)); err == nil {
			t.Skip("the system data directory exists on this host")
		}
		home := t.TempDir()
		t.Setenv("HOME", home)
		t.Setenv(app.AgentEnvPrefix+"DATA_DIR", "")
		t.Setenv("DATA_DIR", "")

		result, err := GetDataDir()
		require.NoError(t, err)
		assert.Equal(t, filepath.Join(home, ".config", app.AgentConfigDirName), result)
	})
}

func TestTestDataDirs(t *testing.T) {
	// Test with existing valid directory
	t.Run("existing valid directory", func(t *testing.T) {
		tempDir := t.TempDir()
		result, err := testDataDirs([]string{tempDir})
		require.NoError(t, err)
		assert.Equal(t, tempDir, result)
	})

	// Test with multiple directories, first one valid
	t.Run("multiple dirs - first valid", func(t *testing.T) {
		tempDir := t.TempDir()
		invalidDir := uncreatableDir(t, "data")
		result, err := testDataDirs([]string{tempDir, invalidDir})
		require.NoError(t, err)
		assert.Equal(t, tempDir, result)
	})

	// Test with multiple directories, second one valid
	t.Run("multiple dirs - second valid", func(t *testing.T) {
		tempDir := t.TempDir()
		invalidDir := uncreatableDir(t, "data")
		result, err := testDataDirs([]string{invalidDir, tempDir})
		require.NoError(t, err)
		assert.Equal(t, tempDir, result)
	})

	// Test with non-existing directory that can be created
	t.Run("create new directory", func(t *testing.T) {
		tempDir := t.TempDir()
		newDir := filepath.Join(tempDir, "new-dir")
		result, err := testDataDirs([]string{newDir})
		require.NoError(t, err)
		assert.Equal(t, newDir, result)

		// Verify directory was created
		stat, err := os.Stat(newDir)
		require.NoError(t, err)
		assert.True(t, stat.IsDir())
	})

	// Test with no valid directories
	t.Run("no valid directories", func(t *testing.T) {
		invalidPaths := []string{uncreatableDir(t, "data1"), uncreatableDir(t, "data2")}
		_, err := testDataDirs(invalidPaths)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "data directory not found")
	})
}

func TestIsValidDataDir(t *testing.T) {
	// Test with existing directory
	t.Run("existing directory", func(t *testing.T) {
		tempDir := t.TempDir()
		valid, err := isValidDataDir(tempDir, false)
		require.NoError(t, err)
		assert.True(t, valid)
	})

	// Test with non-existing directory, createIfNotExists=false
	t.Run("non-existing dir - no create", func(t *testing.T) {
		tempDir := t.TempDir()
		nonExistentDir := filepath.Join(tempDir, "does-not-exist")
		valid, err := isValidDataDir(nonExistentDir, false)
		require.NoError(t, err)
		assert.False(t, valid)
	})

	// Test with non-existing directory, createIfNotExists=true
	t.Run("non-existing dir - create", func(t *testing.T) {
		tempDir := t.TempDir()
		newDir := filepath.Join(tempDir, "new-dir")
		valid, err := isValidDataDir(newDir, true)
		require.NoError(t, err)
		assert.True(t, valid)

		// Verify directory was created
		stat, err := os.Stat(newDir)
		require.NoError(t, err)
		assert.True(t, stat.IsDir())
	})

	// Test with file instead of directory
	t.Run("file instead of directory", func(t *testing.T) {
		tempDir := t.TempDir()
		tempFile := filepath.Join(tempDir, "testfile")
		err := os.WriteFile(tempFile, []byte("test"), 0644)
		require.NoError(t, err)

		valid, err := isValidDataDir(tempFile, false)
		require.Error(t, err)
		assert.False(t, valid)
		assert.Contains(t, err.Error(), "is not a directory")
	})
}

func TestDirectoryExists(t *testing.T) {
	// Test with existing directory
	t.Run("existing directory", func(t *testing.T) {
		tempDir := t.TempDir()
		exists, err := directoryExists(tempDir)
		require.NoError(t, err)
		assert.True(t, exists)
	})

	// Test with non-existing directory
	t.Run("non-existing directory", func(t *testing.T) {
		tempDir := t.TempDir()
		nonExistentDir := filepath.Join(tempDir, "does-not-exist")
		exists, err := directoryExists(nonExistentDir)
		require.NoError(t, err)
		assert.False(t, exists)
	})

	// Test with file instead of directory
	t.Run("file instead of directory", func(t *testing.T) {
		tempDir := t.TempDir()
		tempFile := filepath.Join(tempDir, "testfile")
		err := os.WriteFile(tempFile, []byte("test"), 0644)
		require.NoError(t, err)

		exists, err := directoryExists(tempFile)
		require.Error(t, err)
		assert.False(t, exists)
		assert.Contains(t, err.Error(), "is not a directory")
	})
}

func TestDirectoryIsWritable(t *testing.T) {
	// Test with writable directory
	t.Run("writable directory", func(t *testing.T) {
		tempDir := t.TempDir()
		writable, err := directoryIsWritable(tempDir)
		require.NoError(t, err)
		assert.True(t, writable)
	})

	// Test with non-existing directory
	t.Run("non-existing directory", func(t *testing.T) {
		tempDir := t.TempDir()
		nonExistentDir := filepath.Join(tempDir, "does-not-exist")
		writable, err := directoryIsWritable(nonExistentDir)
		assert.Error(t, err)
		assert.False(t, writable)
	})

	// Test with non-writable directory (Unix-like systems only)
	t.Run("non-writable directory", func(t *testing.T) {
		if runtime.GOOS != "linux" && runtime.GOOS != "darwin" {
			t.Skip("Skipping non-writable directory test on", runtime.GOOS)
		}
		if os.Geteuid() == 0 {
			t.Skip("root bypasses directory permissions")
		}

		tempDir := t.TempDir()
		readOnlyDir := filepath.Join(tempDir, "readonly")

		// Create the directory
		err := os.Mkdir(readOnlyDir, 0755)
		require.NoError(t, err)

		// Make it read-only
		err = os.Chmod(readOnlyDir, 0444)
		require.NoError(t, err)

		// Restore permissions after test for cleanup
		defer func() {
			os.Chmod(readOnlyDir, 0755)
		}()

		writable, err := directoryIsWritable(readOnlyDir)
		assert.Error(t, err)
		assert.False(t, writable)
	})
}
