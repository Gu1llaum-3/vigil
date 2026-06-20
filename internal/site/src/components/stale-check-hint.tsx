import { Trans, useLingui } from "@lingui/react/macro"
import { AlertTriangleIcon } from "lucide-react"
import type { ContainerImageAudit } from "@/lib/dashboard-types"
import { cn } from "@/lib/utils"

/**
 * StaleCheckHint shows a discreet "last check errored" indicator when an image audit's
 * status is still a good (preserved) result but recent checks failed transiently
 * (consecutive_failures > 0). Renders nothing otherwise, so it is safe to drop next to
 * any audit status badge. Keeps the dashboard, images page and container detail consistent.
 */
export function StaleCheckHint({
	audit,
	className,
}: {
	audit?: ContainerImageAudit | null
	className?: string
}) {
	const { t } = useLingui()
	if (!audit || (audit.consecutive_failures ?? 0) === 0 || audit.status === "check_failed") {
		return null
	}
	return (
		<span
			className={cn("inline-flex items-center gap-1 text-amber-500/80", className)}
			title={audit.last_check_error || t`The last image check failed; showing the last successful result.`}
		>
			<AlertTriangleIcon className="size-3 shrink-0" />
			<Trans>last check errored</Trans>
		</span>
	)
}
