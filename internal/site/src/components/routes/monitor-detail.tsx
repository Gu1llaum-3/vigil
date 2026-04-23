import { Trans, useLingui } from "@lingui/react/macro"
import { getPagePath } from "@nanostores/router"
import { useStore } from "@nanostores/react"
import {
	ArrowLeftIcon,
	CheckCircle2Icon,
	Clock3Icon,
	GaugeIcon,
	XCircleIcon,
} from "lucide-react"
import {
	memo,
	useEffect,
	useMemo,
	useState,
} from "react"
import { Line } from "react-chartjs-2"
import {
	Chart as ChartJS,
	Legend,
	LineElement,
	LinearScale,
	PointElement,
	Tooltip,
	type ChartOptions,
	type Plugin,
} from "chart.js"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"
import { $router, Link } from "@/components/router"
import { pb } from "@/lib/api"
import type { MonitorEventRecord, MonitorGroupResponse, MonitorRecord, MonitorStatus } from "@/lib/monitor-types"

ChartJS.register(LineElement, LinearScale, PointElement, Tooltip, Legend)

type RangeKey = "1h" | "3h" | "6h" | "24h"

const ranges: { key: RangeKey; label: string; hours: number }[] = [
	{ key: "1h", label: "1h", hours: 1 },
	{ key: "3h", label: "3h", hours: 3 },
	{ key: "6h", label: "6h", hours: 6 },
	{ key: "24h", label: "24h", hours: 24 },
]

function formatLatencyMs(ms?: number): string {
	if (ms == null) return "N/A"
	return `${Math.round(ms * 10) / 10}ms`
}

function formatPercent(value?: number): string {
	if (value == null) return "N/A"
	return `${Math.round(value * 10) / 10}%`
}

function formatSeconds(value?: number): string {
	if (!value || value <= 0) return "—"
	return `${value}s`
}

function formatAge(ts?: string): string {
	if (!ts) return "—"
	const diff = Date.now() - new Date(ts).getTime()
	const s = Math.max(0, Math.floor(diff / 1000))
	if (s < 60) return `${s}s ago`
	if (s < 3600) return `${Math.floor(s / 60)}m ago`
	if (s < 86400) return `${Math.floor(s / 3600)}h ago`
	return `${Math.floor(s / 86400)}d ago`
}

function formatDateTime(value: number): string {
	return new Intl.DateTimeFormat(undefined, {
		month: "short",
		day: "numeric",
		hour: "2-digit",
		minute: "2-digit",
	}).format(new Date(value))
}

function formatAxisTick(value: number): string {
	return new Intl.DateTimeFormat(undefined, {
		month: "short",
		day: "numeric",
		hour: "2-digit",
	}).format(new Date(value))
}

function monitorTarget(monitor: MonitorRecord): string {
	switch (monitor.type) {
		case "http":
			return monitor.url || ""
		case "ping":
			return monitor.hostname || ""
		case "tcp":
			return monitor.hostname ? `${monitor.hostname}:${monitor.port}` : ""
		case "dns":
			return monitor.dns_host || ""
		case "push":
			return monitor.push_url || ""
		default:
			return ""
	}
}

function statusBadge(status: MonitorStatus) {
	if (status === 1) {
		return (
			<Badge className="bg-green-500/15 text-green-700 dark:text-green-400 border-green-500/30 gap-1">
				<CheckCircle2Icon className="h-3 w-3" />
				<Trans>Up</Trans>
			</Badge>
		)
	}
	if (status === 0) {
		return (
			<Badge className="bg-red-500/15 text-red-700 dark:text-red-400 border-red-500/30 gap-1">
				<XCircleIcon className="h-3 w-3" />
				<Trans>Down</Trans>
			</Badge>
		)
	}
	return (
		<Badge variant="outline" className="text-muted-foreground gap-1">
			<Clock3Icon className="h-3 w-3" />
			<Trans>Pending</Trans>
		</Badge>
	)
}

