import { Plural, Trans, useLingui } from "@lingui/react/macro"
import { useStore } from "@nanostores/react"
import { getPagePath } from "@nanostores/router"
import {
	ArrowLeftIcon,
	BoxIcon,
	CheckCircle2Icon,
	CpuIcon,
	HardDriveIcon,
	NetworkIcon,
	ServerIcon,
	ShieldAlertIcon,
} from "lucide-react"
import { useCallback, useEffect, useMemo, useRef, useState } from "react"
import { $router, Link } from "@/components/router"
import Spinner from "@/components/spinner"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card"
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs"
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table"
import {
	buildTimeSeries,
	MetricBar,
	MetricCard,
	MetricHistoryChart,
	type MetricsRange,
	metricsRanges,
	NetworkHistoryChart,
} from "@/components/metric-charts"
import { cn } from "@/lib/utils"
import { pb } from "@/lib/api"
import type { HostMetrics, HostsOverviewRecord } from "@/lib/dashboard-types"
import type { ContainerMetricsHistoryPoint } from "@/lib/dashboard-types"
import {
	formatBytes,
	formatBytesCompact,
	formatBytesPerSecond,
	formatDateTime,
	formatPercent,
	formatRam,
	formatStorageValue,
	formatUptime,
} from "@/lib/format"
import { type ContainersFilters, defaultContainersFilters } from "./dashboard/containers-filter-sheet"
import { ContainersTable } from "./dashboard/containers-table"
import { useDashboardData } from "./dashboard/use-dashboard-data"

function statusBadge(status: string) {
	return status === "connected" ? (
		<Badge variant="outline" className="border-emerald-500/40 bg-emerald-500/10 text-emerald-500">
			UP
		</Badge>
	) : (
		<Badge variant="outline" className="border-border/50 text-muted-foreground">
			DOWN
		</Badge>
	)
}

function isImageUpdateAvailable(status: string) {
	return status === "patch_available" || status === "minor_available" || status === "update_available"
}

function simplifiedImageAuditLabel(status: string) {
	return isImageUpdateAvailable(status) ? <Trans>Update available</Trans> : <Trans>Up to date</Trans>
}

function simplifiedImageAuditBadgeClass(status: string) {
	return isImageUpdateAvailable(status)
		? "border-amber-500/40 bg-amber-500/10 text-amber-600 dark:text-amber-400"
		: "border-emerald-500/40 bg-emerald-500/10 text-emerald-600 dark:text-emerald-400"
}

function matchesContainerID(metricID: string, visibleID: string) {
	return metricID === visibleID || visibleID.startsWith(metricID) || metricID.startsWith(visibleID)
}

