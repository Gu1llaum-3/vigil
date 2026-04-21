import { Trans, useLingui } from "@lingui/react/macro"
import { ArcElement, Chart as ChartJS, Legend, Tooltip } from "chart.js"
import { memo } from "react"
import { Doughnut } from "react-chartjs-2"
import { Card, CardContent } from "@/components/ui/card"
import type { DashboardSummary } from "@/lib/dashboard-types"

ChartJS.register(ArcElement, Tooltip, Legend)

interface ChartsProps {
	summary: DashboardSummary
}

const COLORS = ["#3b82f6", "#22c55e", "#f59e0b", "#ef4444", "#8b5cf6", "#06b6d4", "#f97316", "#84cc16"]

export const Charts = memo(function Charts({ summary }: ChartsProps) {
	const { t } = useLingui()

	const osData = {
		labels: (summary.os_distribution ?? []).map((e) => e.label),
		datasets: [
			{
				data: (summary.os_distribution ?? []).map((e) => e.value),
				backgroundColor: COLORS,
				borderWidth: 1,
			},
		],
	}

	const updateLabels: Record<string, string> = {
		reboot_required: t`Reboot required`,
		security_updates: t`Security updates`,
		stale_updates: t`Out of SLA (>30d)`,
		compliant: t`Compliant`,
		unknown: t`Unknown / Pending`,
	}

	const updateColors: Record<string, string> = {
		reboot_required: "#ef4444",
		security_updates: "#f97316",
		stale_updates: "#eab308",
		compliant: "#22c55e",
		unknown: "#94a3b8",
	}

	const preferredOrder = ["reboot_required", "security_updates", "stale_updates", "compliant", "unknown"]
	const updateDist = preferredOrder
		.map((key) => (summary.update_status_distribution ?? []).find((e) => e.label === key))
		.filter((e): e is NonNullable<typeof e> => Boolean(e))
		.concat((summary.update_status_distribution ?? []).filter((e) => !preferredOrder.includes(e.label)))
	const updateData = {
		labels: updateDist.map((e) => updateLabels[e.label] ?? e.label),
		datasets: [
			{
				data: updateDist.map((e) => e.value),
				backgroundColor: updateDist.map((e) => updateColors[e.label] ?? "#94a3b8"),
				borderWidth: 1,
			},
		],
	}

	const options = {
		responsive: true,
		maintainAspectRatio: false,
		plugins: {
			legend: {
				position: "bottom" as const,
				labels: { boxWidth: 12, padding: 12, font: { size: 12 } },
			},
		},
	}

	return (
		<div className="grid gap-4 md:grid-cols-2">
			<Card className="border-border/70">
				<CardContent className="p-5">
					<h3 className="mb-4 text-sm font-medium">
						<Trans>OS Distribution</Trans>
					</h3>
					<div className="h-52">
						{osData.labels.length > 0 ? (
							<Doughnut data={osData} options={options} />
						) : (
							<div className="flex h-full items-center justify-center text-sm text-muted-foreground">
								<Trans>No data</Trans>
							</div>
						)}
					</div>
				</CardContent>
			</Card>

			<Card className="border-border/70">
				<CardContent className="p-5">
					<h3 className="mb-4 text-sm font-medium">
						<Trans>Patch Status</Trans>
					</h3>
					<div className="h-52">
						{updateData.labels.length > 0 ? (
							<Doughnut data={updateData} options={options} />
						) : (
							<div className="flex h-full items-center justify-center text-sm text-muted-foreground">
								<Trans>No data</Trans>
							</div>
						)}
					</div>
				</CardContent>
			</Card>
		</div>
	)
})
