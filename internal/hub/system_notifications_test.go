//go:build testing

package hub

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"testing"
	"time"

	"github.com/pocketbase/pocketbase/core"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type systemNotificationsTestEnv struct {
	hub   *Hub
	app   core.App
	user  *core.Record
	token string
	now   time.Time
}

func newSystemNotificationsTestEnv(t *testing.T) systemNotificationsTestEnv {
	t.Helper()
	hub, testApp, err := createTestHub(t)
	require.NoError(t, err)
	t.Cleanup(func() { cleanupTestHub(hub, testApp) })
	user, err := createTestRecord(hub, "users", map[string]any{"email": "user@example.com", "password": "password123", "role": "admin"})
	require.NoError(t, err)
	token, err := user.NewAuthToken()
	require.NoError(t, err)
	return systemNotificationsTestEnv{hub: hub, app: testApp, user: user, token: token, now: time.Now().UTC().Truncate(time.Second)}
}

// add records a notification that occurred ago before env.now.
func (env systemNotificationsTestEnv) add(t *testing.T, kind, category string, ago time.Duration) string {
	t.Helper()
	rec, err := createTestRecord(env.hub, systemNotificationsCollection, map[string]any{
		"event_kind":  kind,
		"category":    category,
		"severity":    "warning",
		"title":       fmt.Sprintf("%s %s", kind, ago),
		"occurred_at": env.now.Add(-ago),
	})
	require.NoError(t, err)
	return rec.Id
}

func (env systemNotificationsTestEnv) readUpTo(t *testing.T, category string, at time.Time) {
	t.Helper()
	prefs, err := env.hub.systemNotificationPreferencesForUser(env.user.Id)
	require.NoError(t, err)
	prefs.LastReadAtByCategory[category] = at.Format(time.RFC3339Nano)
	require.NoError(t, env.hub.saveSystemNotificationPreferences(env.user.Id, prefs))
}

func (env systemNotificationsTestEnv) get(t *testing.T, path string, out any) {
	t.Helper()
	res := performNotificationRequest(t, env.app, env.hub, http.MethodGet, path, env.token)
	require.Equal(t, http.StatusOK, res.Code, res.Body.String())
	require.NoError(t, json.Unmarshal(res.Body.Bytes(), out))
}

func ids(items []systemNotificationResponse) []string {
	out := make([]string, 0, len(items))
	for _, item := range items {
		out = append(out, item.ID)
	}
	return out
}

func TestUnreadSystemNotifications(t *testing.T) {
	env := newSystemNotificationsTestEnv(t)
	env.add(t, "monitor.down", "monitors", 3*time.Hour)
	readEdge := env.add(t, "monitor.up", "monitors", 2*time.Hour)
	newMonitor := env.add(t, "monitor.down", "monitors", time.Hour)
	agent := env.add(t, "agent.offline", "agents", 90*time.Minute)
	env.add(t, "agent.online", "agents", 30*time.Minute) // disabled below
	env.readUpTo(t, "monitors", env.now.Add(-2*time.Hour))

	prefs, err := env.hub.systemNotificationPreferencesForUser(env.user.Id)
	require.NoError(t, err)
	prefs.EnabledEvents["agent.online"] = false
	require.NoError(t, env.hub.saveSystemNotificationPreferences(env.user.Id, prefs))

	var unread systemNotificationUnreadResponse
	env.get(t, "/api/app/system-notifications/unread?limit=10", &unread)
	assert.Equal(t, 2, unread.Count, "read up to (and including) the cursor, disabled events left out")
	assert.Equal(t, []string{newMonitor, agent}, ids(unread.Items), "newest first")
	assert.NotContains(t, ids(unread.Items), readEdge)

	env.get(t, "/api/app/system-notifications/unread?limit=1", &unread)
	assert.Equal(t, 2, unread.Count, "the count is not capped by the limit")
	assert.Equal(t, []string{newMonitor}, ids(unread.Items))

	prefs.EnabledCategories["agents"] = false
	require.NoError(t, env.hub.saveSystemNotificationPreferences(env.user.Id, prefs))
	env.get(t, "/api/app/system-notifications/unread?limit=10", &unread)
	assert.Equal(t, 1, unread.Count)

	prefs.EnabledCategories["monitors"] = false
	require.NoError(t, env.hub.saveSystemNotificationPreferences(env.user.Id, prefs))
	env.get(t, "/api/app/system-notifications/unread?limit=10", &unread)
	assert.Equal(t, 0, unread.Count, "every category disabled")
	assert.Empty(t, unread.Items)
}

func TestSystemNotificationsPages(t *testing.T) {
	env := newSystemNotificationsTestEnv(t)
	var all []string
	for i := 1; i <= 5; i++ {
		all = append(all, env.add(t, "monitor.down", "monitors", time.Duration(i)*time.Hour))
	}
	agent := env.add(t, "agent.offline", "agents", 150*time.Minute)
	// all[0..1] (1h, 2h ago) unread; all[2..4] read.
	env.readUpTo(t, "monitors", env.now.Add(-3*time.Hour))

	var page systemNotificationsPageResponse
	env.get(t, "/api/app/system-notifications?limit=4", &page)
	assert.Equal(t, []string{all[0], all[1], agent, all[2]}, ids(page.Items))
	assert.True(t, page.HasMore)
	assert.True(t, page.Items[3].Read)
	assert.False(t, page.Items[2].Read, "no cursor for agents")

	env.get(t, "/api/app/system-notifications?limit=4&page=2", &page)
	assert.Equal(t, []string{all[3], all[4]}, ids(page.Items))
	assert.False(t, page.HasMore)

	env.get(t, "/api/app/system-notifications?status=unread&limit=2", &page)
	assert.Equal(t, []string{all[0], all[1]}, ids(page.Items))
	assert.True(t, page.HasMore)
	env.get(t, "/api/app/system-notifications?status=unread&limit=2&page=2", &page)
	assert.Equal(t, []string{agent}, ids(page.Items), "unread filtered in the query, so pages stay full")
	assert.False(t, page.HasMore)

	env.get(t, "/api/app/system-notifications?category=agents", &page)
	assert.Equal(t, []string{agent}, ids(page.Items))
}

