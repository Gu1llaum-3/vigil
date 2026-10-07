//go:build testing

package hub

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func monitorEventCount(t *testing.T, hub *Hub, monitorID string) int {
	t.Helper()
	events, err := hub.FindRecordsByFilter("monitor_events", "monitor = {:id}", "", 0, 0, dbx.Params{"id": monitorID})
	require.NoError(t, err)
	return len(events)
}

// Editing a monitor cancels its in-flight check: that check must neither write the record it
// loaded before the edit back nor record a failure.
func TestCancelledCheckIsNotSaved(t *testing.T) {
	t.Setenv("MONITOR_ALLOW_PRIVATE_TARGETS", "true")
	hub, testApp, err := createTestHub(t)
	require.NoError(t, err)
	defer cleanupTestHub(hub, testApp)

	reached := make(chan struct{}, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reached <- struct{}{}
		<-r.Context().Done()
	}))
	defer srv.Close()

	monitor, err := createTestRecord(hub, "monitors", map[string]any{
		"name": "before", "type": "http", "url": srv.URL, "active": true,
		"timeout": 10, "status": monitorStatusUp, "failure_threshold": 1,
	})
	require.NoError(t, err)

	ms := newMonitorScheduler(hub)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		ms.doCheck(ctx, monitor.Id)
		close(done)
	}()
	select {
	case <-reached:
	case <-time.After(5 * time.Second):
		t.Fatal("check never reached the target")
	}

	edited, err := hub.FindRecordById("monitors", monitor.Id)
	require.NoError(t, err)
	edited.Set("name", "after")
	edited.Set("active", false)
	require.NoError(t, hub.Save(edited))
	cancel() // what updateMonitor does through stopMonitor
	<-done

	got, err := hub.FindRecordById("monitors", monitor.Id)
	require.NoError(t, err)
	assert.Equal(t, "after", got.GetString("name"))
	assert.False(t, got.GetBool("active"), "a pause must not be reverted")
	assert.Equal(t, monitorStatusUp, got.GetInt("status"), "a cancelled check is not a failure")
	assert.Equal(t, 0, monitorEventCount(t, hub, monitor.Id))
}

// A check result only owns the scheduler columns: fields edited while the check ran are kept.
func TestSaveResultKeepsConcurrentEdits(t *testing.T) {
	hub, testApp, err := createTestHub(t)
	require.NoError(t, err)
	defer cleanupTestHub(hub, testApp)

	stale, err := createTestRecord(hub, "monitors", map[string]any{
		"name": "before", "type": "http", "url": "https://before.example", "active": true,
		"status": monitorStatusUp, "failure_threshold": 1,
	})
	require.NoError(t, err)

	edited, err := hub.FindRecordById("monitors", stale.Id)
	require.NoError(t, err)
	edited.Set("name", "after")
	edited.Set("interval", 120)
	require.NoError(t, hub.Save(edited))

	newMonitorScheduler(hub).saveResult(stale, monitorStatusDown, 0, "connection refused")

	got, err := hub.FindRecordById("monitors", stale.Id)
	require.NoError(t, err)
	assert.Equal(t, "after", got.GetString("name"))
	assert.Equal(t, 120, got.GetInt("interval"))
	assert.Equal(t, monitorStatusDown, got.GetInt("status"))
	assert.Equal(t, 1, got.GetInt("failure_count"))
	assert.Equal(t, "connection refused", got.GetString("last_msg"))
}

func TestSaveResultSkipsDeletedMonitor(t *testing.T) {
	hub, testApp, err := createTestHub(t)
	require.NoError(t, err)
	defer cleanupTestHub(hub, testApp)

	stale, err := createTestRecord(hub, "monitors", map[string]any{
		"name": "gone", "type": "http", "url": "https://gone.example", "active": true, "status": monitorStatusUp,
	})
	require.NoError(t, err)
	require.NoError(t, hub.Delete(stale))

	newMonitorScheduler(hub).saveResult(stale, monitorStatusDown, 0, "connection refused")

	assert.Equal(t, 0, monitorEventCount(t, hub, stale.Id))
	_, err = hub.FindRecordById("monitors", stale.Id)
	assert.Error(t, err, "the deleted monitor must not be re-created")
}

