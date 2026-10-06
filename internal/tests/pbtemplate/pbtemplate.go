//go:build testing

// Package pbtemplate starts test apps from a data dir migrated once per test binary.
//
// pocketbase's tests.NewTestApp clones the data dir it is given, then runs the pending
// migrations; starting every test from an empty dir replays the whole migration chain
// each time, which dominates the hub suite's duration (by far under -race). Starting
// from a migrated dir costs a file copy instead. The package only depends on the root
// package and pocketbase, so the hub's in-package tests can import it too.
package pbtemplate

import (
	"os"

	appmeta "github.com/Gu1llaum-3/vigil"
	"github.com/pocketbase/pocketbase/tests"
)

// migratedDir is the shared migrated data dir, set by Run.
var migratedDir string

// migrationEnvVars are the variables some migrations read while they run (the initial
// superuser in internal/migrations/initial-settings.go). The shared dir was migrated
// without them, so a test that sets one must migrate from scratch. Keep this list in
// sync with the migrations.
var migrationEnvVars = []string{"USER_EMAIL", "USER_PASSWORD"}

// Run builds the migrated data dir, runs the tests and removes it. Call it from TestMain:
//
//	func TestMain(m *testing.M) { os.Exit(pbtemplate.Run(m.Run)) }
//
// If building the dir fails, tests migrate from scratch.
func Run(run func() int) int {
	empty, err := os.MkdirTemp("", "vigil_test_empty_*")
	if err != nil {
		return run()
	}
	defer os.RemoveAll(empty)

	tmpl, err := tests.NewTestApp(empty)
	if err != nil {
		return run()
	}
	// The clone lives in its own temp dir; remove it whatever happens next.
	defer os.RemoveAll(tmpl.DataDir())
	// Close the databases (checkpointing SQLite) but keep the migrated files on disk.
	if err := tmpl.ResetBootstrapState(); err != nil {
		return run()
	}

	migratedDir = tmpl.DataDir()
	defer func() { migratedDir = "" }()
	return run()
}

// DataDirFor returns the data dir a test app should be cloned from: the shared migrated
// dir in place of an existing empty dir (typically t.TempDir()), unless Run is not active
// or a test set a variable that a migration reads.
func DataDirFor(dir string) string {
	if migratedDir == "" || dir == "" || migrationEnvSet() {
		return dir
	}
	if entries, err := os.ReadDir(dir); err != nil || len(entries) > 0 {
		return dir
	}
	return migratedDir
}

// migrationEnvSet reports whether a migration-sensitive variable is set, under any of
// the names the migrations read (VIGIL_HUB_, legacy APP_HUB_, or no prefix).
func migrationEnvSet() bool {
	for _, name := range migrationEnvVars {
		for _, k := range []string{appmeta.HubEnvPrefix + name, "APP_HUB_" + name, name} {
			if v, ok := os.LookupEnv(k); ok && v != "" {
				return true
			}
		}
	}
	return false
}
