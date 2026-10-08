//go:build testing

package migrations_test

import (
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"testing"

	_ "github.com/Gu1llaum-3/vigil/internal/migrations"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tests"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// legacyMigrations are the migration files named before the v1_ scheme, in the order
// PocketBase applies them (lexical by file name — not numeric: 10_ runs before 2_, 38_ before
// 3_). A migration that sorted among them would run before collections it may need on a fresh
// install, and silently do nothing there while working on upgraded hubs. Never rename, remove
// or add one: new migrations are named v1_NNNN_<what>.go, which sorts after all of these.
var legacyMigrations = []string{
	"0_collections_snapshot_0_19_0_dev_1.go",
	"10_create_scheduled_jobs.go",
	"11_add_ping_monitor.go",
	"12_add_ping_monitor_fields.go",
	"13_create_container_image_audits.go",
	"14_add_latest_image_id_to_container_image_audits.go",
	"15_add_notification_state_to_container_image_audits.go",
	"16_create_registry_credentials.go",
	"17_create_container_audit_overrides.go",
	"18_add_disabled_image_audit_status.go",
	"19_create_system_notifications.go",
	"20_add_tag_filters_to_container_audit_overrides.go",
	"21_create_host_metrics.go",
	"22_create_container_metrics.go",
	"23_add_monitor_events_composite_index.go",
	"24_add_metric_alert_columns.go",
	"25_create_metric_alerts.go",
	"26_add_alert_tiers.go",
	"27_hide_agent_token.go",
	"28_reset_loadavg_alerts.go",
	"29_add_metric_alert_duration.go",
	"2_create_host_snapshots.go",
	"30_add_image_audit_failure_tracking.go",
	"31_create_user_api_keys.go",
	"32_add_agent_tags.go",
	"33_create_notification_mutes.go",
	"34_create_maintenance.go",
	"35_add_host_metric_disk_mounts.go",
	"36_create_disk_monitor_rules.go",
	"37_add_host_metric_disk_max_mount.go",
	"38_agent_identity.go",
	"3_create_monitors.go",
	"4_add_monitor_failures.go",
	"5_create_notifications.go",
	"6_notification_in_app.go",
	"7_notification_rule_channels_multi.go",
	"8_add_monitor_inverted.go",
	"8_create_data_retention_settings.go",
	"9_add_monitor_event_maintenance.go",
	"9_add_monitor_ip_family.go",
	"9_hide_monitor_push_token.go",
	"initial-settings.go",
}

var newMigrationName = regexp.MustCompile(`^v1_(\d{4})_[a-z0-9_]+\.go$`)

// TestMigrationOrder checks the applied order (PocketBase sorts migrations by file name): the
// legacy files first, unchanged, then v1_0001_…, v1_0002_… with no gap or duplicate, so a new
// migration can only be added after every existing one.
func TestMigrationOrder(t *testing.T) {
	var registered []string
	for _, m := range core.AppMigrations.Items() {
		registered = append(registered, m.File)
	}

	require.GreaterOrEqual(t, len(registered), len(legacyMigrations))
	assert.Equal(t, legacyMigrations, registered[:len(legacyMigrations)],
		"a legacy migration was renamed or removed, or a new one sorts among them: name new migrations v1_NNNN_<what>.go")
	for i, name := range registered[len(legacyMigrations):] {
		match := newMigrationName.FindStringSubmatch(name)
		if !assert.NotNil(t, match, "%s: new migrations are named v1_NNNN_<what>.go so they sort after every legacy one", name) {
			continue
		}
		assert.Equal(t, fmt.Sprintf("%04d", i+1), match[1], "%s: v1_ migrations are numbered 0001, 0002, … with no gap or duplicate", name)
	}
}

// upgradeFixtures are databases created by released hubs (`migrate up` on an empty dir; see
// docs/conventions-and-gotchas.md to add one). keepsPocketBaseDefaults: created by a hub on an
// older PocketBase, whose auth defaults differ (see oldPocketBaseDefault).
var upgradeFixtures = []struct {
	file                    string
	keepsPocketBaseDefaults bool
}{
	{"data-v0.1.0.db.gz", true}, // PocketBase 0.37
	{"data-v0.2.16-beta.db.gz", false},
	{"data-v0.2.17-beta.db.gz", false},
}

// oldPocketBaseDefault matches the auth settings whose defaults PocketBase changed after 0.37
// (token lifetimes, MFA window, email templates) and deliberately keeps as they are on
// existing databases, since an admin may have tuned them. No Vigil migration sets them.
var oldPocketBaseDefault = regexp.MustCompile(`^(users|_superusers)\.definition\.(\w+Template\..*|\w+Token\.duration|mfa\.duration)$`)

// TestUpgradeMatchesFreshInstall migrates databases created by released hubs and checks they
// end up with exactly the schema of a fresh install: same collections, definitions and rules,
// fields (order and options), indexes and table columns. Not covered: data that migrations
// write (settings, records).
func TestUpgradeMatchesFreshInstall(t *testing.T) {
	fresh := schemaOf(t, t.TempDir())
	for _, fixture := range upgradeFixtures {
		t.Run(fixture.file, func(t *testing.T) {
			dir := t.TempDir()
			unpack(t, filepath.Join("testdata", fixture.file), filepath.Join(dir, "data.db"))
			upgraded := schemaOf(t, dir)
			var ignore *regexp.Regexp
			if fixture.keepsPocketBaseDefaults {
				ignore = oldPocketBaseDefault
			}
			for _, d := range schemaDiff(fresh, upgraded, ignore) {
				t.Errorf("upgraded schema differs from a fresh install: %s", d)
			}
		})
	}
}

func unpack(t *testing.T, src, dst string) {
	t.Helper()
	in, err := os.Open(src)
	require.NoError(t, err)
	defer in.Close()
	gz, err := gzip.NewReader(in)
	require.NoError(t, err)
	out, err := os.Create(dst)
	require.NoError(t, err)
	defer out.Close()
	_, err = io.Copy(out, gz)
	require.NoError(t, err)
}

// collectionSchema is what must match between a fresh and an upgraded database. Index order
// and timestamps are not compared.
type collectionSchema struct {
	Definition map[string]any
	FieldOrder []string
	Fields     map[string]any
	Indexes    []string
	Columns    []string
}

func schemaOf(t *testing.T, dataDir string) map[string]collectionSchema {
	t.Helper()
	app, err := tests.NewTestApp(dataDir) // clones dataDir, then applies pending migrations
	require.NoError(t, err)
	defer app.Cleanup()

	collections, err := app.FindAllCollections()
	require.NoError(t, err)
	schema := map[string]collectionSchema{}
	for _, c := range collections {
		raw, err := json.Marshal(c)
		require.NoError(t, err)
		var def map[string]any
		require.NoError(t, json.Unmarshal(raw, &def))
		fields := map[string]any{}
		var order []string
		for _, f := range def["fields"].([]any) {
			field := f.(map[string]any)
			fields[field["name"].(string)] = field
			order = append(order, field["name"].(string))
		}
		indexes := slices.Clone([]string(c.Indexes))
		sort.Strings(indexes)
		for _, k := range []string{"fields", "indexes", "created", "updated"} {
			delete(def, k)
		}

		var columns []string
		if !c.IsView() {
			var infos []struct {
				Name    string  `db:"name"`
				Type    string  `db:"type"`
				NotNull bool    `db:"notnull"`
				Default *string `db:"dflt_value"`
				PK      int     `db:"pk"`
			}
			require.NoError(t, app.DB().NewQuery("SELECT name, type, \"notnull\", dflt_value, pk FROM pragma_table_info({:table})").
				Bind(map[string]any{"table": c.Name}).All(&infos))
			for _, info := range infos {
				def := "NULL"
				if info.Default != nil {
					def = *info.Default
				}
				columns = append(columns, fmt.Sprintf("%s %s notnull=%t default=%s pk=%d", info.Name, info.Type, info.NotNull, def, info.PK))
			}
			sort.Strings(columns)
		}
		schema[c.Name] = collectionSchema{Definition: def, FieldOrder: order, Fields: fields, Indexes: indexes, Columns: columns}
	}
	return schema
}

// schemaDiff lists, path by path, where upgraded differs from fresh, except for the paths
// matching ignore (nil: none).
func schemaDiff(fresh, upgraded map[string]collectionSchema, ignore *regexp.Regexp) []string {
	flat := func(schema map[string]collectionSchema) map[string]string {
		out := map[string]string{}
		var walk func(path string, v any)
		walk = func(path string, v any) {
			switch v := v.(type) {
			case map[string]any:
				if len(v) == 0 {
					out[path] = "{}"
				}
				for k, child := range v {
					walk(path+"."+k, child)
				}
			default:
				b, _ := json.Marshal(v)
				out[path] = string(b)
			}
		}
		for name, c := range schema {
			walk(name+".definition", c.Definition)
			walk(name+".fieldOrder", c.FieldOrder)
			walk(name+".fields", c.Fields)
			walk(name+".indexes", c.Indexes)
			walk(name+".columns", c.Columns)
		}
		return out
	}
	want, got := flat(fresh), flat(upgraded)
	for path := range want {
		if ignore != nil && ignore.MatchString(path) {
			delete(want, path)
			delete(got, path)
		}
	}
	var diffs []string
	for path, w := range want {
		if g, ok := got[path]; !ok {
			diffs = append(diffs, path+": missing (fresh: "+w+")")
		} else if g != w {
			diffs = append(diffs, path+": "+g+" (fresh: "+w+")")
		}
	}
	for path, g := range got {
		if _, ok := want[path]; !ok {
			diffs = append(diffs, path+": "+g+" (absent from a fresh install)")
		}
	}
	sort.Strings(diffs)
	return diffs
}
