package hub

import (
	"log/slog"
	"strings"
	"time"

	"github.com/Gu1llaum-3/vigil/internal/hub/notifications"
	"github.com/pocketbase/dbx"
)

const notificationMutesCollection = "notification_mutes"

// emitNotification is the single chokepoint every notification event flows through.
// It gates delivery on isNotificationSuppressed (per-resource mutes today; maintenance
// windows are layered on in the maintenance feature) before both the in-app bell
// (createSystemNotification) and external channels (notifier.Dispatch). Keeping the
// suppression check here — rather than inside the dispatcher — ensures the bell and the
// channels are silenced together, which is what muting a resource is expected to do.
func (h *Hub) emitNotification(evt notifications.Event) {
	if h.isNotificationSuppressed(evt) {
		return
	}
	if err := h.createSystemNotification(evt); err != nil {
		slog.Warn("notifications: failed to create system notification",
			"kind", evt.Kind, "resource", evt.Resource.ID, "err", err)
	}
	if h.notifier != nil {
		h.notifier.Dispatch(evt)
	}
}

// isNotificationSuppressed reports whether an event must not produce any notification.
// Branch A only checks per-resource mutes; the maintenance feature extends this.
func (h *Hub) isNotificationSuppressed(evt notifications.Event) bool {
	return h.resourceMuted(evt, time.Now())
}

// resourceMuted reports whether the event's resource is currently muted. A mute on a
// host (agent) also silences that host's container image-audit notifications, so muting
// a noisy host covers its containers and metric alerts without muting each individually.
func (h *Hub) resourceMuted(evt notifications.Event, now time.Time) bool {
	resourceType := evt.Resource.Type
	if resourceType == "" {
		return false
	}
	if resourceType == "container_image" {
		return h.containerImageMuted(evt, now)
	}
	return evt.Resource.ID != "" && h.muteActive(resourceType, evt.Resource.ID, now)
}

// containerImageMuted handles the container_image case. The event's resource id is keyed
// by the ephemeral Docker container id ("<agentID>|<containerID>"), which changes on the
// very redeploy an image-update mute is meant to outlive. So mutes are keyed by the stable
// container *name* instead (matching the container_audit_overrides convention) — resolved
// from the event Details. A mute on the parent host (agent) also covers the container.
func (h *Hub) containerImageMuted(evt notifications.Event, now time.Time) bool {
	agentID, _ := evt.Details["agent_id"].(string)
	containerName, _ := evt.Details["container_name"].(string)
	if agentID == "" {
		// Fall back to the composite resource id when Details is absent.
		agentID, _, _ = strings.Cut(evt.Resource.ID, "|")
	}
	if agentID != "" && containerName != "" {
		if h.muteActive("container_image", auditContainerKey(agentID, containerName), now) {
			return true
		}
	}
	return agentID != "" && h.muteActive("agent", agentID, now)
}

// muteActive reports whether an active mute exists for the given resource. A mute is
// active when its muted_until is empty (indefinite) or still in the future.
func (h *Hub) muteActive(resourceType, resourceID string, now time.Time) bool {
	records, err := h.FindRecordsByFilter(
		notificationMutesCollection,
		"resource_type = {:type} && resource_id = {:id}",
		"", 0, 0,
		dbx.Params{"type": resourceType, "id": resourceID},
	)
	if err != nil {
		// Fail open: an alerting system must not silently swallow notifications on a
		// transient DB error. Log so the dropped suppression is observable.
		slog.Warn("notification mute lookup failed; not suppressing",
			"type", resourceType, "id", resourceID, "err", err)
		return false
	}
	if len(records) == 0 {
		return false
	}
	for _, rec := range records {
		until := rec.GetDateTime("muted_until")
		if until.IsZero() || until.Time().After(now) {
			return true
		}
	}
	return false
}