function createDownBandPlugin(bands: Array<{ start: number; end: number }>): Plugin<"line"> {
	return {
		id: "down-bands",
		beforeDatasetsDraw(chart) {
			const { ctx, chartArea, scales } = chart
			if (!chartArea || !bands.length) return
			const xScale = scales.x
			ctx.save()
			ctx.fillStyle = "rgba(239, 68, 68, 0.12)"
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

function buildSeries(events: MonitorEventRecord[]) {
	const sorted = [...events].sort((a, b) => new Date(a.checked_at).getTime() - new Date(b.checked_at).getTime())
	const chartEnd = Date.now()
	const points = sorted
		.map((event) => {
			const x = new Date(event.checked_at).getTime()
			return {
				x,
				y: event.status === 1 ? event.latency_ms : null,
				status: event.status,
				checkedAt: event.checked_at,
				msg: event.msg,
			}
		})
		.filter((point) => Number.isFinite(point.x))

	const downBands: Array<{ start: number; end: number }> = []
	let downStart: number | null = null
	for (const point of points) {
		if (point.status === 0 && downStart === null) {
			downStart = point.x
			continue
		}
		if (point.status !== 0 && downStart !== null) {
			downBands.push({ start: downStart, end: point.x })
			downStart = null
		}
	}
	if (downStart !== null && points.length > 0) {
		downBands.push({ start: downStart, end: chartEnd })
	}

	return { points, downBands, chartEnd }
}

const MonitorDetailPage = memo(function MonitorDetailPage() {
	const { t } = useLingui()
	const page = useStore($router)
	const monitorId = page?.params?.id as string | undefined
	const [range, setRange] = useState<RangeKey>("24h")
	const [monitor, setMonitor] = useState<MonitorRecord | null>(null)
	const [events, setEvents] = useState<MonitorEventRecord[]>([])
	const [loading, setLoading] = useState(true)
	const [error, setError] = useState<string | null>(null)

	useEffect(() => {
		document.title = monitor ? `${monitor.name} / Monitors` : `${t`Monitor`} / Monitors`
	}, [monitor, t])

	useEffect(() => {
		if (!monitorId) {
			setLoading(false)
			return
		}

		let cancelled = false
		async function loadData() {
			setLoading(true)
			setError(null)
			try {
				const groups = (await pb.send("/api/app/monitors", { method: "GET" })) as MonitorGroupResponse[]
				const detail = groups.flatMap((group) => group.monitors).find((item) => item.id === monitorId)
					?? ((await pb.send(`/api/app/monitors/${monitorId}`, { method: "GET" })) as MonitorRecord)
				if (cancelled) return
				setMonitor(detail)

				const selectedRange = ranges.find((item) => item.key === range) ?? ranges[0]
				const since = new Date(Date.now() - selectedRange.hours * 60 * 60 * 1000).toISOString()
				const intervalMs = Math.max((detail.interval || 60) * 1000, 60_000)
				const limit = Math.min(Math.max(Math.ceil((selectedRange.hours * 60 * 60 * 1000) / intervalMs) + 50, 250), 5000)
				const history = (await pb.send(
					`/api/app/monitors/${monitorId}/events?since=${encodeURIComponent(since)}&limit=${limit}`,
					{ method: "GET" },
				)) as MonitorEventRecord[]
				if (cancelled) return
				setEvents(history)
			} catch (err) {
				if (cancelled) return
				setError(err instanceof Error ? err.message : "Failed to load monitor")
			} finally {
				if (!cancelled) setLoading(false)
			}
		}

		loadData()
		return () => {
			cancelled = true
		}
	}, [monitorId, range])

	const series = useMemo(() => buildSeries(events), [events])
	const chartData = useMemo(
		() => ({
			datasets: [
				{
					label: "Latency",
					data: series.points,
					borderColor: "rgb(59, 130, 246)",
					backgroundColor: "rgba(59, 130, 246, 0.15)",
					pointBackgroundColor: "rgb(59, 130, 246)",
					pointBorderColor: "rgb(59, 130, 246)",
					borderWidth: 2,
					pointRadius: 2,
					pointHoverRadius: 4,
					spanGaps: false,
					tension: 0.2,
				},
			],
		}),
		[series.points],
	)
	const chartOptions = useMemo<ChartOptions<"line">>(
		() => ({
			responsive: true,
			maintainAspectRatio: false,
			interaction: { mode: "index", intersect: false },
			plugins: {
				legend: { display: false },
				tooltip: {
					callbacks: {
						title(items) {
							const raw = items[0]?.parsed?.x
							return typeof raw === "number" ? formatDateTime(raw) : ""
						},
						label(context) {
							const point = context.raw as { y?: number | null; status?: number }
							if (point.status === 0) return t`Down`
							if (point.y == null) return t`No latency`
							return `${t`Latency`}: ${formatLatencyMs(point.y)}`
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
							return typeof value === "number" ? formatAxisTick(value) : value
						},
					},
					min: series.points[0]?.x,
					max: series.chartEnd,
				},
				y: {
					beginAtZero: true,
					grid: { color: "rgba(148, 163, 184, 0.15)" },
					ticks: {
						callback(value) {
							return typeof value === "number" ? `${value} ms` : value
						},
					},
				},
			},
		}),
		[series.chartEnd, series.points, t],
	)
	const downBandPlugin = useMemo(() => createDownBandPlugin(series.downBands), [series.downBands])

	if (!monitorId) {
		return <div className="text-center py-10 text-muted-foreground">404</div>
	}

	if (loading && !monitor) {
		return <div className="py-10 text-center text-muted-foreground">Loading...</div>
	}

	if (error) {
		return (
			<Card className="pt-5 px-4 pb-8 min-h-96 mb-14 sm:pt-6 sm:px-7">
				<CardHeader className="p-0">
					<CardTitle>
						<Trans>Monitor details</Trans>
					</CardTitle>
					<CardDescription>{error}</CardDescription>
				</CardHeader>
				<CardContent className="p-0 mt-4">
					<Button asChild variant="outline">
						<Link href={getPagePath($router, "monitors")}>
							<ArrowLeftIcon className="me-2 h-4 w-4" />
							<Trans>Back to monitors</Trans>
						</Link>
					</Button>
				</CardContent>
			</Card>
		)
	}

	if (!monitor) {
		return <div className="py-10 text-center text-muted-foreground">404</div>
	}

	const target = monitorTarget(monitor)
	const canVisitTarget = monitor.type === "http" && /^https?:\/\//i.test(target)
	const latencyPoint = typeof monitor.last_latency_ms === "number" ? monitor.last_latency_ms : undefined

	return (
		<Card className="pt-5 px-4 pb-8 min-h-96 mb-14 sm:pt-6 sm:px-7">
			<CardHeader className="p-0">
				<div className="flex flex-col gap-4 md:flex-row md:items-start md:justify-between">
					<div className="min-w-0">
						<CardTitle className="flex items-center gap-3 text-2xl sm:text-3xl">
							{statusBadge(monitor.status)}
							<span className="truncate">{monitor.name}</span>
						</CardTitle>
						<CardDescription className="mt-2 max-w-3xl space-y-3">
							{canVisitTarget ? (
								<a
									href={target}
									target="_blank"
									rel="noreferrer"
									className="font-mono text-xs sm:text-sm break-all text-muted-foreground underline-offset-4 hover:text-foreground hover:underline"
								>
									{target}
								</a>
							) : (
								<div className="font-mono text-xs sm:text-sm break-all text-muted-foreground">{target || "—"}</div>
							)}
							<div className="grid gap-3 sm:grid-cols-2 xl:grid-cols-4 text-sm">
								<div>
									<div className="text-xs text-muted-foreground"><Trans>Type</Trans></div>
									<div className="font-mono uppercase text-foreground">{monitor.type || "—"}</div>
								</div>
								<div>
									<div className="text-xs text-muted-foreground"><Trans>Interval</Trans></div>
									<div className="text-foreground">{formatSeconds(monitor.interval)}</div>
								</div>
								<div>
									<div className="text-xs text-muted-foreground"><Trans>Timeout</Trans></div>
									<div className="text-foreground">{formatSeconds(monitor.timeout)}</div>
								</div>
								<div>
									<div className="text-xs text-muted-foreground"><Trans>Threshold</Trans></div>
									<div className="text-foreground">{monitor.failure_threshold ?? 3}</div>
								</div>
							</div>
						</CardDescription>
					</div>
					<div className="flex flex-wrap gap-2">
						<Button asChild variant="outline">
							<Link href={getPagePath($router, "monitors")}>
								<ArrowLeftIcon className="me-2 h-4 w-4" />
								<Trans>Back</Trans>
							</Link>
						</Button>
						<Select value={range} onValueChange={(value) => setRange(value as RangeKey)}>
							<SelectTrigger className="w-28">
								<SelectValue />
							</SelectTrigger>
							<SelectContent>
								{ranges.map((item) => (
									<SelectItem key={item.key} value={item.key}>
										{item.label}
									</SelectItem>
								))}
							</SelectContent>
						</Select>
					</div>
				</div>
			</CardHeader>

			<CardContent className="p-0 mt-6 space-y-6">
				<div className="grid gap-3 sm:grid-cols-2 xl:grid-cols-4">
					<div className="rounded-lg border bg-muted/20 p-4">
						<div className="text-xs text-muted-foreground"><Trans>Current latency</Trans></div>
						<div className="mt-1 text-2xl font-semibold">{formatLatencyMs(latencyPoint)}</div>
					</div>
					<div className="rounded-lg border bg-muted/20 p-4">
						<div className="text-xs text-muted-foreground"><Trans>Avg 24h</Trans></div>
						<div className="mt-1 text-2xl font-semibold">{formatLatencyMs(monitor.avg_latency_24h_ms)}</div>
					</div>
					<div className="rounded-lg border bg-muted/20 p-4">
						<div className="text-xs text-muted-foreground"><Trans>Uptime 24h</Trans></div>
						<div className="mt-1 text-2xl font-semibold">{formatPercent(monitor.uptime_24h)}</div>
					</div>
					<div className="rounded-lg border bg-muted/20 p-4">
						<div className="text-xs text-muted-foreground"><Trans>Uptime 30d</Trans></div>
						<div className="mt-1 text-2xl font-semibold">{formatPercent(monitor.uptime_30d)}</div>
					</div>
				</div>

				<div className="grid gap-3 sm:grid-cols-2 xl:grid-cols-4">
					<div className="rounded-lg border bg-muted/20 p-4">
						<div className="text-xs text-muted-foreground"><Trans>Last check</Trans></div>
						<div className="mt-1 text-sm font-medium">{monitor.last_checked_at ? formatDateTime(Date.parse(monitor.last_checked_at)) : "—"}</div>
						<div className="mt-1 text-xs text-muted-foreground">{formatAge(monitor.last_checked_at)}</div>
					</div>
					<div className="rounded-lg border bg-muted/20 p-4">
						<div className="text-xs text-muted-foreground"><Trans>Last message</Trans></div>
						<div className="mt-1 text-sm font-medium break-words">{monitor.last_msg || "—"}</div>
					</div>
					<div className="rounded-lg border bg-muted/20 p-4">
						<div className="text-xs text-muted-foreground"><Trans>Checks loaded</Trans></div>
						<div className="mt-1 text-sm font-medium">{events.length}</div>
					</div>
					<div className="rounded-lg border bg-muted/20 p-4">
						<div className="text-xs text-muted-foreground"><Trans>Status</Trans></div>
						<div className="mt-1">{statusBadge(monitor.status)}</div>
					</div>
				</div>

				<Card className="border-border/70">
					<CardHeader className="pb-3">
						<CardTitle className="text-sm font-medium flex items-center gap-2">
							<GaugeIcon className="h-4 w-4" />
							<Trans>Latency history</Trans>
						</CardTitle>
						<CardDescription>
							<Trans>Blue line for successful checks, red background for down periods.</Trans>
						</CardDescription>
					</CardHeader>
					<CardContent>
						<div className="h-[360px]">
							{series.points.length > 0 ? (
								<Line data={chartData} options={chartOptions} plugins={[downBandPlugin]} />
							) : (
								<div className="flex h-full items-center justify-center rounded-lg border border-dashed text-sm text-muted-foreground">
									<Trans>No history available.</Trans>
								</div>
							)}
						</div>
					</CardContent>
				</Card>
			</CardContent>
		</Card>
	)
})

export default MonitorDetailPage
