import { useLingui } from "@lingui/react/macro"
import { ChartLineIcon } from "lucide-react"
import { memo, useCallback, useEffect, useRef, useState } from "react"
import { PageHeader } from "@/components/page-header"
import Spinner from "@/components/spinner"
import {
	buildTimeSeries,
	FleetMetricChart,
	type FleetSeries,
	type MetricsRange,
	metricsRanges,
} from "@/components/metric-charts"
import { Card, CardContent } from "@/components/ui/card"
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"
import { pb } from "@/lib/api"
import { formatPercent } from "@/lib/format"

type FleetMetric = "cpu" | "memory" | "disk" | "load"

// Raw series shape returned by GET /api/app/fleet-metrics/{metric}.
type FleetMetricResponse = { id: string; name: string; points: { collected_at: string; value: number }[] }

const loadFormatter = (v: number) => v.toFixed(2)

const FleetMetricsPage = memo(() => {
	const { t } = useLingui()
	const [metric, setMetric] = useState<FleetMetric>("cpu")
	const [range, setRange] = useState<MetricsRange>("6h")
	const [series, setSeries] = useState<FleetSeries[]>([])
	const [loading, setLoading] = useState(true)
	const [error, setError] = useState(false)
	const debounceRef = useRef<ReturnType<typeof setTimeout> | null>(null)

	const metricOptions: { key: FleetMetric; label: string }[] = [
		{ key: "cpu", label: t`CPU` },
		{ key: "memory", label: t`Memory` },
		{ key: "disk", label: t`Disk` },
		{ key: "load", label: t`Load (5m)` },
	]
	const formatter = metric === "load" ? loadFormatter : formatPercent

	const fetchSeries = useCallback(async () => {
		setError(false)
		try {
			const data = await pb.send<FleetMetricResponse[]>(`/api/app/fleet-metrics/${metric}?range=${range}`, {
				method: "GET",
			})
			setSeries(
				(data ?? []).map((s) => ({ id: s.id, name: s.name, points: buildTimeSeries(s.points, (p) => p.value) }))
			)
		} catch (e) {
			console.error("fleet metrics fetch failed", e)
			setError(true)
			setSeries([])
		} finally {
			setLoading(false)
		}
	}, [metric, range])

	// Refetch (with a loading indicator) whenever the metric or range changes.
	useEffect(() => {
		setLoading(true)
		fetchSeries()
	}, [fetchSeries])

	// Subscribe once to live metric writes; the debounced refresh always calls the
	// latest fetch (via a ref) so toggling metric/range never churns the socket.
	const fetchRef = useRef(fetchSeries)
	fetchRef.current = fetchSeries
	useEffect(() => {
		let cancelled = false
		let unsubscribe: (() => void) | undefined
		const refresh = () => {
			if (debounceRef.current) clearTimeout(debounceRef.current)
			debounceRef.current = setTimeout(() => fetchRef.current(), 1000)
		}
		pb.collection("host_metric_current")
			.subscribe("*", refresh)
			.then((unsub) => {
				if (cancelled) unsub()
				else unsubscribe = unsub
			})
			.catch((e) => console.error("fleet metrics subscribe failed", e))
		return () => {
			cancelled = true
			unsubscribe?.()
			if (debounceRef.current) clearTimeout(debounceRef.current)
		}
	}, [])

	return (
		<div className="space-y-5 pb-10">
			<PageHeader
				icon={ChartLineIcon}
				title={t`Metrics`}
				actions={
					<div className="flex gap-2">
						<Select value={metric} onValueChange={(v) => setMetric(v as FleetMetric)}>
							<SelectTrigger className="w-[140px]">
								<SelectValue />
							</SelectTrigger>
							<SelectContent>
								{metricOptions.map((opt) => (
									<SelectItem key={opt.key} value={opt.key}>
										{opt.label}
									</SelectItem>
								))}
							</SelectContent>
						</Select>
						<Select value={range} onValueChange={(v) => setRange(v as MetricsRange)}>
							<SelectTrigger className="w-[100px]">
								<SelectValue />
							</SelectTrigger>
							<SelectContent>
								{metricsRanges.map((r) => (
									<SelectItem key={r.key} value={r.key}>
										{r.label}
									</SelectItem>
								))}
							</SelectContent>
						</Select>
					</div>
				}
			/>
			{loading ? (
				<div className="flex min-h-72 items-center justify-center">
					<Spinner />
				</div>
			) : (
				<Card className="border-border/70">
					<CardContent className="pt-6">
						{error ? (
							<div className="flex h-[420px] items-center justify-center rounded-md border border-dashed border-border/60 text-sm text-muted-foreground">
								{t`Failed to load metrics.`}
							</div>
						) : (
							<FleetMetricChart series={series} formatter={formatter} />
						)}
					</CardContent>
				</Card>
			)}
		</div>
	)
})

export default FleetMetricsPage
