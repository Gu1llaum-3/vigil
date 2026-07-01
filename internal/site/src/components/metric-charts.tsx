import { Trans } from "@lingui/react/macro"
import {
	Chart as ChartJS,
	Filler,
	Legend,
	LineElement,
	LinearScale,
	PointElement,
	type ChartOptions,
	type Plugin,
	type ScriptableContext,
	Tooltip,
} from "chart.js"
import { Line } from "react-chartjs-2"
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card"
import { formatBytesPerSecond, formatChartTime, formatPercent } from "@/lib/format"
import { cn } from "@/lib/utils"

ChartJS.register(LineElement, LinearScale, PointElement, Filler, Tooltip, Legend)

export type ChartPoint = { x: number; y: number }

export type ChartBand = { start: number; end: number }

// createBandPlugin shades the chart background over the given x-ranges in `color` (e.g. red
// down / amber pending / blue maintenance bands). Each plugin instance needs a unique `id` so
// Chart.js can register several at once; list them bottom-to-top in the chart's `plugins`
// array. Exported from here so any chart (monitor or host) can reuse it.
export function createBandPlugin(id: string, color: string, bands: ChartBand[]): Plugin<"line"> {
	return {
		id,
		beforeDatasetsDraw(chart) {
			const { ctx, chartArea, scales } = chart
			if (!chartArea || !bands.length) return
			const xScale = scales.x
			ctx.save()
			ctx.fillStyle = color
			for (const band of bands) {
				const start = xScale.getPixelForValue(band.start)
				const end = xScale.getPixelForValue(band.end)
				const left = Math.min(start, end)
				const width = Math.max(1, Math.abs(end - start))
				ctx.fillRect(left, chartArea.top, width, chartArea.bottom - chartArea.top)
			}
			ctx.restore()
		},
	}
}

// areaFill builds a scriptable vertical gradient (color → transparent) for the
// filled area under a line, matching the Beszel-style area look.
export function areaFill(color: string, topOpacity = 0.35) {
	const rgba = (alpha: number) => color.replace("rgb(", "rgba(").replace(")", `, ${alpha})`)
	return (ctx: ScriptableContext<"line">) => {
		const { chartArea, ctx: canvas } = ctx.chart
		// chartArea is undefined on the first render pass before layout.
		if (!chartArea) return rgba(0)
		const gradient = canvas.createLinearGradient(0, chartArea.top, 0, chartArea.bottom)
		gradient.addColorStop(0, rgba(topOpacity))
		gradient.addColorStop(1, rgba(0))
		return gradient
	}
}

// Shared line styling for the smooth, filled, dotless Beszel-style charts.
const areaLineStyle = {
	borderWidth: 1.5,
	fill: true as const,
	pointRadius: 0,
	pointHoverRadius: 4,
	pointHitRadius: 8,
	cubicInterpolationMode: "monotone" as const,
}

