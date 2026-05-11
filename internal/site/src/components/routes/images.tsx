import { Plural, Trans, useLingui } from "@lingui/react/macro"
import { getPagePath } from "@nanostores/router"
import {
	AlertTriangleIcon,
	BoxesIcon,
	CheckCircle2Icon,
	ChevronRightIcon,
	CircleSlash2Icon,
	Loader2Icon,
	PartyPopperIcon,
	RefreshCwIcon,
	SearchXIcon,
	ServerIcon,
	ShieldOffIcon,
	XCircleIcon,
} from "lucide-react"
import { useCallback, useEffect, useMemo, useRef, useState } from "react"
import { PageHeader } from "@/components/page-header"
import { $router, Link, navigate } from "@/components/router"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"
import { toast } from "@/components/ui/use-toast"
import { isAdmin, isReadOnlyUser, pb } from "@/lib/api"
import type { ContainerFleetEntry, ContainerImageAudit, DashboardResponse } from "@/lib/dashboard-types"
import { cn } from "@/lib/utils"

// ── helpers ──────────────────────────────────────────────────────────────────

type AuditedEntry = ContainerFleetEntry & { image_audit: ContainerImageAudit }

type Bucket = "major" | "update" | "up_to_date" | "failed" | "disabled" | "other"

function classifyBucket(audit: ContainerImageAudit): Bucket {
	if (audit.major_update_available) return "major"
	const ls = audit.line_status || audit.status
	if (
		ls === "patch_available" ||
		ls === "minor_available" ||
		ls === "tag_rebuilt" ||
		audit.status === "update_available"
	)
		return "update"
	if (audit.status === "up_to_date" || ls === "up_to_date") return "up_to_date"
	if (audit.status === "check_failed") return "failed"
	if (audit.status === "disabled") return "disabled"
	return "other"
}

function useBucketLabels(): Record<Bucket, string> {
	const { t } = useLingui()
	return {
		major: t`New major versions`,
		update: t`Updates available`,
		up_to_date: t`Up to date`,
		failed: t`Check failed`,
		disabled: t`Disabled`,
		other: t`Unsupported`,
	}
}

function bucketIcon(bucket: Bucket) {
	switch (bucket) {
		case "major":
			return <PartyPopperIcon className="size-4 text-sky-400" />
		case "update":
			return <AlertTriangleIcon className="size-4 text-amber-400" />
		case "up_to_date":
			return <CheckCircle2Icon className="size-4 text-emerald-400" />
		case "failed":
			return <XCircleIcon className="size-4 text-red-400" />
		case "disabled":
			return <ShieldOffIcon className="size-4 text-muted-foreground" />
		default:
			return <CircleSlash2Icon className="size-4 text-muted-foreground" />
	}
}

const bucketOrder: Bucket[] = ["major", "update", "failed", "up_to_date", "disabled", "other"]
const ALL_FILTERS = "__all__"

