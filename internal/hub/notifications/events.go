package notifications

import "time"

// EventKind identifies the type of notification event.
type EventKind string

const (
	EventMonitorDown  EventKind = "monitor.down"
	EventMonitorUp    EventKind = "monitor.up"
	EventAgentOffline EventKind = "agent.offline"
	EventAgentOnline  EventKind = "agent.online"
	EventContainerImageUpdateAvailable EventKind = "container_image.update_available"
)

// ResourceRef identifies the resource that triggered the event.
type ResourceRef struct {
	ID   string
	Name string
	Type string // "monitor" | "agent"
}

// Event carries all information about a state change that should trigger notifications.
type Event struct {
	Kind       EventKind
	OccurredAt time.Time
	Resource   ResourceRef
	Previous   string
	Current    string
	Details    map[string]any
	// Severity overrides Kind.Severity() when non-empty. Used by event sources
	// that derive severity from the payload (e.g. image audits where a new
	// major version is more severe than a patch update).
	Severity string
}

// EffectiveSeverity returns the explicit Severity if set, otherwise the
// default severity associated with the event kind.
func (e Event) EffectiveSeverity() string {
	if e.Severity != "" {
		return e.Severity
	}
	return e.Kind.Severity()
}

// KindForMonitor maps a monitor status int to the appropriate EventKind.
// monitorStatusUp = 1, monitorStatusDown = 0 (from hub package constants).
func KindForMonitor(status int) EventKind {
	if status == 1 {
		return EventMonitorUp
	}
	return EventMonitorDown
}

// KindForAgent maps an agent status string to the appropriate EventKind.
func KindForAgent(status string) EventKind {
	if status == "offline" {
		return EventAgentOffline
	}
	return EventAgentOnline
}

// Severity returns the severity level of the event kind.
func (k EventKind) Severity() string {
	switch k {
	case EventMonitorDown, EventAgentOffline:
		return "critical"
	default:
		return "info"
	}
}
