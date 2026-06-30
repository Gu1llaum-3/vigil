import { Trans, useLingui } from "@lingui/react/macro"
import { WrenchIcon } from "lucide-react"
import { useEffect, useState } from "react"
import { apiGet, pb } from "@/lib/api"
import { cn } from "@/lib/utils"

interface ActiveMaintenance {
	id: string
	title: string
	description?: string
	severity: "info" | "warning" | "critical"
	ends_at?: string
}

// Poll cadence: recurring windows activate by wall-clock, not by a DB change, so the
// banner refreshes on a timer (plus on tab focus) rather than relying on realtime.
const POLL_MS = 60_000

const TONES: Record<ActiveMaintenance["severity"], string> = {
	info: "bg-sky-500/15 text-sky-700 dark:text-sky-300 border-sky-500/30",
	warning: "bg-amber-500/15 text-amber-700 dark:text-amber-300 border-amber-500/30",
	critical: "bg-red-500/15 text-red-700 dark:text-red-300 border-red-500/30",
}

/**
 * MaintenanceBanner shows a global "maintenance in progress" bar to every authenticated
 * user while any maintenance window is active. The bar colour follows the highest active
 * severity. Data comes from the authenticated /api/app/maintenance/active endpoint.
 */
export function MaintenanceBanner() {
	const { t } = useLingui()
	const [windows, setWindows] = useState<ActiveMaintenance[]>([])

	useEffect(() => {
		let cancelled = false
		let debounce: ReturnType<typeof setTimeout> | null = null
		const load = async () => {
			try {
				const data = await apiGet<ActiveMaintenance[]>("/api/app/maintenance/active")
				if (!cancelled) setWindows(data ?? [])
			} catch {
				// non-fatal: keep the last known state
			}
		}
		// Debounced refetch so a burst of realtime events coalesces into one /active call.
		const scheduleLoad = () => {
			if (debounce) clearTimeout(debounce)
			debounce = setTimeout(load, 300)
		}
		load()
		// Realtime: react instantly when anyone creates/edits/deletes a window (e.g. a
		// teammate enabling a maintenance). The poll remains for recurring windows that
		// activate purely by wall-clock and produce no DB event.
		let unsubscribe: (() => void) | undefined
		;(async () => {
			try {
				const handle = await pb.collection("maintenance").subscribe("*", scheduleLoad)
				if (cancelled) handle()
				else unsubscribe = handle
			} catch {
				// realtime is best-effort; the poll still covers changes
			}
		})()
		const interval = setInterval(load, POLL_MS)
		const onFocus = () => load()
		window.addEventListener("focus", onFocus)
		return () => {
			cancelled = true
			unsubscribe?.()
			if (debounce) clearTimeout(debounce)
			clearInterval(interval)
			window.removeEventListener("focus", onFocus)
		}
	}, [])

	if (windows.length === 0) return null

	// Show the most severe active window (ties broken by soonest end) so the title and
	// countdown match the bar colour, which is driven by that same window.
	const rank: Record<ActiveMaintenance["severity"], number> = { info: 0, warning: 1, critical: 2 }
	const primary = [...windows].sort((a, b) => {
		const bySeverity = rank[b.severity] - rank[a.severity]
		if (bySeverity !== 0) return bySeverity
		return (a.ends_at || "").localeCompare(b.ends_at || "")
	})[0]
	const severity = primary.severity
	const extra = windows.length - 1
	const until = primary.ends_at
		? t`until ${new Date(primary.ends_at).toLocaleTimeString([], { hour: "2-digit", minute: "2-digit" })}`
		: ""

	return (
		<div className={cn("border-b text-sm", TONES[severity])} role="status">
			<div className="container flex items-center gap-2 py-2">
				<WrenchIcon className="size-4 shrink-0" />
				<span className="font-medium shrink-0">
					<Trans>Maintenance in progress</Trans>
				</span>
				<span className="truncate text-muted-foreground/90">
					{primary.title}
					{primary.description ? ` — ${primary.description}` : ""}
					{until ? ` (${until})` : ""}
					{extra > 0 ? ` · +${extra}` : ""}
				</span>
			</div>
		</div>
	)
}
