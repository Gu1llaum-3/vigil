import { Plural, Trans, useLingui } from "@lingui/react/macro"
import { useStore } from "@nanostores/react"
import { getPagePath } from "@nanostores/router"
import {
	ArrowLeftIcon,
	BoxIcon,
	CpuIcon,
	HardDriveIcon,
	Loader2Icon,
	NetworkIcon,
	RefreshCwIcon,
	ServerIcon,
} from "lucide-react"
import { memo, useCallback, useEffect, useMemo, useRef, useState } from "react"
import {
	buildTimeSeries,
	MetricCard,
	MetricHistoryChart,
	type MetricsRange,
	metricsRanges,
	NetworkHistoryChart,
} from "@/components/metric-charts"
import { $router, Link } from "@/components/router"
import Spinner from "@/components/spinner"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card"
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs"
import { toast } from "@/components/ui/use-toast"
import { isAdmin, pb } from "@/lib/api"
import { containerSeverity, isStoppedContainerStatus } from "@/lib/container-status"
import type { ContainerFleetEntry, ContainerImageAudit } from "@/lib/dashboard-types"
import { formatBytesCompact, formatBytesPerSecond, formatDateTime, formatPercent } from "@/lib/format"
import { cn } from "@/lib/utils"
import { useDashboardData } from "./dashboard/use-dashboard-data"

interface ContainerMetricSeriesPoint {
	collected_at: string
	cpu_percent: number
	memory_used_bytes: number
	memory_limit_bytes: number
	network_rx_bps: number
	network_tx_bps: number
}

function useContainerStatusLabel(status: string): string {
	const { t } = useLingui()
	switch (status) {
		case "running":
			return t`Running`
		case "exited":
			return t`Exited`
		case "restarting":
			return t`Restarting`
		case "paused":
			return t`Paused`
		case "created":
			return t`Created`
		case "dead":
			return t`Dead`
		default:
			return t`Unknown`
	}
}

function ContainerStatusBadge({ container }: { container: ContainerFleetEntry }) {
	const label = useContainerStatusLabel(container.status)
	const severity = containerSeverity(container)
	const cls =
		severity === "ok"
			? "border-emerald-500/30 bg-emerald-500/10 text-emerald-400"
			: severity === "warning"
				? "border-amber-500/30 bg-amber-500/10 text-amber-400"
				: severity === "error"
					? "border-red-500/30 bg-red-500/10 text-red-400"
					: "border-border/50 text-muted-foreground"
	return (
		<Badge variant="outline" className={cn(cls)}>
			{label}
		</Badge>
	)
}

function useAuditLineLabel(audit: ContainerImageAudit | null | undefined): string {
	const { t } = useLingui()
	if (!audit) return ""
	const ls = audit.line_status || audit.status
	if (ls === "patch_available") return t`Patch available`
	if (ls === "minor_available") return t`Minor available`
	if (ls === "tag_rebuilt") return t`Tag rebuilt`
	if (audit.status === "update_available") return t`Update available`
	if (audit.status === "up_to_date" || ls === "up_to_date") return t`Up to date`
	if (audit.status === "check_failed") return t`Check failed`
	if (audit.status === "unsupported") return t`Unsupported`
	if (audit.status === "disabled") return t`Disabled`
	return t`Unknown`
}

function InfoRow({ label, value, mono }: { label: React.ReactNode; value: React.ReactNode; mono?: boolean }) {
	return (
		<div className="grid grid-cols-[10rem_1fr] gap-2 text-sm">
			<span className="text-muted-foreground">{label}</span>
			<span className={cn("break-all", mono && "font-mono text-xs")}>{value || "—"}</span>
		</div>
	)
}