function useLineStatusLabel(): (audit: ContainerImageAudit) => string {
	const { t } = useLingui()
	return (audit: ContainerImageAudit) => {
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
}

function formatRelative(iso: string): string {
	if (!iso) return "—"
	const parsed = Date.parse(iso)
	if (Number.isNaN(parsed)) return "—"
	const diff = Date.now() - parsed
	if (diff < 60_000) return "just now"
	if (diff < 3_600_000) return `${Math.floor(diff / 60_000)}m ago`
	if (diff < 86_400_000) return `${Math.floor(diff / 3_600_000)}h ago`
	return `${Math.floor(diff / 86_400_000)}d ago`
}

// ── data hook ────────────────────────────────────────────────────────────────

function useImageAudits() {
	const [dashboard, setDashboard] = useState<DashboardResponse | null>(null)
	const [containers, setContainers] = useState<AuditedEntry[]>([])
	const [loading, setLoading] = useState(true)
	const debounceRef = useRef<ReturnType<typeof setTimeout> | null>(null)

	const refetch = useCallback(async () => {
		try {
			const res = await pb.send<DashboardResponse>("/api/app/dashboard", { method: "GET" })
			const audited = (res.containers ?? []).filter((c): c is AuditedEntry => !!c.image_audit && !!c.image_audit.status)
			setDashboard(res)
			setContainers(audited)
		} catch {
			/* transient error — keep last value */
		} finally {
			setLoading(false)
		}
	}, [])

	useEffect(() => {
		refetch()
		const unsubscribes: Array<() => void> = []
		const debouncedRefetch = () => {
			if (debounceRef.current) clearTimeout(debounceRef.current)
			debounceRef.current = setTimeout(refetch, 1000)
		}
		;(async () => {
			unsubscribes.push(await pb.collection("agents").subscribe("*", debouncedRefetch))
			unsubscribes.push(await pb.collection("host_snapshots").subscribe("*", debouncedRefetch))
			unsubscribes.push(await pb.collection("container_image_audits").subscribe("*", debouncedRefetch))
		})()
		return () => {
			for (const unsubscribe of unsubscribes) unsubscribe()
			if (debounceRef.current) clearTimeout(debounceRef.current)
		}
	}, [refetch])

	return { dashboard, containers, loading, refetch }
}

// ── counters ─────────────────────────────────────────────────────────────────

function CountersStrip({ counts }: { counts: Record<Bucket, number> }) {
	const cards: Array<{ bucket: Bucket; tone: string }> = [
		{ bucket: "major", tone: "text-sky-400 border-sky-500/30 bg-sky-500/5" },
		{ bucket: "update", tone: "text-amber-400 border-amber-500/30 bg-amber-500/5" },
		{ bucket: "failed", tone: "text-red-400 border-red-500/30 bg-red-500/5" },
		{ bucket: "up_to_date", tone: "text-emerald-400 border-emerald-500/30 bg-emerald-500/5" },
		{ bucket: "disabled", tone: "text-muted-foreground border-border/40 bg-muted/30" },
	]
	const labels = useBucketLabels()
	return (
		<div className="grid gap-3 sm:grid-cols-2 lg:grid-cols-5">
			{cards.map(({ bucket, tone }) => (
				<div
					key={bucket}
					className={cn(
						"rounded-lg border p-4 transition-colors",
						counts[bucket] > 0 ? tone : "border-border/40 text-muted-foreground"
					)}
				>
					<div className="flex items-center gap-2 text-xs uppercase tracking-wide">
						{bucketIcon(bucket)}
						<span>{labels[bucket]}</span>
					</div>
					<div className="mt-2 text-2xl font-bold tabular-nums">{counts[bucket] ?? 0}</div>
				</div>
			))}
		</div>
	)
}

// ── row + group ──────────────────────────────────────────────────────────────

function AuditRow({
	entry,
	onSelect,
	statusLabel,
}: {
	entry: AuditedEntry
	onSelect: () => void
	statusLabel: string
}) {
	const { t } = useLingui()
	const audit = entry.image_audit
	const bucket = classifyBucket(audit)
	const targets: Array<{ label: string; value: string; tone: string }> = []
	const lineLatest = audit.line_latest_tag || audit.latest_tag || ""
	if (lineLatest && lineLatest !== audit.tag) {
		targets.push({
			label: t`patch line`,
			value: lineLatest,
			tone: "border-amber-500/30 bg-amber-500/10 text-amber-300",
		})
	}
	if (audit.same_major_latest_tag && audit.same_major_latest_tag !== lineLatest) {
		targets.push({
			label: t`same major`,
			value: audit.same_major_latest_tag,
			tone: "border-violet-500/30 bg-violet-500/10 text-violet-300",
		})
	}
	if (audit.major_update_available && audit.new_major_tag) {
		targets.push({
			label: t`new major`,
			value: audit.new_major_tag,
			tone: "border-sky-500/30 bg-sky-500/10 text-sky-300",
		})
	}
	return (
		<button
			type="button"
			onClick={onSelect}
			className="flex w-full items-center gap-3 rounded-md border border-transparent px-3 py-2 text-start hover:border-border hover:bg-accent/40"
		>
			<div className="min-w-0 flex-1">
				<div className="flex items-center gap-2 text-sm font-semibold">
					<span className="truncate">{entry.name || entry.id}</span>
					<span className="truncate text-xs text-muted-foreground">
						<Trans>on</Trans> {entry.host_name || entry.host_id}
					</span>
				</div>
				<div className="mt-0.5 flex items-center gap-1.5 truncate text-xs">
					<span className="font-mono text-muted-foreground">{audit.current_ref || entry.image}</span>
				</div>
			</div>
			<div className="flex shrink-0 flex-wrap items-center justify-end gap-1.5">
				{bucket !== "up_to_date" && bucket !== "disabled" && statusLabel && (
					<Badge variant="outline" className="text-[10px] uppercase tracking-wide">
						{statusLabel}
					</Badge>
				)}
				{targets.map((tg) => (
					<Badge
						key={tg.label + tg.value}
						variant="outline"
						className={cn("font-mono text-[10px]", tg.tone)}
						title={tg.label}
					>
						{tg.value}
					</Badge>
				))}
				<ChevronRightIcon className="size-4 text-muted-foreground" />
			</div>
		</button>
	)
}

function AuditGroup({
	bucket,
	entries,
	onSelect,
	defaultOpen,
}: {
	bucket: Bucket
	entries: AuditedEntry[]
	onSelect: (e: AuditedEntry) => void
	defaultOpen: boolean
}) {
	const labels = useBucketLabels()
	const lineLabelFor = useLineStatusLabel()
	const [open, setOpen] = useState(defaultOpen)
	if (entries.length === 0) return null
	return (
		<div className="rounded-lg border border-border/60 bg-card">
			<button
				type="button"
				onClick={() => setOpen((v) => !v)}
				className="flex w-full items-center justify-between gap-2 px-4 py-3 text-start hover:bg-accent/30"
			>
				<div className="flex items-center gap-2">
					{bucketIcon(bucket)}
					<span className="text-sm font-semibold">{labels[bucket]}</span>
					<Badge variant="secondary" className="ms-1">
						{entries.length}
					</Badge>
				</div>
				<ChevronRightIcon className={cn("size-4 transition-transform", open && "rotate-90")} />
			</button>
			{open && (
				<div className="divide-y divide-border/40">
					{entries.map((entry) => (
						<AuditRow
							key={`${entry.host_id}|${entry.id}`}
							entry={entry}
							onSelect={() => onSelect(entry)}
							statusLabel={lineLabelFor(entry.image_audit)}
						/>
					))}
				</div>
			)}
		</div>
	)
}

function EmptyAuditState({
	kind,
	onResetFilters,
	onRunAudit,
	auditing,
}: {
	kind: "no_agents" | "no_containers" | "no_audits" | "no_filter_results"
	onResetFilters: () => void
	onRunAudit: () => void
	auditing: boolean
}) {
	const canManageAgents = !isReadOnlyUser()
	const admin = isAdmin()
	const icon =
		kind === "no_agents" ? (
			<ServerIcon className="size-8 text-muted-foreground" />
		) : (
			<SearchXIcon className="size-8 text-muted-foreground" />
		)

	return (
		<div className="flex flex-col items-center justify-center gap-5 rounded-lg border border-dashed border-border/60 px-6 py-12 text-center">
			<div className="flex size-16 items-center justify-center rounded-2xl border border-border/70 bg-muted/40">
				{icon}
			</div>
			{kind === "no_agents" && (
				<div className="space-y-2">
					<h2 className="text-lg font-semibold">
						<Trans>No agents configured</Trans>
					</h2>
					<p className="max-w-md text-sm leading-relaxed text-muted-foreground">
						<Trans>Configure an agent to start collecting container and image audit data.</Trans>
					</p>
				</div>
			)}
			{kind === "no_containers" && (
				<div className="space-y-2">
					<h2 className="text-lg font-semibold">
						<Trans>No containers detected</Trans>
					</h2>
					<p className="max-w-md text-sm leading-relaxed text-muted-foreground">
						<Trans>Agents are configured, but no Docker containers have been reported by monitored hosts.</Trans>
					</p>
				</div>
			)}
			{kind === "no_audits" && (
				<div className="space-y-2">
					<h2 className="text-lg font-semibold">
						<Trans>No image audit data yet</Trans>
					</h2>
					<p className="max-w-md text-sm leading-relaxed text-muted-foreground">
						<Trans>Containers were detected, but image audit results have not been generated yet.</Trans>
					</p>
				</div>
			)}
			{kind === "no_filter_results" && (
				<div className="space-y-2">
					<h2 className="text-lg font-semibold">
						<Trans>No images match these filters</Trans>
					</h2>
					<p className="max-w-md text-sm leading-relaxed text-muted-foreground">
						<Trans>Adjust or reset the current filters to see audited container images.</Trans>
					</p>
				</div>
			)}
			{kind === "no_agents" && canManageAgents && (
				<Button asChild>
					<Link href={getPagePath($router, "settings", { name: "agents" })}>
						<Trans>Set up agents</Trans>
					</Link>
				</Button>
			)}
			{kind === "no_audits" && admin && (
				<Button variant="outline" disabled={auditing} onClick={onRunAudit}>
					{auditing ? <Loader2Icon className="me-2 size-4 animate-spin" /> : <RefreshCwIcon className="me-2 size-4" />}
					<Trans>Check images now</Trans>
				</Button>
			)}
			{kind === "no_filter_results" && (
				<Button variant="outline" onClick={onResetFilters}>
					<Trans>Reset filters</Trans>
				</Button>
			)}
		</div>
	)
}

// ── page ─────────────────────────────────────────────────────────────────────

export default function ImagesPage() {
	const { t } = useLingui()
	const { dashboard, containers, loading, refetch } = useImageAudits()
	const [search, setSearch] = useState("")
	const [hostFilter, setHostFilter] = useState("")
	const [statusFilter, setStatusFilter] = useState<Bucket | "">("")
	const [auditing, setAuditing] = useState(false)
	const admin = isAdmin()
	const bucketLabels = useBucketLabels()
	const allContainers = dashboard?.containers ?? []
	const totalAgents = dashboard?.summary.total_hosts ?? 0
	const totalContainers = dashboard?.summary.total_containers ?? allContainers.length
	const hasActiveFilters = Boolean(search.trim() || hostFilter || statusFilter)

	const lastChecked = useMemo(() => {
		let max = ""
		for (const c of containers) {
			if (c.image_audit.checked_at > max) max = c.image_audit.checked_at
		}
		return max
	}, [containers])

	const uniqueHosts = useMemo(() => {
		const seen = new Map<string, string>()
		for (const c of containers) seen.set(c.host_id, c.host_name || c.host_id)
		return Array.from(seen.entries()).map(([id, name]) => ({ id, name }))
	}, [containers])

	const filtered = useMemo(() => {
		let result = containers
		const q = search.trim().toLowerCase()
		if (q) {
			result = result.filter((c) =>
				[c.name, c.id, c.host_name, c.host_id, c.image, c.image_audit?.tag, c.image_audit?.latest_tag]
					.filter(Boolean)
					.some((v) => (v as string).toLowerCase().includes(q))
			)
		}
		if (hostFilter) result = result.filter((c) => c.host_id === hostFilter)
		if (statusFilter) result = result.filter((c) => classifyBucket(c.image_audit) === statusFilter)
		return result
	}, [containers, search, hostFilter, statusFilter])

	const grouped = useMemo(() => {
		const out: Record<Bucket, AuditedEntry[]> = {
			major: [],
			update: [],
			up_to_date: [],
			failed: [],
			disabled: [],
			other: [],
		}
		for (const entry of filtered) {
			out[classifyBucket(entry.image_audit)].push(entry)
		}
		return out
	}, [filtered])

	const counts = useMemo<Record<Bucket, number>>(() => {
		const c: Record<Bucket, number> = {
			major: 0,
			update: 0,
			up_to_date: 0,
			failed: 0,
			disabled: 0,
			other: 0,
		}
		for (const entry of containers) c[classifyBucket(entry.image_audit)]++
		return c
	}, [containers])

	const emptyKind = !loading
		? totalAgents === 0
			? "no_agents"
			: totalContainers === 0
				? "no_containers"
				: containers.length === 0
					? "no_audits"
					: hasActiveFilters && filtered.length === 0
						? "no_filter_results"
						: null
		: null

	function resetFilters() {
		setSearch("")
		setHostFilter("")
		setStatusFilter("")
	}

	async function runAuditNow() {
		setAuditing(true)
		try {
			await pb.send("/api/app/jobs/vigilContainerImageAudit/run", { method: "POST" })
			toast({ title: t`Image audit completed` })
			refetch()
		} catch (error: unknown) {
			toast({
				title: t`Failed to run image audit`,
				description: (error as Error).message,
				variant: "destructive",
			})
		} finally {
			setAuditing(false)
		}
	}

	const openContainer = useCallback((entry: AuditedEntry) => {
		navigate(getPagePath($router, "container", { hostId: entry.host_id, name: entry.name }))
	}, [])

	return (
		<div className="space-y-4 pb-10">
			<PageHeader
				icon={BoxesIcon}
				title={<Trans>Image updates</Trans>}
				meta={
					!loading ? (
						<Plural value={containers.length} one="# audited container" other="# audited containers" />
					) : undefined
				}
			/>

			{containers.length > 0 && (
				<div className="flex flex-col gap-2 sm:flex-row sm:flex-wrap sm:items-center">
					<Input
						placeholder={t`Search container, host, image…`}
						value={search}
						onChange={(e) => setSearch(e.target.value)}
						className="sm:max-w-[280px]"
					/>
					{uniqueHosts.length > 1 && (
						<Select value={hostFilter || ALL_FILTERS} onValueChange={(v) => setHostFilter(v === ALL_FILTERS ? "" : v)}>
							<SelectTrigger className="w-40">
								<SelectValue placeholder={t`All hosts`} />
							</SelectTrigger>
							<SelectContent>
								<SelectItem value={ALL_FILTERS}>
									<Trans>All hosts</Trans>
								</SelectItem>
								{uniqueHosts.map((h) => (
									<SelectItem key={h.id} value={h.id}>
										{h.name}
									</SelectItem>
								))}
							</SelectContent>
						</Select>
					)}
					<Select
						value={statusFilter || ALL_FILTERS}
						onValueChange={(v) => setStatusFilter(v === ALL_FILTERS ? "" : (v as Bucket))}
					>
						<SelectTrigger className="w-44">
							<SelectValue placeholder={t`All statuses`} />
						</SelectTrigger>
						<SelectContent>
							<SelectItem value={ALL_FILTERS}>
								<Trans>All statuses</Trans>
							</SelectItem>
							{bucketOrder.map((b) => (
								<SelectItem key={b} value={b}>
									{bucketLabels[b]}
								</SelectItem>
							))}
						</SelectContent>
					</Select>
					{admin && (
						<div className="ml-auto flex shrink-0 items-center gap-2">
							{lastChecked && (
								<span className="text-xs text-muted-foreground">
									<Trans>Last check:</Trans> {formatRelative(lastChecked)}
								</span>
							)}
							<Button variant="outline" disabled={auditing} onClick={runAuditNow}>
								{auditing ? (
									<Loader2Icon className="me-2 size-4 animate-spin" />
								) : (
									<RefreshCwIcon className="me-2 size-4" />
								)}
								<Trans>Check images now</Trans>
							</Button>
						</div>
					)}
				</div>
			)}

			{containers.length > 0 && <CountersStrip counts={counts} />}

			{emptyKind ? (
				<EmptyAuditState kind={emptyKind} onResetFilters={resetFilters} onRunAudit={runAuditNow} auditing={auditing} />
			) : (
				<div className="space-y-3">
					{bucketOrder.map((bucket) => (
						<AuditGroup
							key={bucket}
							bucket={bucket}
							entries={grouped[bucket]}
							onSelect={openContainer}
							defaultOpen={bucket === "major" || bucket === "update" || bucket === "failed"}
						/>
					))}
				</div>
			)}
		</div>
	)
}