export default function HostDetailPage() {
	const { t } = useLingui()
	const page = useStore($router)
	const hostId = (page?.params as { id?: string } | undefined)?.id ?? ""
	const { dashboard, loading } = useDashboardData()
	const [containerFilters, setContainerFilters] = useState<ContainersFilters>(defaultContainersFilters)
	const [host, setHost] = useState<HostsOverviewRecord | null>(null)
	const [hostLoading, setHostLoading] = useState(true)
	const [metricsRange, setMetricsRange] = useState<MetricsRange>("24h")
	const [metricsHistory, setMetricsHistory] = useState<HostMetrics[]>([])
	const [latestContainerMetricsPoint, setLatestContainerMetricsPoint] = useState<ContainerMetricsHistoryPoint | null>(
		null
	)
	const metricsRequestRef = useRef(0)
	const containerMetricsRequestRef = useRef(0)
	const detailDebounceRef = useRef<ReturnType<typeof setTimeout> | null>(null)

	const loadHost = useCallback(async () => {
		if (!hostId) {
			setHost(null)
			setHostLoading(false)
			return
		}
		try {
			const data = await pb.send<HostsOverviewRecord>(`/api/app/hosts/${hostId}`, { method: "GET" })
			setHost(data)
		} catch (error) {
			console.error("host detail fetch failed", error)
			setHost(null)
		} finally {
			setHostLoading(false)
		}
	}, [hostId])

	const loadMetrics = useCallback(async () => {
		if (!hostId) {
			setMetricsHistory([])
			return
		}
		const requestId = ++metricsRequestRef.current
		try {
			const data = await pb.send<HostMetrics[]>(`/api/app/hosts/${hostId}/metrics?range=${metricsRange}`, { method: "GET" })
			if (requestId === metricsRequestRef.current) {
				setMetricsHistory(data)
			}
		} catch (error) {
			if (requestId === metricsRequestRef.current) {
				console.error("host metrics fetch failed", error)
				setMetricsHistory([])
			}
		}
	}, [hostId, metricsRange])

	const loadLatestContainerMetrics = useCallback(async () => {
		if (!hostId) {
			setLatestContainerMetricsPoint(null)
			return
		}
		const requestId = ++containerMetricsRequestRef.current
		try {
			const data = await pb.send<ContainerMetricsHistoryPoint>(
				`/api/app/hosts/${hostId}/container-metrics/latest`,
				{ method: "GET" }
			)
			if (requestId === containerMetricsRequestRef.current) {
				setLatestContainerMetricsPoint(data ?? null)
			}
		} catch (error) {
			if (requestId === containerMetricsRequestRef.current) {
				console.error("host container metrics fetch failed", error)
				setLatestContainerMetricsPoint(null)
			}
		}
	}, [hostId])

	const hostContainers = useMemo(
		() => (dashboard?.containers ?? []).filter((container) => container.host_id === hostId),
		[dashboard, hostId]
	)
	const auditedContainers = hostContainers.filter((container) => container.image_audit)
	const actionableAudits = auditedContainers.filter((container) => {
		const audit = container.image_audit
		return audit && audit.status !== "up_to_date" && audit.status !== "disabled"
	})

	useEffect(() => {
		setHostLoading(true)
		loadHost()
	}, [loadHost])

	useEffect(() => {
		loadMetrics()
	}, [loadMetrics])

	useEffect(() => {
		loadLatestContainerMetrics()
	}, [loadLatestContainerMetrics])

	useEffect(() => {
		if (!hostId) return
		const unsubscribes: Array<() => void> = []
		const refresh = () => {
			if (detailDebounceRef.current) clearTimeout(detailDebounceRef.current)
			detailDebounceRef.current = setTimeout(() => {
				loadHost()
				loadMetrics()
				loadLatestContainerMetrics()
			}, 1000)
		}
		;(async () => {
			unsubscribes.push(await pb.collection("agents").subscribe(hostId, refresh))
			unsubscribes.push(await pb.collection("host_snapshots").subscribe("*", refresh))
			unsubscribes.push(await pb.collection("host_metric_current").subscribe("*", refresh))
			unsubscribes.push(await pb.collection("container_metric_samples").subscribe("*", refresh))
		})()
		return () => {
			for (const unsubscribe of unsubscribes) unsubscribe()
			if (detailDebounceRef.current) clearTimeout(detailDebounceRef.current)
		}
	}, [hostId, loadLatestContainerMetrics, loadHost, loadMetrics])

	useEffect(() => {
		document.title = `${host?.name || t`Host`} / Vigil`
	}, [host?.name, t])

	const cpuHistory = useMemo(() => buildTimeSeries(metricsHistory, (point) => point.cpu_percent), [metricsHistory])
	const memoryHistory = useMemo(() => buildTimeSeries(metricsHistory, (point) => point.memory_used_percent), [metricsHistory])
	const diskHistory = useMemo(() => buildTimeSeries(metricsHistory, (point) => point.disk_used_percent), [metricsHistory])
	const visibleContainerIDList = useMemo(() => hostContainers.map((container) => container.id), [hostContainers])
	const latestContainerMetrics = useMemo(() => {
		const metricsByVisibleID = new Map<string, ContainerMetricsHistoryPoint["containers"][number]>()
		const containers = latestContainerMetricsPoint?.containers ?? []
		if (containers.length === 0) return metricsByVisibleID
		for (const visibleID of visibleContainerIDList) {
			const metric = containers.find((container) => matchesContainerID(container.id, visibleID))
			if (metric) {
				metricsByVisibleID.set(visibleID, metric)
			}
		}
		return metricsByVisibleID
	}, [latestContainerMetricsPoint, visibleContainerIDList])

	if (loading || hostLoading) {
		return (
			<div className="flex min-h-72 items-center justify-center">
				<Spinner />
			</div>
		)
	}

	if (!host) {
		return (
			<div className="space-y-4 py-6">
				<Button variant="outline" asChild>
					<Link href={getPagePath($router, "hosts")}>
						<ArrowLeftIcon className="me-2 size-4" />
						<Trans>Back to hosts</Trans>
					</Link>
				</Button>
				<div className="rounded-lg border border-dashed border-border/60 p-10 text-center text-muted-foreground">
					<Trans>Host not found.</Trans>
				</div>
			</div>
		)
	}

	const securityCount = host.packages?.security_count ?? 0
	const outdatedCount = host.packages?.outdated_count ?? 0
	const rebootRequired = host.reboot?.required
	const dockerCount = host.docker?.container_count ?? 0

	return (
		<div className="space-y-6 pb-10">
			<div className="space-y-4">
				<Button variant="ghost" asChild className="px-0 text-muted-foreground hover:text-foreground">
					<Link href={getPagePath($router, "hosts")}>
						<ArrowLeftIcon className="me-2 size-4" />
						<Trans>Back to hosts</Trans>
					</Link>
				</Button>
				<div className="flex flex-col gap-3 sm:flex-row sm:items-start sm:justify-between">
					<div>
						<div className="flex flex-wrap items-center gap-3">
							<h1 className="text-2xl font-semibold tracking-tight">{host.name || host.hostname || host.id}</h1>
							{statusBadge(host.status)}
							{rebootRequired && (
								<Badge variant="danger">
									<Trans>Reboot required</Trans>
								</Badge>
							)}
						</div>
						<div className="mt-1 flex flex-wrap gap-x-3 gap-y-1 text-sm text-muted-foreground">
							<span>{host.hostname || host.id}</span>
							<span>{host.primary_ip || "-"}</span>
							<span>
								<Trans>Last snapshot</Trans>: {formatDateTime(host.collected_at)}
							</span>
						</div>
					</div>
				</div>
			</div>

			<div className="grid gap-3 sm:grid-cols-2 lg:grid-cols-4">
				<MetricCard
					title={<Trans>Platform</Trans>}
					value={host.os ? `${host.os.name} ${host.os.version}`.trim() : "-"}
					icon={<ServerIcon className="size-4" />}
				/>
				<MetricCard
					title={<Trans>Resources</Trans>}
					value={`${host.resources?.cpu_cores ?? "-"} CPU / ${formatRam(host.resources?.ram_mb ?? 0)}`}
					icon={<CpuIcon className="size-4" />}
				/>
				<MetricCard
					title={<Trans>Patch status</Trans>}
					value={`${securityCount} sec / ${outdatedCount} upd`}
					icon={<ShieldAlertIcon className="size-4" />}
					tone={
						securityCount > 0
							? "border-red-500/40 bg-red-500/5"
							: outdatedCount > 0
								? "border-amber-500/40 bg-amber-500/5"
								: "border-emerald-500/30"
					}
				/>
				<MetricCard
					title={<Trans>Containers</Trans>}
					value={`${host.docker?.running_count ?? 0}/${dockerCount}`}
					icon={<BoxIcon className="size-4" />}
				/>
			</div>

			<Tabs defaultValue="monitoring" className="space-y-4">
				<div className="overflow-x-auto">
					<TabsList>
						<TabsTrigger value="monitoring">
							<Trans>Monitoring</Trans>
						</TabsTrigger>
						<TabsTrigger value="overview">
							<Trans>Overview</Trans>
						</TabsTrigger>
						<TabsTrigger value="containers">
							<Trans>Containers</Trans>
						</TabsTrigger>
						<TabsTrigger value="images">
							<Trans>Image updates</Trans>
						</TabsTrigger>
						<TabsTrigger value="packages">
							<Trans>Packages</Trans>
						</TabsTrigger>
						<TabsTrigger value="system">
							<Trans>System</Trans>
						</TabsTrigger>
					</TabsList>
				</div>

				<TabsContent value="monitoring" className="space-y-4">
					<div className="flex justify-end">
						<Select value={metricsRange} onValueChange={(value) => setMetricsRange(value as MetricsRange)}>
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

					<div className="grid gap-3 sm:grid-cols-2 lg:grid-cols-4">
						<MetricCard
							title={<Trans>CPU now</Trans>}
							value={formatPercent(host.metrics?.cpu_percent)}
							icon={<CpuIcon className="size-4" />}
						/>
						<MetricCard
							title={<Trans>RAM now</Trans>}
							value={formatPercent(host.metrics?.memory_used_percent)}
							icon={<ServerIcon className="size-4" />}
						/>
						<MetricCard
							title={<Trans>Disk now</Trans>}
							value={formatPercent(host.metrics?.disk_used_percent)}
							icon={<HardDriveIcon className="size-4" />}
						/>
						<MetricCard
							title={<Trans>Network now</Trans>}
							value={`${formatBytesPerSecond(host.metrics?.network_rx_bps ?? 0)} ↓`}
							icon={<NetworkIcon className="size-4" />}
						/>
					</div>

					<div className="grid gap-4 xl:grid-cols-2">
						<MetricHistoryChart
							title={<Trans>CPU usage</Trans>}
							points={cpuHistory}
							formatter={(value) => formatPercent(value)}
							color="rgb(59, 130, 246)"
						/>
						<MetricHistoryChart
							title={<Trans>Memory usage</Trans>}
							points={memoryHistory}
							formatter={(value) => formatPercent(value)}
							color="rgb(16, 185, 129)"
						/>
						<MetricHistoryChart
							title={<Trans>Disk usage</Trans>}
							points={diskHistory}
							formatter={(value) => formatPercent(value)}
							color="rgb(245, 158, 11)"
						/>
						<NetworkHistoryChart
							rxPoints={buildTimeSeries(metricsHistory, (point) => point.network_rx_bps)}
							txPoints={buildTimeSeries(metricsHistory, (point) => point.network_tx_bps)}
						/>
					</div>
				</TabsContent>

				<TabsContent value="overview" className="space-y-4">
					<div className="grid gap-4 lg:grid-cols-2">
						<Card>
							<CardHeader>
								<CardTitle className="text-base">
									<Trans>Needs attention</Trans>
								</CardTitle>
							</CardHeader>
							<CardContent className="space-y-3 text-sm">
								{host.status !== "connected" && <AttentionItem tone="danger" label={<Trans>Host is offline</Trans>} />}
								{rebootRequired && (
									<AttentionItem tone="danger" label={host.reboot?.reason || <Trans>Reboot required</Trans>} />
								)}
								{securityCount > 0 && (
									<AttentionItem tone="danger" label={<Trans>{securityCount} security update(s)</Trans>} />
								)}
								{actionableAudits.length > 0 && (
									<AttentionItem
										tone="warning"
										label={<Trans>{actionableAudits.length} image update issue(s)</Trans>}
									/>
								)}
								{host.status === "connected" &&
									!rebootRequired &&
									securityCount === 0 &&
									actionableAudits.length === 0 && (
										<div className="flex items-center gap-2 text-emerald-500">
											<CheckCircle2Icon className="size-4" />
											<Trans>No urgent issue detected.</Trans>
										</div>
									)}
							</CardContent>
						</Card>
						<Card>
							<CardHeader>
								<CardTitle className="text-base">
									<Trans>System summary</Trans>
								</CardTitle>
							</CardHeader>
							<CardContent className="grid gap-2 text-sm">
								<SummaryRow label={<Trans>Kernel</Trans>} value={host.kernel || "-"} mono />
								<SummaryRow label={<Trans>Architecture</Trans>} value={host.architecture || "-"} />
								<SummaryRow label={<Trans>Uptime</Trans>} value={formatUptime(host.uptime_seconds)} />
								<SummaryRow label={<Trans>Docker</Trans>} value={host.docker?.state?.replace(/_/g, " ") || "-"} />
							</CardContent>
						</Card>
					</div>
				</TabsContent>

				<TabsContent value="containers">
					<div className="space-y-4">
						<Card>
							<CardHeader>
								<CardTitle className="text-base">
									<Trans>Container runtime usage</Trans>
								</CardTitle>
							</CardHeader>
							<CardContent>
								{hostContainers.length === 0 ? (
									<div className="rounded-md border border-dashed border-border/60 p-6 text-sm text-muted-foreground">
										<Trans>No containers on this host.</Trans>
									</div>
								) : (
									<div className="overflow-x-auto rounded-md border border-border/60">
										<Table>
											<TableHeader>
												<TableRow>
													<TableHead>
														<Trans>Container</Trans>
													</TableHead>
													<TableHead>
														<Trans>CPU</Trans>
													</TableHead>
													<TableHead>
														<Trans>Memory</Trans>
													</TableHead>
													<TableHead>
														<Trans>Network</Trans>
													</TableHead>
												</TableRow>
											</TableHeader>
											<TableBody>
												{hostContainers.map((container) => {
													const metrics = latestContainerMetrics.get(container.id)
													const memoryPercent =
														metrics && metrics.memory_limit_bytes > 0
															? (metrics.memory_used_bytes / metrics.memory_limit_bytes) * 100
															: undefined
													return (
														<TableRow key={container.id}>
															<TableCell>
																<div className="min-w-[240px]">
																	{container.name ? (
																		<Link
																			href={getPagePath($router, "container", {
																				hostId: container.host_id,
																				name: container.name,
																			})}
																			className="font-medium hover:underline"
																		>
																			{container.name}
																		</Link>
																	) : (
																		<div className="font-medium">{container.id}</div>
																	)}
																	<div className="font-mono text-xs text-muted-foreground">
																		{container.image_ref || container.image || container.id}
																	</div>
																</div>
															</TableCell>
															<TableCell>
																<MetricBar value={metrics?.cpu_percent} />
															</TableCell>
															<TableCell>
																<div className="space-y-1">
																	<MetricBar value={memoryPercent} />
																	<div className="text-xs text-muted-foreground tabular-nums">
																		{metrics ? `${formatBytesCompact(metrics.memory_used_bytes)} / ${formatBytesCompact(metrics.memory_limit_bytes)}` : "—"}
																	</div>
																</div>
															</TableCell>
															<TableCell>
																<div className="space-y-0.5 text-xs tabular-nums">
																	<div>{formatBytesPerSecond(metrics?.network_rx_bps ?? 0)} ↓</div>
																	<div className="text-muted-foreground">{formatBytesPerSecond(metrics?.network_tx_bps ?? 0)} ↑</div>
																</div>
															</TableCell>
														</TableRow>
													)
												})}
											</TableBody>
										</Table>
									</div>
								)}
							</CardContent>
						</Card>
						<ContainersTable
							containers={hostContainers}
							filters={containerFilters}
							onFiltersChange={setContainerFilters}
						/>
					</div>
				</TabsContent>

				<TabsContent value="images">
					<Card>
						<CardHeader>
							<CardTitle className="text-base">
								<Plural value={auditedContainers.length} one="# audited container" other="# audited containers" />
							</CardTitle>
						</CardHeader>
						<CardContent>
							<div className="overflow-x-auto rounded-md border border-border/60">
								<Table>
									<TableHeader>
										<TableRow>
											<TableHead>
												<Trans>Container</Trans>
											</TableHead>
											<TableHead>
												<Trans>Image</Trans>
											</TableHead>
											<TableHead>
												<Trans>Status</Trans>
											</TableHead>
											<TableHead>
												<Trans>Candidate</Trans>
											</TableHead>
											<TableHead className="text-right">
												<Trans>Checked</Trans>
											</TableHead>
										</TableRow>
									</TableHeader>
									<TableBody>
										{auditedContainers.length === 0 ? (
											<TableRow>
												<TableCell colSpan={5} className="h-24 text-center text-sm text-muted-foreground">
													<Trans>No image audit data for this host.</Trans>
												</TableCell>
											</TableRow>
										) : (
											auditedContainers.map((container) => {
												const audit = container.image_audit
												const simplifiedStatus = isImageUpdateAvailable(audit?.line_status || audit?.status || "")
													? "update_available"
													: "up_to_date"
												return (
													<TableRow key={container.id}>
														<TableCell className="font-medium">
															{container.name ? (
																<Link
																	href={getPagePath($router, "container", {
																		hostId: container.host_id,
																		name: container.name,
																	})}
																	className="hover:underline"
																>
																	{container.name}
																</Link>
															) : (
																container.id
															)}
														</TableCell>
														<TableCell className="font-mono text-xs">
															{audit?.current_ref || container.image_ref || container.image}
														</TableCell>
														<TableCell>
															<Badge
																variant="outline"
																className={cn("text-[10px]", simplifiedImageAuditBadgeClass(simplifiedStatus))}
															>
																{simplifiedImageAuditLabel(simplifiedStatus)}
															</Badge>
														</TableCell>
														<TableCell className="font-mono text-xs">
															{simplifiedStatus === "update_available" ? audit?.line_latest_tag || audit?.latest_tag || "-" : "-"}
														</TableCell>
														<TableCell className="text-right text-xs text-muted-foreground">
															{formatDateTime(audit?.checked_at || "")}
														</TableCell>
													</TableRow>
												)
											})
										)}
									</TableBody>
								</Table>
							</div>
						</CardContent>
					</Card>
				</TabsContent>

				<TabsContent value="packages" className="space-y-4">
					<Card>
						<CardHeader>
							<CardTitle className="text-base">
								<Trans>Package updates</Trans>
							</CardTitle>
						</CardHeader>
						<CardContent>
							<div className="overflow-x-auto rounded-md border border-border/60">
								<Table>
									<TableHeader>
										<TableRow>
											<TableHead>
												<Trans>Package</Trans>
											</TableHead>
											<TableHead>
												<Trans>Installed</Trans>
											</TableHead>
											<TableHead>
												<Trans>Candidate</Trans>
											</TableHead>
											<TableHead>
												<Trans>Type</Trans>
											</TableHead>
										</TableRow>
									</TableHeader>
									<TableBody>
										{(host.packages?.outdated ?? []).length === 0 ? (
											<TableRow>
												<TableCell colSpan={4} className="h-24 text-center text-sm text-muted-foreground">
													<Trans>No outdated packages.</Trans>
												</TableCell>
											</TableRow>
										) : (
											host.packages.outdated.map((pkg) => (
												<TableRow key={`${pkg.name}-${pkg.candidate_version}`}>
													<TableCell className="font-medium">{pkg.name}</TableCell>
													<TableCell className="font-mono text-xs">{pkg.installed_version}</TableCell>
													<TableCell className="font-mono text-xs">{pkg.candidate_version}</TableCell>
													<TableCell>
														{pkg.is_security ? (
															<Badge variant="danger">
																<Trans>Security</Trans>
															</Badge>
														) : (
															<Badge variant="outline">
																<Trans>Update</Trans>
															</Badge>
														)}
													</TableCell>
												</TableRow>
											))
										)}
									</TableBody>
								</Table>
							</div>
						</CardContent>
					</Card>
				</TabsContent>

				<TabsContent value="system" className="grid gap-4 lg:grid-cols-2">
					<Card>
						<CardHeader>
							<CardTitle className="flex items-center gap-2 text-base">
								<HardDriveIcon className="size-4" />
								<Trans>Storage</Trans>
							</CardTitle>
						</CardHeader>
						<CardContent className="space-y-2 text-sm">
							{(host.storage ?? []).length === 0 ? (
								<p className="text-muted-foreground">
									<Trans>No storage data.</Trans>
								</p>
							) : (
								host.storage.map((mount) => (
									<div key={`${mount.device}-${mount.mountpoint}`} className="rounded-md border border-border/60 p-3">
										<div className="flex items-center justify-between gap-3">
											<span className="font-medium">{mount.mountpoint}</span>
											<span className="text-muted-foreground">{formatStorageValue(mount.used_percent)}%</span>
										</div>
										<div className="mt-1 text-xs text-muted-foreground">
											{mount.device} · {mount.fs_type} · {formatBytes(mount.used_bytes)} /{" "}
											{formatBytes(mount.total_bytes)}
										</div>
									</div>
								))
							)}
						</CardContent>
					</Card>
					<Card>
						<CardHeader>
							<CardTitle className="flex items-center gap-2 text-base">
								<NetworkIcon className="size-4" />
								<Trans>Network and repositories</Trans>
							</CardTitle>
						</CardHeader>
						<CardContent className="space-y-3 text-sm">
							<SummaryRow label={<Trans>Primary IP</Trans>} value={host.primary_ip || "-"} mono />
							<SummaryRow label={<Trans>Gateway</Trans>} value={host.network?.gateway || "-"} mono />
							<SummaryRow label={<Trans>DNS</Trans>} value={host.network?.dns_servers?.join(", ") || "-"} mono />
							<div className="pt-2 text-xs font-semibold uppercase tracking-wide text-muted-foreground">
								<Trans>Repositories</Trans>
							</div>
							{(host.repositories ?? []).length === 0 ? (
								<p className="text-muted-foreground">
									<Trans>No repository data.</Trans>
								</p>
							) : (
								host.repositories.map((repo) => (
									<div key={`${repo.name}-${repo.url}`} className="rounded-md border border-border/60 p-3">
										<div className="flex items-center justify-between gap-3">
											<span className="font-medium">{repo.name || repo.distribution || repo.url}</span>
											<Badge variant={repo.secure ? "outline" : "danger"}>
												{repo.secure ? <Trans>Secure</Trans> : <Trans>Insecure</Trans>}
											</Badge>
										</div>
										<div className="mt-1 break-all font-mono text-xs text-muted-foreground">{repo.url}</div>
									</div>
								))
							)}
						</CardContent>
					</Card>
				</TabsContent>
			</Tabs>
		</div>
	)
}

function AttentionItem({ label, tone }: { label: React.ReactNode; tone: "danger" | "warning" }) {
	return (
		<div
			className={cn(
				"rounded-md border px-3 py-2",
				tone === "danger"
					? "border-red-500/30 bg-red-500/5 text-red-500"
					: "border-amber-500/30 bg-amber-500/5 text-amber-500"
			)}
		>
			{label}
		</div>
	)
}

function SummaryRow({ label, value, mono }: { label: React.ReactNode; value: React.ReactNode; mono?: boolean }) {
	return (
		<div className="grid grid-cols-[9rem_1fr] gap-3">
			<span className="text-muted-foreground">{label}</span>
			<span className={cn("min-w-0 break-all", mono && "font-mono text-xs")}>{value}</span>
		</div>
	)
}
