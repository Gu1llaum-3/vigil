//go:build testing

package hub

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/pocketbase/pocketbase/core"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type purgeTestEnv struct {
	hub     *Hub
	app     core.App
	agent   *core.Record
	monitor *core.Record
}

func newPurgeTestEnv(t *testing.T) purgeTestEnv {
	t.Helper()
	hub, testApp, err := createTestHub(t)
	require.NoError(t, err)
	t.Cleanup(func() { cleanupTestHub(hub, testApp) })
	agent, err := createTestRecord(hub, "agents", map[string]any{"name": "a", "token": "t", "status": "connected"})
	require.NoError(t, err)
	monitor, err := createTestRecord(hub, "monitors", map[string]any{"name": "m", "type": "http", "url": "https://example.com", "interval": 60})
	require.NoError(t, err)
	return purgeTestEnv{hub: hub, app: testApp, agent: agent, monitor: monitor}
}

// row creates a row of collection dated at, with the fields that collection requires.
func (env purgeTestEnv) row(t *testing.T, collection string, at time.Time) string {
	t.Helper()
	var fields map[string]any
	switch collection {
	case "monitor_events":
		fields = map[string]any{"monitor": env.monitor.Id, "status": 1, "checked_at": at}
	case "notification_logs":
		fields = map[string]any{"event_kind": "monitor.down", "status": "sent", "sent_at": at}
	case "system_notifications":
		fields = map[string]any{"event_kind": "monitor.down", "category": "monitors", "severity": "critical", "title": "x", "occurred_at": at}
	case "host_metric_samples":
		fields = map[string]any{"agent": env.agent.Id, "collected_at": at}
	case "container_metric_samples":
		fields = map[string]any{"agent": env.agent.Id, "collected_at": at}
	default:
		t.Fatalf("no fixture for %s", collection)
	}
	rec, err := createTestRecord(env.hub, collection, fields)
	require.NoError(t, err, collection)
	return rec.Id
}

func (env purgeTestEnv) exists(collection, id string) bool {
	_, err := env.hub.FindRecordById(collection, id)
	return err == nil
}

// Each age-based purge deletes exactly the rows older than the cutoff, and refuses a
// non-positive number of days instead of deleting everything.
func TestPurgeOlderThanCutoff(t *testing.T) {
	env := newPurgeTestEnv(t)
	purges := map[string]func(int) (int, error){
		"monitor_events":           env.hub.purgeMonitorEventsOlderThan,
		"notification_logs":        env.hub.purgeNotificationLogsOlderThan,
		"system_notifications":     env.hub.purgeSystemNotificationsOlderThan,
		"host_metric_samples":      env.hub.purgeHostMetricSamplesOlderThan,
		"container_metric_samples": env.hub.purgeContainerMetricSamplesOlderThan,
	}
	for _, target := range retentionPurgeTargets {
		require.Contains(t, purges, target.table, "every purge target is covered")
	}
	require.Len(t, purges, len(retentionPurgeTargets))
	now := time.Now().UTC()
	for collection, purge := range purges {
		t.Run(collection, func(t *testing.T) {
			// One minute either side of the cutoff: a cutoff in another date format (e.g. with
			// a "T") misorders rows of the cutoff's own day, which a wider margin can miss.
			old := env.row(t, collection, now.AddDate(0, 0, -30).Add(-time.Minute))
			older := env.row(t, collection, now.AddDate(0, 0, -40))
			recent := env.row(t, collection, now.AddDate(0, 0, -30).Add(time.Minute))
			fresh := env.row(t, collection, now.AddDate(0, 0, -10))

			for _, days := range []int{0, -1} {
				_, err := purge(days)
				assert.Error(t, err, "days=%d", days)
			}
			assert.True(t, env.exists(collection, old), "a refused purge deletes nothing")

			deleted, err := purge(30)
			require.NoError(t, err)
			assert.Equal(t, 2, deleted)
			assert.False(t, env.exists(collection, old))
			assert.False(t, env.exists(collection, older))
			assert.True(t, env.exists(collection, recent))
			assert.True(t, env.exists(collection, fresh))
		})
	}
}

