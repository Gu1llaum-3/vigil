import { useLingui } from "@lingui/react/macro"
import type { ContainerImageAudit } from "@/lib/dashboard-types"

export type AuditBucket = "major" | "update" | "up_to_date" | "failed" | "disabled" | "other"

// classifyAuditBucket maps a container image audit to a coarse bucket shared across
// every view that displays audit status (dashboard, images page, host/container detail).
export function classifyAuditBucket(audit: ContainerImageAudit): AuditBucket {
	if (audit.major_update_available) return "major"
	const ls = audit.line_status || audit.status
	if (
		ls === "patch_available" ||
		ls === "minor_available" ||
		ls === "tag_rebuilt" ||
		audit.status === "update_available"
	)
		return "update"
	if (audit.status === "up_to_date" || ls === "up_to_date") return "up_to_date"
	if (audit.status === "check_failed") return "failed"
	if (audit.status === "disabled") return "disabled"
	return "other"
}

// auditBadgeClass returns the border/background/text classes for an audit bucket.
export function auditBadgeClass(bucket: AuditBucket): string {
	switch (bucket) {
		case "major":
			return "border-sky-500/30 bg-sky-500/10 text-sky-500 dark:text-sky-400"
		case "update":
			return "border-amber-500/30 bg-amber-500/10 text-amber-600 dark:text-amber-400"
		case "up_to_date":
			return "border-emerald-500/30 bg-emerald-500/10 text-emerald-600 dark:text-emerald-400"
		case "failed":
			return "border-red-500/30 bg-red-500/10 text-red-600 dark:text-red-400"
		case "disabled":
		case "other":
		default:
			return "border-border/40 text-muted-foreground"
	}
}

// useAuditLabel returns a translator that maps an audit to its human-readable, per-row label.
export function useAuditLabel(): (audit: ContainerImageAudit | null | undefined) => string {
	const { t } = useLingui()
	return (audit: ContainerImageAudit | null | undefined) => {
		if (!audit) return ""
		const ls = audit.line_status || audit.status
		if (ls === "patch_available") return t`Patch available`
		if (ls === "minor_available") return t`Minor available`
		if (ls === "tag_rebuilt") return t`Tag rebuilt`
		if (audit.status === "update_available") return t`Update available`
		if (audit.status === "up_to_date" || ls === "up_to_date") return t`Up to date`
		if (audit.status === "check_failed") return t`Check failed`
		if (audit.status === "unsupported") return t`Unsupported`
		if (audit.status === "disabled") return t`Disabled`
		return t`Unknown`
	}
}