func TestSystemNotificationsRetention(t *testing.T) {
	env := newSystemNotificationsTestEnv(t)
	kept := env.add(t, "monitor.down", "monitors", 89*24*time.Hour)
	env.add(t, "monitor.down", "monitors", 91*24*time.Hour)

	settings, err := env.hub.getRetentionSettings()
	require.NoError(t, err)
	assert.Equal(t, 90, settings.SystemNotificationsRetentionDays, "default")

	result := env.hub.runAutomaticRetentionPurge()
	require.Equal(t, "success", result.Status, result.Error)
	assert.Equal(t, 1, result.SystemNotificationsDeleted)
	left, err := env.hub.FindRecordsByFilter(systemNotificationsCollection, "", "", 0, 0)
	require.NoError(t, err)
	require.Len(t, left, 1)
	assert.Equal(t, kept, left[0].Id)

	patch := func(body string) int {
		return performNotificationRequest(t, env.app, env.hub, http.MethodPatch, "/api/app/purge/settings", env.token, body).Code
	}
	base := `"monitor_events_retention_days":30,"notification_logs_retention_days":30,"monitor_events_manual_default_days":30,"notification_logs_manual_default_days":30,"offline_agents_manual_default_days":180`
	assert.Equal(t, http.StatusBadRequest, patch(`{`+base+`,"system_notifications_retention_days":45}`))
	require.Equal(t, http.StatusOK, patch(`{`+base+`,"system_notifications_retention_days":30}`))
	settings, err = env.hub.getRetentionSettings()
	require.NoError(t, err)
	assert.Equal(t, 30, settings.SystemNotificationsRetentionDays)
	require.Equal(t, http.StatusOK, patch(`{`+base+`}`), "clients that predate the setting keep working")
	settings, err = env.hub.getRetentionSettings()
	require.NoError(t, err)
	assert.Equal(t, 30, settings.SystemNotificationsRetentionDays, "and do not reset it")

	result = env.hub.runAutomaticRetentionPurge()
	require.Equal(t, "success", result.Status, result.Error)
	assert.Equal(t, 1, result.SystemNotificationsDeleted)
}

// Read cursors are written at nanosecond precision, in any offset; occurred_at is stored at
// millisecond precision.
func TestUnreadSystemNotificationsCursorEdges(t *testing.T) {
	env := newSystemNotificationsTestEnv(t)
	atCursor := env.add(t, "monitor.down", "monitors", time.Hour)
	after := env.add(t, "monitor.up", "monitors", time.Hour-time.Millisecond)
	agent := env.add(t, "agent.offline", "agents", time.Hour)
	var unread systemNotificationUnreadResponse

	env.readUpTo(t, "monitors", env.now.Add(-time.Hour+500*time.Microsecond))
	env.get(t, "/api/app/system-notifications/unread?limit=10", &unread)
	assert.Equal(t, []string{after, agent}, ids(unread.Items), "an entry before a sub-millisecond cursor is read")

	prefs, err := env.hub.systemNotificationPreferencesForUser(env.user.Id)
	require.NoError(t, err)
	prefs.LastReadAtByCategory["monitors"] = env.now.Add(-time.Hour).In(time.FixedZone("CEST", 2*3600)).Format(time.RFC3339Nano)
	prefs.LastReadAtByCategory["agents"] = "not a date"
	require.NoError(t, env.hub.saveSystemNotificationPreferences(env.user.Id, prefs))
	env.get(t, "/api/app/system-notifications/unread?limit=10", &unread)
	assert.Equal(t, []string{after, agent}, ids(unread.Items), "a cursor in another offset; a malformed cursor reads nothing")
	assert.NotContains(t, ids(unread.Items), atCursor)
	assert.Equal(t, 2, unread.Count)
}

func TestSystemNotificationsSearch(t *testing.T) {
	env := newSystemNotificationsTestEnv(t)
	rec, err := createTestRecord(env.hub, systemNotificationsCollection, map[string]any{
		"event_kind": "agent.offline", "category": "agents", "severity": "warning",
		"title": "web_01 is offline", "message": "disk at 50% on web_01", "occurred_at": env.now,
	})
	require.NoError(t, err)
	env.add(t, "agent.offline", "agents", time.Hour) // title "agent.offline 1h0m0s"

	var page systemNotificationsPageResponse
	for _, q := range []string{"web_01", "50%", "WEB_01"} {
		env.get(t, "/api/app/system-notifications?q="+url.QueryEscape(q), &page)
		assert.Equal(t, []string{rec.Id}, ids(page.Items), "q=%s", q)
	}
	env.get(t, "/api/app/system-notifications?q="+url.QueryEscape("web%01"), &page)
	assert.Empty(t, page.Items, "wildcards in the search are literal")
}
