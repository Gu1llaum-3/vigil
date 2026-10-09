//go:build testing

package hub

import (
	"compress/gzip"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/pocketbase/pocketbase/core"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// bootstrapHubFrom bootstraps a hub on dataDir as the serve command does, before migrations.
func bootstrapHubFrom(t *testing.T, dataDir string) core.App {
	t.Helper()
	app := core.NewBaseApp(core.BaseAppConfig{DataDir: dataDir})
	NewHub(app).BackupBeforeMigrations(func() bool { return true })
	require.NoError(t, app.Bootstrap())
	t.Cleanup(func() { _ = app.ClearBootstrap() })
	return app
}

func preUpdateBackups(t *testing.T, app core.App) []string {
	t.Helper()
	fsys, err := app.NewBackupsFilesystem()
	require.NoError(t, err)
	defer fsys.Close()
	files, err := fsys.List(preUpdateBackupPrefix)
	require.NoError(t, err)
	var names []string
	for _, f := range files {
		names = append(names, f.Key)
	}
	return names
}

func unpackFixture(t *testing.T, name, dataDir string) {
	t.Helper()
	in, err := os.Open(filepath.Join("..", "migrations", "testdata", name))
	require.NoError(t, err)
	defer in.Close()
	gz, err := gzip.NewReader(in)
	require.NoError(t, err)
	out, err := os.Create(filepath.Join(dataDir, "data.db"))
	require.NoError(t, err)
	defer out.Close()
	_, err = io.Copy(out, gz)
	require.NoError(t, err)
}

// Starting a newer hub on an existing database backs it up before the new migrations run.
func TestPreUpdateBackupBeforePendingMigrations(t *testing.T) {
	dir := t.TempDir()
	unpackFixture(t, "data-v0.2.17-beta.db.gz", dir)
	app := bootstrapHubFrom(t, dir)

	backups := preUpdateBackups(t, app)
	require.Len(t, backups, 1, "migrations are pending: one backup")
	assert.True(t, strings.HasSuffix(backups[0], ".zip"))

	require.NoError(t, app.RunAllMigrations())
	require.NoError(t, backupBeforePendingMigrations(app))
	assert.Len(t, preUpdateBackups(t, app), 1, "nothing pending: no new backup")
}

func TestPreUpdateBackupSkipped(t *testing.T) {
	t.Run("fresh install", func(t *testing.T) {
		app := bootstrapHubFrom(t, t.TempDir())
		assert.Empty(t, preUpdateBackups(t, app), "nothing to protect")
	})
	t.Run("disabled", func(t *testing.T) {
		t.Setenv("PRE_UPDATE_BACKUP", "false")
		dir := t.TempDir()
		unpackFixture(t, "data-v0.2.17-beta.db.gz", dir)
		app := bootstrapHubFrom(t, dir)
		assert.Empty(t, preUpdateBackups(t, app))
	})
}

func TestPreUpdateBackupsArePruned(t *testing.T) {
	dir := t.TempDir()
	unpackFixture(t, "data-v0.2.17-beta.db.gz", dir)
	app := bootstrapHubFrom(t, dir)
	for i := 0; i < 4; i++ {
		require.NoError(t, createPreUpdateBackup(app))
	}
	backups := preUpdateBackups(t, app)
	assert.Len(t, backups, preUpdateBackupsKept, "only the most recent ones are kept")
}

// A backup that cannot be written stops the start instead of migrating without one.
func TestPreUpdateBackupFailureStopsTheStart(t *testing.T) {
	dir := t.TempDir()
	unpackFixture(t, "data-v0.2.17-beta.db.gz", dir)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "backups"), nil, 0o600))
	app := core.NewBaseApp(core.BaseAppConfig{DataDir: dir})
	NewHub(app).BackupBeforeMigrations(func() bool { return true })
	t.Cleanup(func() { _ = app.ClearBootstrap() })
	err := app.Bootstrap()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "PRE_UPDATE_BACKUP=false")
}

func TestPreUpdateBackupOrder(t *testing.T) {
	keys := []string{
		"pre_update_0_3_0_20261009_100000_000001.zip",
		"pre_update_0_2_17-beta_20261009_120000_000000.zip",
		"pre_update_1_0_0_20261008_090000_000000.zip",
	}
	assert.Greater(t, backupTime(keys[1]), backupTime(keys[0]), "the time decides, not the version")
	assert.Greater(t, backupTime(keys[0]), backupTime(keys[2]))
}

// Only the commands that migrate (serve, migrate up) ask for the backup.
func TestPreUpdateBackupOnlyWhenAsked(t *testing.T) {
	dir := t.TempDir()
	unpackFixture(t, "data-v0.2.17-beta.db.gz", dir)
	app := core.NewBaseApp(core.BaseAppConfig{DataDir: dir})
	NewHub(app).BackupBeforeMigrations(func() bool { return false })
	require.NoError(t, app.Bootstrap())
	t.Cleanup(func() { _ = app.ClearBootstrap() })
	assert.Empty(t, preUpdateBackups(t, app))
}
