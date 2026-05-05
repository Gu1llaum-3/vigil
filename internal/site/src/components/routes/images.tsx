import { Plural, Trans, useLingui } from "@lingui/react/macro"
import {
	AlertTriangleIcon,
	BoxesIcon,
	CheckCircle2Icon,
	ChevronRightIcon,
	CircleSlash2Icon,
	Loader2Icon,
	PartyPopperIcon,
	RefreshCwIcon,
	ShieldOffIcon,
	XCircleIcon,
} from "lucide-react"
import { useCallback, useEffect, useMemo, useRef, useState } from "react"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Sheet, SheetContent, SheetDescription, SheetHeader, SheetTitle } from "@/components/ui/sheet"
import { toast } from "@/components/ui/use-toast"
import { isAdmin, pb } from "@/lib/api"
import type { ContainerFleetEntry, ContainerImageAudit, DashboardResponse } from "@/lib/dashboard-types"
import { cn } from "@/lib/utils"

// ── helpers ──────────────────────────────────────────────────────────────────

type AuditedEntry = ContainerFleetEntry & { image_audit: ContainerImageAudit }

type Bucket = "major" | "update" | "up_to_date" | "failed" | "disabled" | "other"

function classifyBucket(audit: ContainerImageAudit): Bucket {
	if (audit.major_update_available) return "major"
	const ls = audit.line_status || audit.status
	if (ls === "patch_available" || ls === "minor_available" || ls === "tag_rebuilt" || audit.status === "update_available")
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
	const [containers, setContainers] = useState<AuditedEntry[]>([])
	const [loading, setLoading] = useState(true)
	const debounceRef = useRef<ReturnType<typeof setTimeout> | null>(null)

	const refetch = useCallback(async () => {
		try {
			const res = await pb.send<DashboardResponse>("/api/app/dashboard", { method: "GET" })
			const audited = (res.containers ?? []).filter(
				(c): c is AuditedEntry => !!c.image_audit && !!c.image_audit.status
			)
			setContainers(audited)
		} catch {
			/* transient error — keep last value */
		} finally {
			setLoading(false)
		}
	}, [])

	useEffect(() => {
		refetch()
		let unsubscribe: (() => void) | undefined
		;(async () => {
			unsubscribe = await pb.collection("container_image_audits").subscribe("*", () => {
				if (debounceRef.current) clearTimeout(debounceRef.current)
				debounceRef.current = setTimeout(refetch, 1000)
			})
		})()
		return () => {
			unsubscribe?.()
			if (debounceRef.current) clearTimeout(debounceRef.current)
		}
	}, [refetch])

	return { containers, loading, refetch }
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
				<span className="hidden text-xs text-muted-foreground sm:inline">{formatRelative(audit.checked_at)}</span>
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

// ── detail drawer ────────────────────────────────────────────────────────────

function AuditDetail({
	entry,
	onClose,
	onPin,
	onDisable,
}: {
	entry: AuditedEntry | null
	onClose: () => void
	onPin: (entry: AuditedEntry) => Promise<void>
	onDisable: (entry: AuditedEntry) => Promise<void>
}) {
	const { t } = useLingui()
	const lineLabelFor = useLineStatusLabel()
	const admin = isAdmin()
	if (!entry) return null
	const audit = entry.image_audit
	const Row = ({ label, value, mono }: { label: string; value: string; mono?: boolean }) => (
		<div className="grid grid-cols-[10rem_1fr] gap-2 text-sm">
			<span className="text-muted-foreground">{label}</span>
			<span className={cn("break-all", mono && "font-mono text-xs")}>{value || "—"}</span>
		</div>
	)
	return (
		<Sheet open onOpenChange={(o) => !o && onClose()}>
			<SheetContent side="right" className="w-full max-w-xl overflow-y-auto px-6 py-6 sm:max-w-xl">
				<SheetHeader className="space-y-1 px-0">
					<SheetTitle className="text-base font-semibold">
						{entry.name || entry.id}
					</SheetTitle>
					<SheetDescription className="font-mono text-xs">
						{entry.host_name || entry.host_id}
					</SheetDescription>
				</SheetHeader>
				<div className="mt-4 space-y-4">
					<section className="space-y-1.5 rounded-md border border-border/60 p-3">
						<h3 className="text-xs font-semibold uppercase tracking-wide text-muted-foreground">
							<Trans>Status</Trans>
						</h3>
						<div className="flex flex-wrap items-center gap-2">
							<Badge variant="outline" className="text-[10px] uppercase">
								{lineLabelFor(audit)}
							</Badge>
							<Badge variant="secondary" className="text-[10px]">
								{audit.policy || "auto"}
							</Badge>
							<span className="text-xs text-muted-foreground">{formatRelative(audit.checked_at)}</span>
						</div>
						{audit.error && (
							<p className="text-xs text-red-400">
								<Trans>Error</Trans>: <span className="font-mono">{audit.error}</span>
							</p>
						)}
					</section>

					<section className="space-y-2 rounded-md border border-border/60 p-3">
						<h3 className="text-xs font-semibold uppercase tracking-wide text-muted-foreground">
							<Trans>Versions</Trans>
						</h3>
						<Row label={t`Current`} value={audit.tag} mono />
						<Row label={t`Latest in line`} value={audit.line_latest_tag || audit.latest_tag || ""} mono />
						{audit.same_major_latest_tag && audit.same_major_latest_tag !== (audit.line_latest_tag || audit.latest_tag) && (
							<Row label={t`Latest same major`} value={audit.same_major_latest_tag} mono />
						)}
						{audit.overall_latest_tag &&
							audit.overall_latest_tag !== audit.same_major_latest_tag &&
							audit.overall_latest_tag !== (audit.line_latest_tag || audit.latest_tag) && (
								<Row label={t`Latest overall`} value={audit.overall_latest_tag} mono />
							)}
						{audit.major_update_available && audit.new_major_tag && (
							<Row label={t`New major available`} value={audit.new_major_tag} mono />
						)}
					</section>

					<section className="space-y-2 rounded-md border border-border/60 p-3">
						<h3 className="text-xs font-semibold uppercase tracking-wide text-muted-foreground">
							<Trans>Digests</Trans>
						</h3>
						<Row label={t`Local`} value={audit.local_digest} mono />
						<Row label={t`Latest`} value={audit.latest_digest} mono />
					</section>

					<section className="space-y-2 rounded-md border border-border/60 p-3">
						<h3 className="text-xs font-semibold uppercase tracking-wide text-muted-foreground">
							<Trans>Source</Trans>
						</h3>
						<Row label={t`Image ref`} value={audit.current_ref || entry.image_ref || entry.image} mono />
						<Row label={t`Registry`} value={audit.registry} mono />
						<Row label={t`Repository`} value={audit.repository} mono />
					</section>

					{admin && (
						<section className="flex flex-wrap gap-2">
							<Button variant="outline" size="sm" onClick={() => onPin(entry)}>
								<Trans>Pin to current tag</Trans>
							</Button>
							<Button variant="outline" size="sm" onClick={() => onDisable(entry)}>
								<Trans>Disable audit</Trans>
							</Button>
						</section>
					)}
				</div>
			</SheetContent>
		</Sheet>
	)
}

// ── page ─────────────────────────────────────────────────────────────────────

export default function ImagesPage() {
	const { t } = useLingui()
	const { containers, loading, refetch } = useImageAudits()
	const [search, setSearch] = useState("")
	const [auditing, setAuditing] = useState(false)
	const [selected, setSelected] = useState<AuditedEntry | null>(null)
	const admin = isAdmin()

	// Re-resolve the selected entry against the latest data so the drawer
	// reflects the most recent audit state without a manual refresh.
	const selectedKey = selected ? `${selected.host_id}|${selected.id}` : null
	const liveSelected = useMemo(
		() => (selectedKey ? containers.find((c) => `${c.host_id}|${c.id}` === selectedKey) ?? null : null),
		[containers, selectedKey]
	)

	const filtered = useMemo(() => {
		const q = search.trim().toLowerCase()
		if (!q) return containers
		return containers.filter((c) =>
			[c.name, c.id, c.host_name, c.host_id, c.image, c.image_audit?.tag, c.image_audit?.latest_tag]
				.filter(Boolean)
				.some((v) => (v as string).toLowerCase().includes(q))
		)
	}, [containers, search])

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

	const upsertOverride = useCallback(
		async (entry: AuditedEntry, payload: { policy: string; tag_include?: string; tag_exclude?: string }) => {
			try {
				await pb.send("/api/app/container-audit-overrides", {
					method: "PUT",
					body: JSON.stringify({
						agent: entry.host_id,
						container_name: entry.name,
						policy: payload.policy,
						tag_include: payload.tag_include ?? "",
						tag_exclude: payload.tag_exclude ?? "",
					}),
					headers: { "Content-Type": "application/json" },
				})
				toast({ title: t`Audit policy updated` })
				refetch()
			} catch (error: unknown) {
				toast({
					title: t`Failed to update audit policy`,
					description: (error as Error).message,
					variant: "destructive",
				})
			}
		},
		[refetch, t]
	)

	const pin = useCallback(
		async (entry: AuditedEntry) => {
			// Pin = lock to the current tag's patch line (no upgrade until manual change).
			await upsertOverride(entry, {
				policy: "patch",
				tag_include: `^${entry.image_audit.tag.replace(/[.+?^${}()|[\]\\]/g, "\\$&")}$`,
			})
			setSelected(null)
		},
		[upsertOverride]
	)

	const disable = useCallback(
		async (entry: AuditedEntry) => {
			await upsertOverride(entry, { policy: "disabled" })
			setSelected(null)
		},
		[upsertOverride]
	)

	return (
		<div className="space-y-4 pb-10">
			<div className="flex flex-col gap-2 sm:flex-row sm:items-center sm:justify-between">
				<div className="flex items-center gap-2">
					<BoxesIcon className="size-5 text-muted-foreground" />
					<h1 className="text-xl font-semibold">
						<Trans>Container images</Trans>
					</h1>
					{!loading && (
						<span className="text-sm text-muted-foreground">
							<Plural value={containers.length} one="# audited container" other="# audited containers" />
						</span>
					)}
				</div>
				<div className="flex flex-wrap items-center gap-2">
					<Input
						placeholder={t`Search container, host, image…`}
						value={search}
						onChange={(e) => setSearch(e.target.value)}
						className="sm:w-72"
					/>
					{admin && (
						<Button variant="outline" disabled={auditing} onClick={runAuditNow}>
							{auditing ? (
								<Loader2Icon className="me-2 size-4 animate-spin" />
							) : (
								<RefreshCwIcon className="me-2 size-4" />
							)}
							<Trans>Check images now</Trans>
						</Button>
					)}
				</div>
			</div>

			<CountersStrip counts={counts} />

			<div className="space-y-3">
				{bucketOrder.map((bucket) => (
					<AuditGroup
						key={bucket}
						bucket={bucket}
						entries={grouped[bucket]}
						onSelect={setSelected}
						defaultOpen={bucket === "major" || bucket === "update" || bucket === "failed"}
					/>
				))}
			</div>

			{!loading && containers.length === 0 && (
				<div className="rounded-lg border border-dashed border-border/60 px-6 py-10 text-center text-sm text-muted-foreground">
					<Trans>No image audit data yet. Run a check to populate this view.</Trans>
				</div>
			)}

			<AuditDetail entry={liveSelected} onClose={() => setSelected(null)} onPin={pin} onDisable={disable} />
		</div>
	)
}
