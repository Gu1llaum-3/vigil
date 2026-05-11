import { Trans } from "@lingui/react/macro"
import {
	Chart as ChartJS,
	Legend,
	LineElement,
	LinearScale,
	PointElement,
	Tooltip,
	type ChartOptions,
} from "chart.js"
import { Line } from "react-chartjs-2"
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card"
import { formatBytesPerSecond, formatChartTime, formatPercent } from "@/lib/format"
import { cn } from "@/lib/utils"

ChartJS.register(LineElement, LinearScale, PointElement, Tooltip, Legend)

export type ChartPoint = { x: number; y: number }

export function buildSeries<T>(history: T[], xSelector: (point: T) => number, ySelector: (point: T) => number) {
	return history
		.map((point) => {
			const x = xSelector(point)
			if (!Number.isFinite(x)) return null
			return { x, y: ySelector(point) }
		})
		.filter((point): point is ChartPoint => Boolean(point))
}

export function buildTimeSeries<T extends { collected_at: string }>(history: T[], selector: (point: T) => number) {
	return buildSeries(history, (point) => new Date(point.collected_at).getTime(), selector)
}

export function MetricHistoryChart({
	title,
	points,
	formatter,
	color,
}: {
	title: React.ReactNode
	points: ChartPoint[]
	formatter: (value: number) => string
	color: string
}) {
	const chartData = {
		datasets: [
			{
				label: "metric",
				data: points,
				borderColor: color,
				borderWidth: 2,
				pointRadius: 2,
				pointHoverRadius: 4,
				tension: 0.2,
			},
		],
	}
	const options: ChartOptions<"line"> = {
		responsive: true,
		maintainAspectRatio: false,
		interaction: { mode: "index", intersect: false },
		plugins: {
			legend: { display: false },
			tooltip: {
				callbacks: {
					title(items) {
						const raw = items[0]?.parsed?.x
						return typeof raw === "number" ? formatChartTime(raw) : ""
					},
					label(context) {
						const raw = context.parsed?.y
						return typeof raw === "number" ? formatter(raw) : ""
					},
				},
			},
		},
		scales: {
			x: {
				type: "linear",
				grid: { display: false },
				ticks: {
					callback(value) {
						return typeof value === "number" ? formatChartTime(value) : value
					},
				},
			},
			y: {
				beginAtZero: true,
				grid: { color: "rgba(148, 163, 184, 0.15)" },
				ticks: {
					callback(value) {
						return typeof value === "number" ? formatter(value) : value
					},
				},
			},
		},
	}

	return (
		<Card>
			<CardHeader>
				<CardTitle className="text-base">{title}</CardTitle>
			</CardHeader>
			<CardContent>
				{points.length === 0 ? (
					<div className="flex h-64 items-center justify-center rounded-md border border-dashed border-border/60 text-sm text-muted-foreground">
						<Trans>No metrics yet.</Trans>
					</div>
				) : (
					<div className="h-64">
						<Line data={chartData} options={options} />
					</div>
				)}
			</CardContent>
		</Card>
	)
}

export function NetworkHistoryChart({ rxPoints, txPoints }: { rxPoints: ChartPoint[]; txPoints: ChartPoint[] }) {
	const data = {
		datasets: [
			{
				label: "rx",
				data: rxPoints,
				borderColor: "rgb(59, 130, 246)",
				backgroundColor: "rgba(59, 130, 246, 0.15)",
				borderWidth: 2,
				pointRadius: 2,
				pointHoverRadius: 4,
				tension: 0.2,
			},
			{
				label: "tx",
				data: txPoints,
				borderColor: "rgb(16, 185, 129)",
				backgroundColor: "rgba(16, 185, 129, 0.15)",
				borderWidth: 2,
				pointRadius: 2,
				pointHoverRadius: 4,
				tension: 0.2,
			},
		],
	}
	const options: ChartOptions<"line"> = {
		responsive: true,
		maintainAspectRatio: false,
		interaction: { mode: "index", intersect: false },
		plugins: {
			legend: { display: true, position: "bottom" },
			tooltip: {
				callbacks: {
					title(items) {
						const raw = items[0]?.parsed?.x
						return typeof raw === "number" ? formatChartTime(raw) : ""
					},
					label(context) {
						const raw = context.parsed?.y
						return typeof raw === "number" ? formatBytesPerSecond(raw) : ""
					},
				},
			},
		},
		scales: {
			x: {
				type: "linear",
				grid: { display: false },
				ticks: {
					callback(value) {
						return typeof value === "number" ? formatChartTime(value) : value
					},
				},
			},
			y: {
				beginAtZero: true,
				grid: { color: "rgba(148, 163, 184, 0.15)" },
				ticks: {
					callback(value) {
						return typeof value === "number" ? formatBytesPerSecond(value) : value
					},
				},
			},
		},
	}

	const hasData = rxPoints.length > 0 || txPoints.length > 0
	return (
		<Card>
			<CardHeader>
				<CardTitle className="text-base">
					<Trans>Network throughput</Trans>
				</CardTitle>
			</CardHeader>
			<CardContent>
				{!hasData ? (
					<div className="flex h-64 items-center justify-center rounded-md border border-dashed border-border/60 text-sm text-muted-foreground">
						<Trans>No metrics yet.</Trans>
					</div>
				) : (
					<div className="h-64">
						<Line data={data} options={options} />
					</div>
				)}
			</CardContent>
		</Card>
	)
}

export function MetricCard({
	title,
	value,
	icon,
	tone,
}: {
	title: React.ReactNode
	value: React.ReactNode
	icon: React.ReactNode
	tone?: string
}) {
	return (
		<Card className={cn("border-border/70", tone)}>
			<CardContent className="p-4">
				<div className="mb-2 text-muted-foreground">{icon}</div>
				<div className="text-2xl font-semibold tabular-nums">{value}</div>
				<div className="mt-1 text-xs text-muted-foreground">{title}</div>
			</CardContent>
		</Card>
	)
}

export function MetricBar({ value, tone = "emerald" }: { value?: number | null; tone?: "emerald" | "amber" }) {
	const percent = Math.max(0, Math.min(100, value ?? 0))
	const barClass = tone === "amber" ? "bg-amber-500/80" : "bg-emerald-500/80"
	return (
		<div className="flex min-w-[180px] items-center gap-3">
			<span className="w-12 shrink-0 text-xs font-medium tabular-nums">{formatPercent(value)}</span>
			<div className="h-2.5 flex-1 overflow-hidden rounded-full bg-muted">
				<div className={cn("h-full rounded-full transition-all", barClass)} style={{ width: `${percent}%` }} />
			</div>
		</div>
	)
}

export type MetricsRange = "1h" | "6h" | "24h" | "7d"

export const metricsRanges: { key: MetricsRange; label: string }[] = [
	{ key: "1h", label: "1h" },
	{ key: "6h", label: "6h" },
	{ key: "24h", label: "24h" },
	{ key: "7d", label: "7d" },
]