export default memo(function ContainerDetailPage() {
	const { t } = useLingui()
	const page = useStore($router)
	const hostId = (page?.params as { hostId?: string } | undefined)?.hostId ?? ""
	const name = (page?.params as { name?: string } | undefined)?.name ?? ""
	const decodedName = useMemo(() => {
		try {
			return decodeURIComponent(name)
		} catch {
			return name
		}
	}, [name])

	const { dashboard, loading } = useDashboardData()
	const [metricsRange, setMetricsRange] = useState<MetricsRange>("24h")
	const [latest, setLatest] = useState<ContainerMetricSeriesPoint | null>(null)
	const [history, setHistory] = useState<ContainerMetricSeriesPoint[]>([])
	const latestRequestRef = useRef(0)
	const historyRequestRef = useRef(0)
	const debounceRef = useRef<ReturnType<typeof setTimeout> | null>(null)

	const container = useMemo(
		() =>
			(dashboard?.containers ?? []).find(
				(entry) => entry.host_id === hostId && (entry.name === decodedName || entry.name === `/${decodedName}`)
			),
		[dashboard, hostId, decodedName]
	)

	const hostName = container?.host_name || hostId

	const loadLatest = useCallback(async () => {
		if (!hostId || !decodedName) return
		const requestId = ++latestRequestRef.current
		try {
			const data = await pb.send<ContainerMetricSeriesPoint | null>(
				`/api/app/hosts/${hostId}/container-metrics/by-name/${encodeURIComponent(decodedName)}/latest`,
				{ method: "GET" }
			)
			if (requestId === latestRequestRef.current) setLatest(data ?? null)
		} catch (error) {
			if (requestId === latestRequestRef.current) {
				console.error("container latest metrics fetch failed", error)
				setLatest(null)
			}
		}
	}, [hostId, decodedName])

	const loadHistory = useCallback(async () => {
		if (!hostId || !decodedName) return
		const requestId = ++historyRequestRef.current
		try {
			const data = await pb.send<ContainerMetricSeriesPoint[]>(
				`/api/app/hosts/${hostId}/container-metrics/by-name/${encodeURIComponent(decodedName)}?range=${metricsRange}`,
				{ method: "GET" }
			)
			if (requestId === historyRequestRef.current) setHistory(data ?? [])
		} catch (error) {
			if (requestId === historyRequestRef.current) {
				console.error("container history metrics fetch failed", error)
				setHistory([])
			}
		}
	}, [hostId, decodedName, metricsRange])

	useEffect(() => {
		loadLatest()
	}, [loadLatest])

	useEffect(() => {
		loadHistory()
	}, [loadHistory])

	useEffect(() => {
		if (!hostId) return
		const unsubscribes: Array<() => void> = []
		const refresh = () => {
			if (debounceRef.current) clearTimeout(debounceRef.current)
			debounceRef.current = setTimeout(() => {
				loadLatest()
				loadHistory()
			}, 1000)
		}
		;(async () => {
			unsubscribes.push(await pb.collection("container_metric_samples").subscribe("*", refresh))
			unsubscribes.push(await pb.collection("host_snapshots").subscribe("*", refresh))
			unsubscribes.push(await pb.collection("container_image_audits").subscribe("*", refresh))
		})()
		return () => {
			for (const u of unsubscribes) u()
			if (debounceRef.current) clearTimeout(debounceRef.current)
		}
	}, [hostId, loadHistory, loadLatest])

	useEffect(() => {
		document.title = `${decodedName || t`Container`} / Vigil`
	}, [decodedName, t])

	const cpuPoints = useMemo(() => buildTimeSeries(history, (p) => p.cpu_percent), [history])
	const memoryPoints = useMemo(
		() =>
			buildTimeSeries(history, (p) =>
				p.memory_limit_bytes > 0 ? (p.memory_used_bytes / p.memory_limit_bytes) * 100 : 0
			),
		[history]
	)
	const rxPoints = useMemo(() => buildTimeSeries(history, (p) => p.network_rx_bps), [history])
	const txPoints = useMemo(() => buildTimeSeries(history, (p) => p.network_tx_bps), [history])

	const cpuNow = latest?.cpu_percent
	const memoryNow =
		latest && latest.memory_limit_bytes > 0
			? (latest.memory_used_bytes / latest.memory_limit_bytes) * 100
			: undefined
	const netNow = latest?.network_rx_bps ?? 0
	const statusLabel = useContainerStatusLabel(container?.status ?? "")

	if (loading) {
		return (
			<div className="flex min-h-72 items-center justify-center">
				<Spinner />
			</div>
		)
	}

	if (!container) {
		return (
			<div className="space-y-4 py-6">
				<Button variant="outline" asChild>
					<Link href={getPagePath($router, "containers")}>
						<ArrowLeftIcon className="me-2 size-4" />
						<Trans>Back to containers</Trans>
					</Link>
				</Button>
				<div className="rounded-lg border border-dashed border-border/60 p-10 text-center text-muted-foreground">
					<Trans>Container not found.</Trans>
				</div>
			</div>
		)
	}

	const audit = container.image_audit ?? null
	const imageRef = audit?.current_ref || container.image_ref || container.image || ""

	return (
		<div className="space-y-6 pb-10">
			<div className="space-y-4">
				<Button variant="ghost" asChild className="px-0 text-muted-foreground hover:text-foreground">
					<Link href={getPagePath($router, "containers")}>
						<ArrowLeftIcon className="me-2 size-4" />
						<Trans>Back to containers</Trans>
					</Link>
				</Button>
				<div className="flex flex-col gap-3 sm:flex-row sm:items-start sm:justify-between">
					<div>
						<div className="flex flex-wrap items-center gap-3">
							<h1 className="text-2xl font-semibold tracking-tight">{decodedName}</h1>
							<ContainerStatusBadge container={container} />
						</div>
						<div className="mt-1 flex flex-wrap gap-x-3 gap-y-1 text-sm text-muted-foreground">
							<Link href={getPagePath($router, "host", { id: hostId })} className="hover:underline">
								{hostName}
							</Link>
							<span className="font-mono">{imageRef || "—"}</span>
							{container.ports && <span className="font-mono">{container.ports}</span>}
						</div>
					</div>
				</div>
			</div>

			<div className="grid gap-3 sm:grid-cols-2 lg:grid-cols-4">
				<MetricCard
					title={<Trans>Status</Trans>}
					value={statusLabel}
					icon={<BoxIcon className="size-4" />}
				/>
				<MetricCard
					title={<Trans>CPU now</Trans>}
					value={formatPercent(cpuNow)}
					icon={<CpuIcon className="size-4" />}
				/>
				<MetricCard
					title={<Trans>Memory now</Trans>}
					value={
						memoryNow != null
							? formatPercent(memoryNow)
							: latest
								? formatBytesCompact(latest.memory_used_bytes)
								: "—"
					}
					icon={<HardDriveIcon className="size-4" />}
				/>
				<MetricCard
					title={<Trans>Network now</Trans>}
					value={`${formatBytesPerSecond(netNow)} ↓`}
					icon={<NetworkIcon className="size-4" />}
				/>
			</div>

			<Tabs defaultValue="monitoring" className="space-y-4">
				<div className="overflow-x-auto">
					<TabsList>
						<TabsTrigger value="monitoring">
							<Trans>Monitoring</Trans>
						</TabsTrigger>
						<TabsTrigger value="image-audit">
							<Trans>Image audit</Trans>
						</TabsTrigger>
						<TabsTrigger value="inspect">
							<Trans>Inspect</Trans>
						</TabsTrigger>
					</TabsList>
				</div>

				<TabsContent value="monitoring" className="space-y-4">
					<div className="flex justify-end">
						<Select value={metricsRange} onValueChange={(v) => setMetricsRange(v as MetricsRange)}>
							<SelectTrigger className="w-[110px]">
								<SelectValue />
							</SelectTrigger>
							<SelectContent>
								{metricsRanges.map((range) => (
									<SelectItem key={range.key} value={range.key}>
										{range.label}
									</SelectItem>
								))}
							</SelectContent>
						</Select>
					</div>
					<div className="grid gap-4 xl:grid-cols-2">
						<MetricHistoryChart
							title={<Trans>CPU usage</Trans>}
							points={cpuPoints}
							formatter={(v) => formatPercent(v)}
							color="rgb(59, 130, 246)"
						/>
						<MetricHistoryChart
							title={<Trans>Memory usage</Trans>}
							points={memoryPoints}
							formatter={(v) => formatPercent(v)}
							color="rgb(16, 185, 129)"
						/>
						<NetworkHistoryChart rxPoints={rxPoints} txPoints={txPoints} />
					</div>
				</TabsContent>

				<TabsContent value="image-audit">
					<ImageAuditPane container={container} onChanged={loadLatest} />
				</TabsContent>

				<TabsContent value="inspect">
					<InspectPane container={container} />
				</TabsContent>
			</Tabs>
		</div>
	)
})