func TestPurgeAll(t *testing.T) {
	env := newPurgeTestEnv(t)
	now := time.Now().UTC()
	for _, c := range []struct {
		collection string
		purge      func() (int, error)
	}{
		{"monitor_events", env.hub.purgeAllMonitorEvents},
		{"notification_logs", env.hub.purgeAllNotificationLogs},
	} {
		env.row(t, c.collection, now)
		env.row(t, c.collection, now.AddDate(-1, 0, 0))
		deleted, err := c.purge()
		require.NoError(t, err)
		assert.Equal(t, 2, deleted, c.collection)
		left, err := env.hub.FindRecordsByFilter(c.collection, "", "", 0, 0)
		require.NoError(t, err)
		assert.Empty(t, left, c.collection)
	}
	// The other tables are untouched.
	sample := env.row(t, "host_metric_samples", now)
	_, err := env.hub.purgeAllMonitorEvents()
	require.NoError(t, err)
	assert.True(t, env.exists("host_metric_samples", sample))
}

func TestPurgeOfflineAgents(t *testing.T) {
	env := newPurgeTestEnv(t)
	now := time.Now().UTC()
	agent := func(name, status string, lastSeen *time.Time) string {
		fields := map[string]any{"name": name, "token": name, "status": status}
		if lastSeen != nil {
			fields["last_seen"] = *lastSeen
		}
		rec, err := createTestRecord(env.hub, "agents", fields)
		require.NoError(t, err)
		return rec.Id
	}
	old, recent := now.AddDate(0, 0, -200), now.AddDate(0, 0, -10)
	offlineOld := agent("offline-old", "offline", &old)
	offlineRecent := agent("offline-recent", "offline", &recent)
	offlineNeverSeen := agent("offline-never-seen", "offline", nil)
	connectedOld := agent("connected-old", "connected", &old)
	pendingOld := agent("pending-old", "pending", &old)
	awaitingOld := agent("awaiting-old", agentStatusAwaitingApproval, &old)

	_, err := env.hub.purgeOfflineAgentsOlderThan(0)
	assert.Error(t, err)

	deleted, err := env.hub.purgeOfflineAgentsOlderThan(180)
	require.NoError(t, err)
	assert.Equal(t, 1, deleted)
	assert.False(t, env.exists("agents", offlineOld))
	for _, id := range []string{offlineRecent, offlineNeverSeen, connectedOld, pendingOld, awaitingOld, env.agent.Id} {
		assert.True(t, env.exists("agents", id), id)
	}

	deleted, err = env.hub.purgeAllOfflineAgents()
	require.NoError(t, err)
	assert.Equal(t, 2, deleted, "every offline host, whatever its last_seen")
	assert.False(t, env.exists("agents", offlineRecent))
	assert.False(t, env.exists("agents", offlineNeverSeen))
	for _, id := range []string{connectedOld, pendingOld, awaitingOld, env.agent.Id} {
		assert.True(t, env.exists("agents", id), id)
	}
}

func TestNormalizeRetentionSettings(t *testing.T) {
	got := normalizeRetentionSettings(DataRetentionSettings{
		MonitorEventsRetentionDays:        90,
		NotificationLogsRetentionDays:     45, // not an allowed choice
		SystemNotificationsRetentionDays:  0,
		MonitorEventsManualDefaultDays:    12,
		NotificationLogsManualDefaultDays: -3,
		OfflineAgentsManualDefaultDays:    0,
	})
	assert.Equal(t, DataRetentionSettings{
		MonitorEventsRetentionDays:        90,
		NotificationLogsRetentionDays:     defaultNotificationLogsRetentionDays,
		SystemNotificationsRetentionDays:  defaultSystemNotificationsRetentionDays,
		MonitorEventsManualDefaultDays:    12,
		NotificationLogsManualDefaultDays: defaultNotificationLogsManualDefaultDays,
		OfflineAgentsManualDefaultDays:    defaultOfflineAgentsManualDefaultDays,
	}, got)
	for _, days := range []int{30, 90, 180, 360} {
		assert.Equal(t, days, normalizeAutoRetentionDays(days, 1))
	}
}

