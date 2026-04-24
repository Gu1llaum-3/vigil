export type ContainerSeverity = "ok" | "warning" | "error" | "neutral"

export interface ContainerSeverityInput {
	status: string
	exit_code?: number | null
}

// containerSeverity is the single source of truth for classifying a container's
// state into a user-visible severity. Keep in sync with the backend counts in
// internal/hub/dashboard.go (ContainersInWarning / ContainersInError).
//
// - running             → ok
// - restarting          → warning (may still recover via restart policy)
// - dead                → error
// - exited (code === 0) → neutral (one-shot job finished cleanly)
// - exited (code !== 0) → error (crash / non-graceful termination)
// - paused / created    → neutral (intentional, not a problem)
// - anything else       → neutral
export function containerSeverity(container: ContainerSeverityInput): ContainerSeverity {
	switch (container.status) {
		case "running":
			return "ok"
		case "restarting":
			return "warning"
		case "dead":
			return "error"
		case "exited":
			return container.exit_code === 0 ? "neutral" : "error"
		default:
			return "neutral"
	}
}

export function isWarningContainer(container: ContainerSeverityInput): boolean {
	return containerSeverity(container) === "warning"
}

export function isErrorContainer(container: ContainerSeverityInput): boolean {
	return containerSeverity(container) === "error"
}

// isStoppedContainerStatus covers the "not running" terminal states, regardless
// of severity. Used by the "Stopped" filter chip which groups exited + dead.
export function isStoppedContainerStatus(status: string): boolean {
	return status === "exited" || status === "dead"
}
