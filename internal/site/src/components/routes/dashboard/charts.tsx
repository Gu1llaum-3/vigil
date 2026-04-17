import { Trans } from "@lingui/react/macro"
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
		security: "Security",
		needs_update: "Needs update",
		pending: "Pending",
		up_to_date: "Up to date",
	}

	const updateColors: Record<string, string> = {
		security: "#ef4444",
		needs_update: "#f59e0b",
		pending: "#94a3b8",
		up_to_date: "#22c55e",
	}

	const updateDist = summary.update_status_distribution ?? []
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
