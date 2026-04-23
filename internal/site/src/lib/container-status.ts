export function isStoppedContainerStatus(status: string): boolean {
	return status === "exited" || status === "dead"
}

export function isWarningContainerStatus(status: string): boolean {
	return status === "restarting" || status === "dead"
}
