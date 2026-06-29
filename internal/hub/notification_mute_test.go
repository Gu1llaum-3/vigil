//go:build testing

package hub

import (
	"testing"
	"time"

	"github.com/Gu1llaum-3/vigil/internal/hub/notifications"
	"github.com/pocketbase/pocketbase/tools/types"
	"github.com/stretchr/testify/require"
)

// createMute inserts a notification_mutes row. A zero mutedUntil means indefinite.
func createMute(t *testing.T, h *Hub, resourceType, resourceID string, mutedUntil time.Time) {
	t.Helper()
	data := map[string]any{
		"resource_type": resourceType,
		"resource_id":   resourceID,
	}
	if !mutedUntil.IsZero() {
		dt, err := types.ParseDateTime(mutedUntil)
		require.NoError(t, err)
		data["muted_until"] = dt
	}
	_, err := createTestRecord(h, notificationMutesCollection, data)
	require.NoError(t, err)
}

func TestResourceMuted(t *testing.T) {
	hub, testApp, err := createTestHub(t)
	require.NoError(t, err)
	defer cleanupTestHub(hub, testApp)

	now := time.Now()
	future := now.Add(time.Hour)
	past := now.Add(-time.Hour)

	createMute(t, hub, "monitor", "mon_indefinite", time.Time{})
	createMute(t, hub, "monitor", "mon_future", future)
	createMute(t, hub, "monitor", "mon_expired", past)
	createMute(t, hub, "agent", "agentA", time.Time{})

	evt := func(resourceType, id string) notifications.Event {
		return notifications.Event{Resource: notifications.ResourceRef{Type: resourceType, ID: id}}
	}

	tests := []struct {
		name string
		evt  notifications.Event
		want bool
	}{
		{"indefinite monitor mute", evt("monitor", "mon_indefinite"), true},
		{"future monitor mute", evt("monitor", "mon_future"), true},
		{"expired monitor mute", evt("monitor", "mon_expired"), false},
		{"unmuted monitor", evt("monitor", "mon_other"), false},
		{"muted agent", evt("agent", "agentA"), true},
		{"unmuted agent", evt("agent", "agentB"), false},
		// A muted host silences its containers' image-audit notifications.
		{"container under muted host", evt("container_image", "agentA|nginx"), true},
		{"container under unmuted host", evt("container_image", "agentB|nginx"), false},
		// A direct container mute also works.
		{"empty resource", evt("", ""), false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, hub.resourceMuted(tt.evt, now))
		})
	}

	// A direct container_image mute is keyed by the stable container NAME (resolved from
	// the event Details), so it survives the container being recreated with a new id on
	// the very redeploy the mute is meant to outlive.
	createMute(t, hub, "container_image", auditContainerKey("agentB", "redis"), time.Time{})
	containerEvt := notifications.Event{
		Resource: notifications.ResourceRef{Type: "container_image", ID: auditContainerKey("agentB", "deadbeef0001")},
		Details:  map[string]any{"agent_id": "agentB", "container_name": "redis"},
	}
	require.True(t, hub.resourceMuted(containerEvt, now))
	// Same container, recreated with a different id but the same name → still muted.
	containerEvt.Resource.ID = auditContainerKey("agentB", "f00ba40002")
	require.True(t, hub.resourceMuted(containerEvt, now))
	// A different container name on the same host is unaffected.
	require.False(t, hub.resourceMuted(notifications.Event{
		Resource: notifications.ResourceRef{Type: "container_image", ID: auditContainerKey("agentB", "x")},
		Details:  map[string]any{"agent_id": "agentB", "container_name": "postgres"},
	}, now))
}

// TestEmitNotificationSuppressesBell verifies the chokepoint skips the in-app bell write
// when the resource is muted, and writes it otherwise.
func TestEmitNotificationSuppressesBell(t *testing.T) {
	hub, testApp, err := createTestHub(t)
	require.NoError(t, err)
	defer cleanupTestHub(hub, testApp)

	bellCount := func() int {
		recs, err := hub.FindRecordsByFilter(systemNotificationsCollection, "", "", 0, 0)
		require.NoError(t, err)
		return len(recs)
	}

	mutedEvt := notifications.Event{
		Kind:     notifications.EventMonitorDown,
		Resource: notifications.ResourceRef{Type: "monitor", ID: "mon_muted", Name: "muted"},
		Current:  "down",
	}
	liveEvt := notifications.Event{
		Kind:     notifications.EventMonitorDown,
		Resource: notifications.ResourceRef{Type: "monitor", ID: "mon_live", Name: "live"},
		Current:  "down",
	}

	createMute(t, hub, "monitor", "mon_muted", time.Time{})

	// The single isNotificationSuppressed gate at the top of emitNotification governs
	// BOTH the bell and external dispatch, so asserting the predicate covers the channel
	// path too (AGENTS.md: a mute must silence the bell AND external channels).
	require.True(t, hub.isNotificationSuppressed(mutedEvt))
	require.False(t, hub.isNotificationSuppressed(liveEvt))

	hub.emitNotification(mutedEvt)
	require.Equal(t, 0, bellCount(), "muted event must not write a bell notification")

	hub.emitNotification(liveEvt)
	require.Equal(t, 1, bellCount(), "unmuted event must write a bell notification")
}