// buildLineChartOptions is the shared Chart.js options for the time-series line
// charts: linear time x-axis, zero-based y-axis, index-mode tooltip. `formatter`
// formats both the y ticks and the tooltip value; `legend` toggles the bottom
// legend and (when on) prefixes each tooltip with the dataset label.
function buildLineChartOptions(formatter: (value: number) => string, legend = false): ChartOptions<"line"> {
	return {
		responsive: true,
		maintainAspectRatio: false,
		interaction: { mode: "index", intersect: false },
		plugins: {
			legend: { display: legend, position: "bottom" },
			tooltip: {
				callbacks: {
					title(items) {
						const raw = items[0]?.parsed?.x
						return typeof raw === "number" ? formatChartTime(raw) : ""
					},
					label(context) {
						const raw = context.parsed?.y
						if (typeof raw !== "number") return ""
						return legend ? `${context.dataset.label}: ${formatter(raw)}` : formatter(raw)
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
}

// seriesColor returns a deterministic, well-spread hue per series index
// (golden-angle spacing) so any number of host lines stay visually distinct.
function seriesColor(index: number): string {
	return `hsl(${(index * 137.508) % 360}, 65%, 55%)`
}

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
				backgroundColor: areaFill(color),
				...areaLineStyle,
			},
		],
	}
	const options = buildLineChartOptions(formatter)

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
				backgroundColor: areaFill("rgb(59, 130, 246)", 0.18),
				...areaLineStyle,
			},
			{
				label: "tx",
				data: txPoints,
				borderColor: "rgb(16, 185, 129)",
				backgroundColor: areaFill("rgb(16, 185, 129)", 0.18),
				...areaLineStyle,
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

// LoadHistoryChart plots the 1/5/15-minute load averages as three lines (no fill, to keep
// three overlapping series readable). Values are already normalized to load-per-core by the
// caller so 1.0 = fully utilized — the same scale the load alert threshold uses.
export function LoadHistoryChart({
	oneMin,
	fiveMin,
	fifteenMin,
}: {
	oneMin: ChartPoint[]
	fiveMin: ChartPoint[]
	fifteenMin: ChartPoint[]
}) {
	const lineStyle = {
		borderWidth: 1.5,
		fill: false as const,
		pointRadius: 0,
		pointHoverRadius: 4,
		pointHitRadius: 8,
		cubicInterpolationMode: "monotone" as const,
	}
	const fmt = (v: number) => `${v.toFixed(2)}/core`
	const data = {
		datasets: [
			// 5 min is the line the alert evaluates, so it gets the prominent color.
			{ label: "5m", data: fiveMin, borderColor: "rgb(59, 130, 246)", ...lineStyle },
			{ label: "1m", data: oneMin, borderColor: "rgb(148, 163, 184)", ...lineStyle },
			{ label: "15m", data: fifteenMin, borderColor: "rgb(16, 185, 129)", ...lineStyle },
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
						return typeof raw === "number" ? `${context.dataset.label}: ${fmt(raw)}` : ""
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
						return typeof value === "number" ? value.toFixed(2) : value
					},
				},
			},
		},
	}

	const hasData = oneMin.length > 0 || fiveMin.length > 0 || fifteenMin.length > 0
	return (
		<Card>
			<CardHeader>
				<CardTitle className="text-base">
					<Trans>Load average (per core)</Trans>
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

export type FleetSeries = { id: string; name: string; points: ChartPoint[] }

// FleetMetricChart renders one metric as a line per host (no fill, for legibility
// with many series). The page owns the surrounding card/controls.
export function FleetMetricChart({
	series,
	formatter,
}: {
	series: FleetSeries[]
	formatter: (value: number) => string
}) {
	const lineStyle = {
		borderWidth: 1.5,
		fill: false as const,
		pointRadius: 0,
		pointHoverRadius: 4,
		pointHitRadius: 8,
		cubicInterpolationMode: "monotone" as const,
	}
	const data = {
		datasets: series.map((s, i) => ({
			label: s.name,
			data: s.points,
			borderColor: seriesColor(i),
			...lineStyle,
		})),
	}
	const options = buildLineChartOptions(formatter, true)

	const hasData = series.some((s) => s.points.length > 0)
	if (!hasData) {
		return (
			<div className="flex h-[420px] items-center justify-center rounded-md border border-dashed border-border/60 text-sm text-muted-foreground">
				<Trans>No metrics yet.</Trans>
			</div>
		)
	}
	return (
		<div className="h-[420px]">
			<Line data={data} options={options} />
		</div>
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

export function MetricBar({ value, tone = "emerald" }: { value?: number | null; tone?: "emerald" | "amber" | "red" }) {
	const percent = Math.max(0, Math.min(100, value ?? 0))
	const barClass = tone === "red" ? "bg-red-500/80" : tone === "amber" ? "bg-amber-500/80" : "bg-emerald-500/80"
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