func TestPurgeAPI(t *testing.T) {
	env := newPurgeTestEnv(t)
	tokenFor := func(email, role string) string {
		user, err := createTestRecord(env.hub, "users", map[string]any{"email": email, "password": "password123", "role": role})
		require.NoError(t, err)
		token, err := user.NewAuthToken()
		require.NoError(t, err)
		return token
	}
	admin := tokenFor("admin@example.com", "admin")
	settingsBody := `{"monitor_events_retention_days":90,"notification_logs_retention_days":30,"system_notifications_retention_days":90,"monitor_events_manual_default_days":30,"notification_logs_manual_default_days":30,"offline_agents_manual_default_days":180}`

	for _, role := range []string{"user", "readonly"} {
		token := tokenFor(role+"@example.com", role)
		assert.Equal(t, http.StatusForbidden, performNotificationRequest(t, env.app, env.hub, http.MethodGet, "/api/app/purge/settings", token).Code, role)
		assert.Equal(t, http.StatusForbidden, performNotificationRequest(t, env.app, env.hub, http.MethodPatch, "/api/app/purge/settings", token, settingsBody).Code, role)
		assert.Equal(t, http.StatusForbidden, performNotificationRequest(t, env.app, env.hub, http.MethodPost, "/api/app/purge/run", token, `{"scope":"monitor_events","mode":"all"}`).Code, role)
	}
	assert.Equal(t, http.StatusUnauthorized, performNotificationRequest(t, env.app, env.hub, http.MethodPost, "/api/app/purge/run", "", `{"scope":"monitor_events","mode":"all"}`).Code)

	res := performNotificationRequest(t, env.app, env.hub, http.MethodPatch, "/api/app/purge/settings", admin, settingsBody)
	require.Equal(t, http.StatusOK, res.Code, res.Body.String())
	res = performNotificationRequest(t, env.app, env.hub, http.MethodGet, "/api/app/purge/settings", admin)
	require.Equal(t, http.StatusOK, res.Code)
	var settings DataRetentionSettings
	require.NoError(t, json.Unmarshal(res.Body.Bytes(), &settings))
	assert.Equal(t, 90, settings.MonitorEventsRetentionDays)

	for _, body := range []string{
		`{"scope":"monitor_events","mode":"older_than_days","days":0}`,
		`{"scope":"offline_agents","mode":"older_than_days","days":-5}`,
		`{"scope":"monitor_events","mode":"everything"}`,
		`{"scope":"users","mode":"all"}`,
	} {
		res := performNotificationRequest(t, env.app, env.hub, http.MethodPost, "/api/app/purge/run", admin, body)
		assert.Equal(t, http.StatusBadRequest, res.Code, body)
		assert.NotContains(t, res.Body.String(), "Something went wrong", body)
	}

	now := time.Now().UTC()
	env.row(t, "monitor_events", now.AddDate(0, 0, -100))
	kept := env.row(t, "monitor_events", now)
	res = performNotificationRequest(t, env.app, env.hub, http.MethodPost, "/api/app/purge/run", admin, `{"scope":"monitor_events","mode":"older_than_days","days":30}`)
	require.Equal(t, http.StatusOK, res.Code, res.Body.String())
	var run purgeRunResponse
	require.NoError(t, json.Unmarshal(res.Body.Bytes(), &run))
	assert.Equal(t, purgeRunResponse{Scope: "monitor_events", Mode: "older_than_days", DeletedCount: 1}, run)
	assert.True(t, env.exists("monitor_events", kept))
}
