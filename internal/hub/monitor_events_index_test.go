//go:build testing

package hub

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// TestMonitorEventsCompositeIndex verifies migration 23: monitor_events should have the
// composite (monitor, checked_at) index, keep the (checked_at) index for purges, and no
// longer carry the redundant single-column (monitor) index.
func TestMonitorEventsCompositeIndex(t *testing.T) {
	hub, testApp, err := createTestHub(t)
	require.NoError(t, err)
	defer cleanupTestHub(hub, testApp)

	var names []string
	err = hub.DB().
		NewQuery("SELECT name FROM sqlite_master WHERE type = 'index' AND tbl_name = 'monitor_events'").
		Column(&names)
	require.NoError(t, err)

	has := func(name string) bool {
		for _, n := range names {
			if n == name {
				return true
			}
		}
		return false
	}

	require.True(t, has("idx_monitor_events_monitor_checked_at"), "composite index should exist: %v", names)
	require.True(t, has("idx_monitor_events_checked_at"), "checked_at index should remain for purges: %v", names)
	require.False(t, has("idx_monitor_events_monitor"), "redundant single-column monitor index should be removed: %v", names)
}
