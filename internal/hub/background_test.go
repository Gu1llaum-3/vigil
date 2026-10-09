//go:build testing

package hub

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The shutdown waits for the background goroutines, so none writes to a closed database,
// and refuses new ones once it has started.
func TestStopBackgroundWaits(t *testing.T) {
	var h Hub
	h.bgCtx, h.cancelBg = context.WithCancel(context.Background())
	release := make(chan struct{})
	var finished atomic.Bool
	require.True(t, h.goBackground("collector", func() {
		<-release
		finished.Store(true)
	}))

	stopped := make(chan struct{})
	go func() {
		h.stopBackground(5 * time.Second)
		close(stopped)
	}()
	select {
	case <-stopped:
		t.Fatal("returned while a goroutine still runs")
	case <-time.After(50 * time.Millisecond):
	}
	close(release)
	<-stopped
	assert.True(t, finished.Load())

	// Long work watches backgroundContext, cancelled by the shutdown.
	assert.Error(t, h.backgroundContext().Err())

	assert.False(t, h.goBackground("late", func() { t.Error("must not run") }))
	assert.False(t, h.runBackground(func() { t.Error("must not run") }))
}

func TestStopBackgroundGivesUp(t *testing.T) {
	var h Hub
	stuck := make(chan struct{})
	defer close(stuck)
	h.goBackground("stuck", func() { <-stuck })
	start := time.Now()
	assert.False(t, h.stopBackground(50*time.Millisecond))
	assert.Less(t, time.Since(start), 2*time.Second, "a stuck goroutine does not block the shutdown forever")
}

func TestBackgroundRecoversPanics(t *testing.T) {
	var h Hub
	h.goBackground("panicking", func() { panic("boom") })
	assert.True(t, h.runBackground(func() {}))
	h.stopBackground(time.Second)
}

// Monitor checks are background goroutines: the shutdown waits for them to return.
func TestMonitorChecksAreAwaited(t *testing.T) {
	hub, testApp, err := createTestHub(t)
	require.NoError(t, err)
	defer cleanupTestHub(hub, testApp)
	monitor, err := createTestRecord(hub, "monitors", map[string]any{"name": "m", "type": "push", "interval": 60, "active": true})
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	hub.monitorScheduler.ctx = ctx
	hub.monitorScheduler.startMonitor(monitor.Id)
	cancel()
	assert.True(t, hub.stopBackground(5*time.Second), "the cancelled check returned and was awaited")

	hub.monitorScheduler.startMonitor(monitor.Id) // after the shutdown: refused, no goroutine
	_, registered := hub.monitorScheduler.cancels.Load(monitor.Id)
	assert.False(t, registered)
	assert.False(t, hub.runBackground(func() {}))
}

// A purge stops between batches once the hub is stopping, leaving the rest to the next run.
func TestPurgeStopsWhenTheHubStops(t *testing.T) {
	env := newPurgeTestEnv(t)
	env.row(t, "monitor_events", time.Now().AddDate(0, 0, -100))
	env.hub.cancelBg()
	_, err := env.hub.purgeMonitorEventsOlderThan(30)
	assert.ErrorIs(t, err, context.Canceled)
}