function ImageAuditPane({ container, onChanged }: { container: ContainerFleetEntry; onChanged: () => void }) {
	const { t } = useLingui()
	const admin = isAdmin()
	const audit = container.image_audit
	const auditLineLabel = useAuditLineLabel(audit)
	const [auditing, setAuditing] = useState(false)

	const runAuditNow = useCallback(async () => {
		setAuditing(true)
		try {
			await pb.send("/api/app/jobs/vigilContainerImageAudit/run", { method: "POST" })
			toast({ title: t`Image audit completed` })
			onChanged()
		} catch (error: unknown) {
			toast({
				title: t`Failed to run image audit`,
				description: (error as Error).message,
				variant: "destructive",
			})
		} finally {
			setAuditing(false)
		}
	}, [t, onChanged])

	const upsertOverride = useCallback(
		async (payload: { policy: string; tag_include?: string; tag_exclude?: string }) => {
			try {
				await pb.send("/api/app/container-audit-overrides", {
					method: "PUT",
					body: JSON.stringify({
						agent: container.host_id,
						container_name: container.name,
						policy: payload.policy,
						tag_include: payload.tag_include ?? "",
						tag_exclude: payload.tag_exclude ?? "",
					}),
					headers: { "Content-Type": "application/json" },
				})
				toast({ title: t`Audit policy updated` })
				onChanged()
			} catch (error: unknown) {
				toast({
					title: t`Failed to update audit policy`,
					description: (error as Error).message,
					variant: "destructive",
				})
			}
		},
		[container.host_id, container.name, t, onChanged]
	)

	const pin = useCallback(() => {
		if (!audit) return
		upsertOverride({
			policy: "patch",
			tag_include: `^${audit.tag.replace(/[.+?^${}()|[\]\\]/g, "\\$&")}$`,
		})
	}, [audit, upsertOverride])

	const disableAudit = useCallback(() => {
		upsertOverride({ policy: "disabled" })
	}, [upsertOverride])

	if (!audit) {
		return (
			<Card>
				<CardContent className="space-y-3 py-8 text-center text-sm text-muted-foreground">
					<Trans>No image audit data for this container yet.</Trans>
					{admin && (
						<div>
							<Button variant="outline" size="sm" disabled={auditing} onClick={runAuditNow}>
								{auditing ? (
									<Loader2Icon className="me-2 size-4 animate-spin" />
								) : (
									<RefreshCwIcon className="me-2 size-4" />
								)}
								<Trans>Check images now</Trans>
							</Button>
						</div>
					)}
				</CardContent>
			</Card>
		)
	}

	const lineLatest = audit.line_latest_tag || audit.latest_tag || ""
	const sameMajor = audit.same_major_latest_tag || ""
	const overall = audit.overall_latest_tag || ""

	return (
		<div className="grid gap-4 lg:grid-cols-2">
			<Card>
				<CardHeader>
					<CardTitle className="text-base">
						<Trans>Status</Trans>
					</CardTitle>
				</CardHeader>
				<CardContent className="space-y-2">
					<div className="flex flex-wrap items-center gap-2">
						<Badge variant="outline" className="text-[10px] uppercase">
							{auditLineLabel}
						</Badge>
						<Badge variant="secondary" className="text-[10px]">
							{audit.policy || "auto"}
						</Badge>
						<span className="text-xs text-muted-foreground">
							<Trans>Checked</Trans>: {formatDateTime(audit.checked_at)}
						</span>
					</div>
					{audit.error && (
						<p className="text-xs text-red-400">
							<Trans>Error</Trans>: <span className="font-mono">{audit.error}</span>
						</p>
					)}
				</CardContent>
			</Card>

			<Card>
				<CardHeader>
					<CardTitle className="text-base">
						<Trans>Versions</Trans>
					</CardTitle>
				</CardHeader>
				<CardContent className="space-y-2">
					<InfoRow label={<Trans>Current</Trans>} value={audit.tag} mono />
					{lineLatest && lineLatest !== audit.tag && (
						<InfoRow label={<Trans>Latest in line</Trans>} value={lineLatest} mono />
					)}
					{sameMajor && sameMajor !== lineLatest && (
						<InfoRow label={<Trans>Latest same major</Trans>} value={sameMajor} mono />
					)}
					{overall && overall !== sameMajor && overall !== lineLatest && (
						<InfoRow label={<Trans>Latest overall</Trans>} value={overall} mono />
					)}
					{audit.major_update_available && audit.new_major_tag && (
						<InfoRow label={<Trans>New major available</Trans>} value={audit.new_major_tag} mono />
					)}
				</CardContent>
			</Card>

			<Card>
				<CardHeader>
					<CardTitle className="text-base">
						<Trans>Digests</Trans>
					</CardTitle>
				</CardHeader>
				<CardContent className="space-y-2">
					<InfoRow label={<Trans>Local</Trans>} value={audit.local_digest} mono />
					<InfoRow label={<Trans>Latest</Trans>} value={audit.latest_digest} mono />
				</CardContent>
			</Card>

			<Card>
				<CardHeader>
					<CardTitle className="text-base">
						<Trans>Source</Trans>
					</CardTitle>
				</CardHeader>
				<CardContent className="space-y-2">
					<InfoRow label={<Trans>Image ref</Trans>} value={audit.current_ref} mono />
					<InfoRow label={<Trans>Registry</Trans>} value={audit.registry} mono />
					<InfoRow label={<Trans>Repository</Trans>} value={audit.repository} mono />
				</CardContent>
			</Card>

			{admin && (
				<Card className="lg:col-span-2">
					<CardContent className="flex flex-wrap items-center gap-2 py-4">
						<Button variant="outline" size="sm" disabled={auditing} onClick={runAuditNow}>
							{auditing ? (
								<Loader2Icon className="me-2 size-4 animate-spin" />
							) : (
								<RefreshCwIcon className="me-2 size-4" />
							)}
							<Trans>Check images now</Trans>
						</Button>
						<Button variant="outline" size="sm" onClick={pin}>
							<Trans>Pin to current tag</Trans>
						</Button>
						<Button variant="outline" size="sm" onClick={disableAudit}>
							<Trans>Disable audit</Trans>
						</Button>
					</CardContent>
				</Card>
			)}
		</div>
	)
}

