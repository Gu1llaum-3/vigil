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
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card"
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"
import { pb } from "@/lib/api"
import { formatPercent } from "@/lib/format"

type FleetMetric = "cpu" | "memory" | "disk" | "load"

const METRIC_KEYS: FleetMetric[] = ["cpu", "memory", "disk", "load"]

// Per-host series for one metric, within the GET /api/app/fleet-metrics response.
type RawSeries = { id: string; name: string; points: { collected_at: string; value: number }[] }
// The bulk endpoint returns every metric's series in one response, keyed by metric.
type FleetMetricsResponse = Record<FleetMetric, RawSeries[]>

const loadFormatter = (v: number) => v.toFixed(2)
const emptyByMetric = (): Record<FleetMetric, FleetSeries[]> => ({ cpu: [], memory: [], disk: [], load: [] })

const FleetMetricsPage = memo(() => {
	const { t } = useLingui()
	const [range, setRange] = useState<MetricsRange>("6h")
	const [seriesByMetric, setSeriesByMetric] = useState<Record<FleetMetric, FleetSeries[]>>(emptyByMetric)
	const [loading, setLoading] = useState(true)
	const [error, setError] = useState(false)
	const debounceRef = useRef<ReturnType<typeof setTimeout> | null>(null)

	// All four charts are rendered at once so the page can be skimmed without toggling a
	// metric selector; the range still applies to every chart.
	const metrics: { key: FleetMetric; label: string; formatter: (v: number) => string }[] = [
		{ key: "cpu", label: t`CPU`, formatter: formatPercent },
		{ key: "memory", label: t`Memory`, formatter: formatPercent },
		{ key: "disk", label: t`Disk`, formatter: formatPercent },
		{ key: "load", label: t`Load (5m)`, formatter: loadFormatter },
	]

	const fetchSeries = useCallback(async () => {
		setError(false)
		try {
			// One request → one scan of host_metric_samples for the whole window, all metrics.
			const data = await pb.send<FleetMetricsResponse>(`/api/app/fleet-metrics?range=${range}`, { method: "GET" })
			const next = emptyByMetric()
			for (const key of METRIC_KEYS) {
				next[key] = (data?.[key] ?? []).map((s) => ({
					id: s.id,
					name: s.name,
					points: buildTimeSeries(s.points, (p) => p.value),
				}))
			}
			setSeriesByMetric(next)
		} catch (e) {
			console.error("fleet metrics fetch failed", e)
			setError(true)
			setSeriesByMetric(emptyByMetric())
		} finally {
			setLoading(false)
		}
	}, [range])

	// Refetch (with a loading indicator) whenever the range changes.
	useEffect(() => {
		setLoading(true)
		fetchSeries()
	}, [fetchSeries])

	// Subscribe once to live metric writes; the debounced refresh always calls the latest
	// fetch (via a ref) so toggling the range never churns the socket.
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
				}
			/>
			{loading ? (
				<div className="flex min-h-72 items-center justify-center">
					<Spinner />
				</div>
			) : error ? (
				<div className="flex h-72 items-center justify-center rounded-md border border-dashed border-border/60 text-sm text-muted-foreground">
					{t`Failed to load metrics.`}
				</div>
			) : (
				<div className="grid gap-4">
					{metrics.map(({ key, label, formatter }) => (
						<Card key={key} className="border-border/70">
							<CardHeader className="pb-0">
								<CardTitle className="text-sm font-medium text-muted-foreground">{label}</CardTitle>
							</CardHeader>
							<CardContent className="pt-4">
								<FleetMetricChart series={seriesByMetric[key]} formatter={formatter} />
							</CardContent>
						</Card>
					))}
				</div>
			)}
		</div>
	)
})

export default FleetMetricsPage
