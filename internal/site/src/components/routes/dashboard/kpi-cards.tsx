import { Trans } from "@lingui/react/macro"
import { getPagePath } from "@nanostores/router"
import { ActivityIcon, AlertTriangleIcon, BoxIcon, RefreshCwIcon, ServerIcon, ShieldAlertIcon } from "lucide-react"
import { memo } from "react"
import { $router, Link } from "@/components/router"
import { Card, CardContent } from "@/components/ui/card"
import type { DashboardSummary } from "@/lib/dashboard-types"

interface KpiCardsProps {
	summary: DashboardSummary
	activeFilter: string | null
	onFilterChange: (filter: string | null) => void
	hasContainersSection: boolean
	onRunningContainersClick: () => void
}

interface KpiCardDef {
	key: string
	filterKey?: string
	label: React.ReactNode
	value: number | string
	icon: React.ReactNode
	variant: "default" | "warning" | "danger" | "success"
}

export const KpiCards = memo(function KpiCards({
	summary,
	activeFilter,
	onFilterChange,
	hasContainersSection,
	onRunningContainersClick,
}: KpiCardsProps) {
	const cards: KpiCardDef[] = [
		{
			key: "hosts",
			filterKey: "all",
			label: <Trans>Total hosts</Trans>,
			value: `${summary.connected_hosts}/${summary.total_hosts}`,
			icon: <ServerIcon className="size-4" />,
			variant: summary.connected_hosts === summary.total_hosts ? "success" : "danger",
		},
		{
			key: "security",
			filterKey: "security",
			label: <Trans>Security updates</Trans>,
			value: summary.total_security_updates,
			icon: <ShieldAlertIcon className="size-4" />,
			variant: summary.total_security_updates > 0 ? "danger" : "success",
		},
		{
			key: "outdated",
			filterKey: "outdated",
			label: <Trans>Outdated packages</Trans>,
			value: summary.total_outdated_packages,
			icon: <RefreshCwIcon className="size-4" />,
			variant: summary.total_outdated_packages > 0 ? "warning" : "success",
		},
		{
			key: "reboot",
			filterKey: "reboot",
			label: <Trans>Reboot required</Trans>,
			value: summary.hosts_needing_reboot,
			icon: <AlertTriangleIcon className="size-4" />,
			variant: summary.hosts_needing_reboot > 0 ? "warning" : "success",
		},
		{
			key: "docker",
			filterKey: "docker",
			label: <Trans>Running containers</Trans>,
			value: summary.running_containers,
			icon: <BoxIcon className="size-4" />,
			variant: "default",
		},
		{
			key: "monitors",
			label: <Trans>Monitors</Trans>,
			value: `${summary.up_monitors}/${summary.total_monitors}`,
			icon: <ActivityIcon className="size-4" />,
			variant:
				summary.total_monitors === 0
					? "default"
					: summary.up_monitors === summary.total_monitors
						? "success"
						: "danger",
		},
	]

	const variantClasses: Record<KpiCardDef["variant"], string> = {
		default: "border-border/70",
		warning: "border-amber-500/40 bg-amber-500/5",
		danger: "border-red-500/40 bg-red-500/5",
		success: "border-emerald-500/30",
	}

	const iconClasses: Record<KpiCardDef["variant"], string> = {
		default: "text-muted-foreground",
		warning: "text-amber-500",
		danger: "text-red-500",
		success: "text-emerald-500",
	}

	return (
		<div className="grid grid-cols-2 gap-3 sm:grid-cols-3 lg:grid-cols-6">
			{cards.map((item) => {
				const isRunningContainersCard = item.key === "docker"
				const isInteractive = item.filterKey !== undefined && !isRunningContainersCard
				const isNavigable = item.key === "monitors"
				const isClickable = isInteractive || isNavigable || (isRunningContainersCard && hasContainersSection)
				const isActive = isInteractive && activeFilter === item.filterKey
				const cardContent = (
					<Card
						className={`${isClickable ? "cursor-pointer" : "cursor-default"} transition-all ${variantClasses[item.variant]} ${
							isActive ? "ring-2 ring-primary" : isClickable ? "hover:border-primary/40" : ""
						}`}
						onClick={() => {
							if (isRunningContainersCard) {
								onRunningContainersClick()
								return
							}
							if (!isInteractive) return
							onFilterChange(isActive ? null : (item.filterKey ?? null))
						}}
					>
						<CardContent className="p-4">
							<div className={`mb-2 ${iconClasses[item.variant]}`}>{item.icon}</div>
							<div className="text-2xl font-bold tabular-nums">{item.value}</div>
							<div className="mt-0.5 text-xs text-muted-foreground">{item.label}</div>
						</CardContent>
					</Card>
				)

				if (item.key === "monitors") {
					return (
						<Link key={item.key} href={getPagePath($router, "monitors")} className="block">
							{cardContent}
						</Link>
					)
				}

				return <div key={item.key}>{cardContent}</div>
			})}
		</div>
	)
})
