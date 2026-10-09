package hub

import (
	"context"
	"fmt"
	"log/slog"
	"regexp"
	"sort"
	"strings"
	"time"

	app "github.com/Gu1llaum-3/vigil"
	"github.com/Gu1llaum-3/vigil/internal/hub/utils"
	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// A hub started on a database that has Vigil migrations pending (a newer binary, after an
// update) backs the data directory up first, with PocketBase's own backups
// (<data dir>/backups, listed and restorable from the PocketBase dashboard), so a Vigil
// schema change has a restore point. PocketBase's own system migrations run inside its
// bootstrap, before this can: a PocketBase upgrade that ships some is not covered.
// PRE_UPDATE_BACKUP=false skips it.
const (
	preUpdateBackupPrefix = "pre_update_"
	preUpdateBackupsKept  = 3
)

var backupNameUnsafe = regexp.MustCompile(`[^a-z0-9_-]+`)

// BackupBeforeMigrations backs up right after the bootstrap, before the command runs, when
// migrating reports that this command applies the migrations (serve, migrate up); other
// commands (superuser, ...) do not back up.
func (h *Hub) BackupBeforeMigrations(migrating func() bool) {
	if h.IsBootstrapped() {
		return
	}
	h.OnBootstrap().BindFunc(func(e *core.BootstrapEvent) error {
		if err := e.Next(); err != nil || !migrating() {
			return err
		}
		slog.Info("Starting Vigil hub", "version", app.Version)
		return backupBeforePendingMigrations(e.App)
	})
}

// backupBeforePendingMigrations backs the data directory up when migrations are pending. A failed backup stops the start: migrating without a restore
// point is what it exists to prevent.
func backupBeforePendingMigrations(app core.App) error {
	if raw, ok := utils.GetEnv("PRE_UPDATE_BACKUP"); ok && strings.EqualFold(strings.TrimSpace(raw), "false") {
		return nil
	}
	pending, err := pendingMigrations(app)
	if err != nil || len(pending) == 0 {
		return err
	}
	slog.Info("Migrations pending: backing up the data directory first", "migrations", len(pending))
	if err := createPreUpdateBackup(app); err != nil {
		return fmt.Errorf("backup before migrations failed (%w): free disk space (the backup needs about twice the data directory), or set PRE_UPDATE_BACKUP=false to start without one", err)
	}
	return nil
}

// pendingMigrations lists the Vigil migrations a database that has already been
// migrated by Vigil has not applied yet. A fresh install has nothing to protect.
func pendingMigrations(app core.App) ([]string, error) {
	var tables int
	if err := app.DB().NewQuery("SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = {:name}").
		Bind(dbx.Params{"name": core.DefaultMigrationsTable}).Row(&tables); err != nil || tables == 0 {
		return nil, err
	}
	var applied []string
	if err := app.DB().Select("file").From(core.DefaultMigrationsTable).Column(&applied); err != nil {
		return nil, err
	}
	done := make(map[string]bool, len(applied))
	for _, f := range applied {
		done[f] = true
	}
	// No Vigil migration applied yet: a fresh install (PocketBase may have created the
	// table and applied its own migrations on bootstrap).
	fresh := true
	for _, m := range core.AppMigrations.Items() {
		if done[m.File] {
			fresh = false
			break
		}
	}
	if fresh {
		return nil, nil
	}
	var pending []string
	for _, m := range core.AppMigrations.Items() {
		if !done[m.File] {
			pending = append(pending, m.File)
		}
	}
	return pending, nil
}

// createPreUpdateBackup writes pre_update_<version>_<time>.zip and keeps the most recent
// preUpdateBackupsKept of them.
func createPreUpdateBackup(pb core.App) error {
	version := backupNameUnsafe.ReplaceAllString(strings.ToLower(app.Version), "_")
	now := time.Now().UTC()
	name := fmt.Sprintf("%s%s_%s_%06d.zip", preUpdateBackupPrefix, version, now.Format("20060102_150405"), now.Nanosecond()/1000)
	if err := pb.CreateBackup(context.Background(), name); err != nil {
		return err
	}
	slog.Info("Backup created before migrations", "backup", name)

	fsys, err := pb.NewBackupsFilesystem()
	if err != nil {
		return nil // the backup exists; pruning is best effort
	}
	defer func() { _ = fsys.Close() }()
	files, err := fsys.List(preUpdateBackupPrefix)
	if err != nil {
		return nil
	}
	// The time in the name sorts them, whatever the version.
	sort.Slice(files, func(i, j int) bool { return backupTime(files[i].Key) > backupTime(files[j].Key) })
	for _, old := range files[min(len(files), preUpdateBackupsKept):] {
		if err := fsys.Delete(old.Key); err != nil {
			slog.Warn("Cannot remove an old pre-update backup", "backup", old.Key, "err", err)
		}
	}
	return nil
}

// backupTime returns the <YYYYMMDD_HHMMSS>_<micro> tail of a pre-update backup name.
func backupTime(key string) string {
	name := strings.TrimSuffix(key, ".zip")
	if len(name) < len("20060102_150405_000000") {
		return name
	}
	return name[len(name)-len("20060102_150405_000000"):]
}