func TestSaveResultDropsResultOfPausedOrReconfiguredMonitor(t *testing.T) {
	hub, testApp, err := createTestHub(t)
	require.NoError(t, err)
	defer cleanupTestHub(hub, testApp)

	for name, edit := range map[string]func(rec *core.Record){
		"paused":       func(rec *core.Record) { rec.Set("active", false) },
		"reconfigured": func(rec *core.Record) { rec.Set("url", "https://new.example") },
	} {
		t.Run(name, func(t *testing.T) {
			stale, err := createTestRecord(hub, "monitors", map[string]any{
				"name": name, "type": "http", "url": "https://old.example", "active": true,
				"status": monitorStatusUp, "failure_threshold": 1,
			})
			require.NoError(t, err)
			edited, err := hub.FindRecordById("monitors", stale.Id)
			require.NoError(t, err)
			edit(edited)
			require.NoError(t, hub.Save(edited))

			newMonitorScheduler(hub).saveResult(stale, monitorStatusDown, 0, "connection refused")

			got, err := hub.FindRecordById("monitors", stale.Id)
			require.NoError(t, err)
			assert.Equal(t, monitorStatusUp, got.GetInt("status"), "no DOWN from a check of the previous config")
			assert.Equal(t, 0, monitorEventCount(t, hub, stale.Id))
		})
	}
}

// A check that runs into its own timeout is a real failure: only the monitor context
// being cancelled discards the result.
func TestTimedOutCheckIsSaved(t *testing.T) {
	t.Setenv("MONITOR_ALLOW_PRIVATE_TARGETS", "true")
	hub, testApp, err := createTestHub(t)
	require.NoError(t, err)
	defer cleanupTestHub(hub, testApp)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	}))
	defer srv.Close()
	monitor, err := createTestRecord(hub, "monitors", map[string]any{
		"name": "slow", "type": "http", "url": srv.URL, "active": true,
		"timeout": 1, "status": monitorStatusUp, "failure_threshold": 1,
	})
	require.NoError(t, err)

	newMonitorScheduler(hub).doCheck(context.Background(), monitor.Id)

	got, err := hub.FindRecordById("monitors", monitor.Id)
	require.NoError(t, err)
	assert.Equal(t, monitorStatusDown, got.GetInt("status"))
	assert.Equal(t, 1, monitorEventCount(t, hub, monitor.Id))
}

// API writes on a monitor must not overwrite the status the scheduler wrote meanwhile.
func TestPushHeartbeatKeepsSchedulerColumns(t *testing.T) {
	hub, testApp, err := createTestHub(t)
	require.NoError(t, err)
	defer cleanupTestHub(hub, testApp)

	monitor, err := createTestRecord(hub, "monitors", map[string]any{
		"name": "push", "type": "push", "push_token": "push-tok", "active": true,
		"status": monitorStatusDown, "failure_count": 4,
	})
	require.NoError(t, err)
	stale, err := hub.FindRecordById("monitors", monitor.Id)
	require.NoError(t, err)
	stale.IgnoreUnchangedFields(true)
	// the scheduler records a recovery
	newMonitorScheduler(hub).saveResult(monitor, monitorStatusUp, 0, "ok")
	// a write from an older copy that only changes last_push_at
	stale.Set("last_push_at", time.Now())
	require.NoError(t, hub.SaveNoValidate(stale))

	got, err := hub.FindRecordById("monitors", monitor.Id)
	require.NoError(t, err)
	assert.Equal(t, monitorStatusUp, got.GetInt("status"))
	assert.Equal(t, 0, got.GetInt("failure_count"))
}
