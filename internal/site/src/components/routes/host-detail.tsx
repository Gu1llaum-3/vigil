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
import { useEffect, useMemo, useState } from "react"
import { $router, Link } from "@/components/router"
import Spinner from "@/components/spinner"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card"
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs"
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table"
import { cn } from "@/lib/utils"
import { type ContainersFilters, defaultContainersFilters } from "./dashboard/containers-filter-sheet"
import { ContainersTable } from "./dashboard/containers-table"
import { useDashboardData } from "./dashboard/use-dashboard-data"

function formatBytes(bytes: number) {
	if (!bytes || bytes <= 0) return "-"
	const units = ["B", "KB", "MB", "GB", "TB"]
	let value = bytes
	let unit = 0
	while (value >= 1024 && unit < units.length - 1) {
		value /= 1024
		unit++
	}
	return `${formatStorageValue(value)} ${units[unit]}`
}

function formatStorageValue(value: number) {
	return value.toLocaleString(undefined, { minimumFractionDigits: 2, maximumFractionDigits: 2 })
}

function formatRam(mb: number) {
	if (!mb || mb <= 0) return "-"
	return mb >= 1024 ? `${Math.round(mb / 1024)} GB` : `${Math.round(mb)} MB`
}

function formatUptime(seconds: number) {
	if (!seconds || seconds <= 0) return "-"
	const days = Math.floor(seconds / 86400)
	const hours = Math.floor((seconds % 86400) / 3600)
	const minutes = Math.floor((seconds % 3600) / 60)
	if (days > 0) return `${days}d ${hours}h`
	if (hours > 0) return `${hours}h ${minutes}m`
	return `${minutes}m`
}

function formatDateTime(value: string) {
	if (!value) return "-"
	const parsed = new Date(value)
	if (Number.isNaN(parsed.getTime())) return value
	return parsed.toLocaleString()
}

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

function imageAuditLabel(status: string) {
	switch (status) {
		case "update_available":
			return <Trans>Update available</Trans>
		case "up_to_date":
			return <Trans>Up to date</Trans>
		case "check_failed":
			return <Trans>Check failed</Trans>
		case "disabled":
			return <Trans>Disabled</Trans>
		default:
			return status || <Trans>Unknown</Trans>
	}
}

function imageAuditBadgeClass(status: string) {
	switch (status) {
		case "update_available":
			return "border-amber-500/40 bg-amber-500/10 text-amber-600 dark:text-amber-400"
		case "up_to_date":
			return "border-emerald-500/40 bg-emerald-500/10 text-emerald-600 dark:text-emerald-400"
		case "check_failed":
			return "border-red-500/40 bg-red-500/10 text-red-600 dark:text-red-400"
		case "disabled":
			return "border-border/50 bg-muted/40 text-muted-foreground"
		default:
			return "border-border/50 text-muted-foreground"
	}
}

function MetricCard({
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

export default function HostDetailPage() {
	const { t } = useLingui()
	const page = useStore($router)
	const hostId = (page?.params as { id?: string } | undefined)?.id ?? ""
	const { dashboard, loading } = useDashboardData()
	const [containerFilters, setContainerFilters] = useState<ContainersFilters>(defaultContainersFilters)

	const host = useMemo(() => dashboard?.hosts.find((candidate) => candidate.id === hostId), [dashboard, hostId])
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
		document.title = `${host?.name || t`Host`} / Vigil`
	}, [host?.name, t])

	if (loading) {
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

			<Tabs defaultValue="overview" className="space-y-4">
				<div className="overflow-x-auto">
					<TabsList>
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
					<ContainersTable
						containers={hostContainers}
						filters={containerFilters}
						onFiltersChange={setContainerFilters}
					/>
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
												return (
													<TableRow key={container.id}>
														<TableCell className="font-medium">{container.name || container.id}</TableCell>
														<TableCell className="font-mono text-xs">
															{audit?.current_ref || container.image_ref || container.image}
														</TableCell>
														<TableCell>
															<Badge
																variant="outline"
																className={cn("text-[10px]", imageAuditBadgeClass(audit?.status || ""))}
															>
																{imageAuditLabel(audit?.status || "")}
															</Badge>
														</TableCell>
														<TableCell className="font-mono text-xs">
															{audit?.line_latest_tag || audit?.latest_tag || "-"}
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