function InspectPane({ container }: { container: ContainerFleetEntry }) {
	const exitInfo =
		isStoppedContainerStatus(container.status) && container.exit_code != null ? String(container.exit_code) : null
	return (
		<Card>
			<CardHeader>
				<CardTitle className="text-base">
					<Trans>Container details</Trans>
				</CardTitle>
			</CardHeader>
			<CardContent className="space-y-2">
				<InfoRow label={<Trans>ID</Trans>} value={container.id} mono />
				<InfoRow label={<Trans>Host</Trans>} value={container.host_name || container.host_id} />
				<InfoRow label={<Trans>Image</Trans>} value={container.image} mono />
				<InfoRow label={<Trans>Image ref</Trans>} value={container.image_ref} mono />
				<InfoRow label={<Trans>Image ID</Trans>} value={container.image_id} mono />
				<InfoRow label={<Trans>State</Trans>} value={container.status} />
				<InfoRow label={<Trans>State detail</Trans>} value={container.status_text} />
				{exitInfo && <InfoRow label={<Trans>Exit code</Trans>} value={exitInfo} />}
				<InfoRow label={<Trans>Ports</Trans>} value={container.ports} mono />
				{container.repo_digests && container.repo_digests.length > 0 && (
					<InfoRow
						label={<Plural value={container.repo_digests.length} one="Repo digest" other="Repo digests" />}
						value={container.repo_digests.join("\n")}
						mono
					/>
				)}
			</CardContent>
		</Card>
	)
}
