import { Trans, useLingui } from "@lingui/react/macro"
import {
	type Column,
	type ColumnDef,
	type SortingState,
	type PaginationState,
	flexRender,
	getCoreRowModel,
	getPaginationRowModel,
	getSortedRowModel,
	useReactTable,
} from "@tanstack/react-table"
import { CheckIcon, ChevronDownIcon, CopyIcon, PartyPopperIcon } from "lucide-react"
import { memo, useEffect, useMemo, useRef, useState } from "react"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table"
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip"
import { containerSeverity, isStoppedContainerStatus } from "@/lib/container-status"
import { cn, copyToClipboard } from "@/lib/utils"
import type { ContainerFleetEntry } from "@/lib/dashboard-types"
import { applyContainersFilters, ContainersFilterSheet, type ContainersFilters } from "./containers-filter-sheet"

// ── helpers ──────────────────────────────────────────────────────────────────

function containerUptime(status: string, statusText: string): string {
	if (!statusText) return "—"
	if (status === "running") return statusText.replace(/^Up\s+/i, "")
	if (status === "restarting") return statusText.replace(/^Restarting\s*\([^)]*\)\s*/i, "")
	return "—"
}

function containerStatusRank(status: string): number {
	switch (status) {
		case "running":
			return 6
		case "restarting":
			return 5
		case "paused":
			return 4
		case "created":
			return 3
		case "exited":
			return 2
		case "dead":
			return 1
		default:
			return 0
	}
}

function displayImageRef(container: ContainerFleetEntry): string {
	if (container.image && !container.image.startsWith("sha256:")) {
		return container.image
	}
	if (container.image_ref) {
		return container.image_ref
	}
	return container.image || "—"
}

// ── sub-components ────────────────────────────────────────────────────────────

function ContainerStatusBadge({ container }: { container: Pick<ContainerFleetEntry, "status" | "exit_code"> }) {
	const { t } = useLingui()
	const { status } = container
	const label =
		status === "running"
			? t`Running`
			: status === "exited"
				? t`Exited`
				: status === "restarting"
					? t`Restarting`
					: status === "paused"
						? t`Paused`
						: status === "created"
							? t`Created`
							: status === "dead"
								? t`Dead`
								: t`Unknown`

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
		<Badge variant="outline" className={cn("text-[10px]", cls)}>
			{label}
		</Badge>
	)
}

function StatusCell({ container }: { container: ContainerFleetEntry }) {
	const { t } = useLingui()
	const uptime = containerUptime(container.status, container.status_text)
	const isRunning = container.status === "running"

	const badge = <ContainerStatusBadge container={container} />

	if (isRunning) return badge

	const popoverRows: Array<{ label: string; value: string }> = [{ label: t`State`, value: container.status }]
	if (isStoppedContainerStatus(container.status)) {
		popoverRows.push({ label: t`Exited since`, value: uptime !== "—" ? uptime : t`unknown` })
		if (container.exit_code !== null && container.exit_code !== undefined) {
			popoverRows.push({ label: t`Exit code`, value: String(container.exit_code) })
		}
	} else if (container.status === "restarting") {
		popoverRows.push({ label: t`Restarting for`, value: uptime !== "—" ? uptime : t`unknown` })
	}
	if (container.status_text) {
		popoverRows.push({ label: t`Detail`, value: container.status_text })
	}

	return (
		<div className="group flex items-center">
			{badge}
			<Tooltip>
				<TooltipTrigger asChild>
					<button
						type="button"
						className="ml-1.5 inline-flex size-[15px] shrink-0 cursor-default items-center justify-center rounded-full border border-border/50 bg-background/80 text-[9px] font-bold text-muted-foreground opacity-60 transition-opacity group-hover:opacity-100 hover:border-border hover:text-foreground focus-visible:opacity-100 focus-visible:outline-none"
						onClick={(e) => e.stopPropagation()}
						tabIndex={-1}
					>
						i
					</button>
				</TooltipTrigger>
				<TooltipContent side="bottom" className="max-w-[min(32rem,calc(100vw-2rem))] min-w-[220px] p-2.5">
					<div className="space-y-1.5">
						{popoverRows.map((r, i) => (
							<div key={i} className="flex justify-between gap-4 text-xs">
								<span className="text-muted-foreground">{r.label}</span>
								<span className="max-w-[18rem] break-all text-right font-medium">{r.value}</span>
							</div>
						))}
					</div>
				</TooltipContent>
			</Tooltip>
		</div>
	)
}

function ImageAuditBadge({ container }: { container: ContainerFleetEntry }) {
	const { t } = useLingui()
	const audit = container.image_audit
	if (!audit) {
		return (
			<Badge variant="outline" className="border-border/50 text-[10px] text-muted-foreground">
				{t`Not checked`}
			</Badge>
		)
	}

	const lineStatus = audit.line_status || audit.status
	const lineLatestTag = audit.line_latest_tag || audit.latest_tag || ""
	const sameMajorTag = audit.same_major_latest_tag || ""
	const overallTag = audit.overall_latest_tag || ""
	const showLineTarget =
		(lineStatus === "patch_available" || lineStatus === "minor_available" || audit.status === "update_available") &&
		lineLatestTag &&
		lineLatestTag !== audit.tag
	const showSameMajorTarget = sameMajorTag !== "" && sameMajorTag !== lineLatestTag
	const showOverallTarget = overallTag !== "" && overallTag !== sameMajorTag && overallTag !== lineLatestTag

	const cls =
		lineStatus === "up_to_date"
			? "border-emerald-500/30 bg-emerald-500/10 text-emerald-400"
			: lineStatus === "patch_available" ||
					lineStatus === "minor_available" ||
					lineStatus === "tag_rebuilt" ||
					audit.status === "update_available"
				? "border-amber-500/30 bg-amber-500/10 text-amber-400"
				: audit.status === "check_failed"
					? "border-red-500/30 bg-red-500/10 text-red-400"
					: "border-border/50 text-muted-foreground"

	const label =
		lineStatus === "up_to_date"
			? t`Up to date`
			: lineStatus === "patch_available"
				? t`Patch available`
				: lineStatus === "minor_available"
					? t`Minor available`
					: lineStatus === "tag_rebuilt"
						? t`Tag rebuilt`
						: audit.status === "update_available"
							? t`Update available`
							: audit.status === "unsupported"
								? t`Unsupported`
								: audit.status === "check_failed"
									? t`Check failed`
									: t`Unknown`

	const rows: Array<{ label: string; value: string }> = [
		{ label: t`Policy`, value: audit.policy || "—" },
		{ label: t`Current`, value: audit.current_ref || container.image_ref || container.image || "—" },
	]
	if (lineLatestTag) {
		rows.push({ label: t`Latest in line`, value: lineLatestTag })
	}
	if (showSameMajorTarget) {
		rows.push({ label: t`Latest same major`, value: sameMajorTag })
	}
	if (showOverallTarget) {
		rows.push({ label: t`Latest overall`, value: overallTag })
	}
	if (audit.local_image_id) {
		rows.push({ label: t`Local image id`, value: audit.local_image_id })
	}
	if (audit.latest_image_id) {
		rows.push({ label: t`Latest image id`, value: audit.latest_image_id })
	}
	rows.push({ label: t`Checked`, value: audit.checked_at ? new Date(audit.checked_at).toLocaleString() : "—" })
	if (audit.error) {
		rows.push({ label: t`Error`, value: audit.error })
	}

	return (
		<div className="group flex flex-wrap items-center gap-1.5">
			<Badge variant="outline" className={cn("text-[10px]", cls)}>
				{label}
			</Badge>
			{showLineTarget && (
				<Badge variant="outline" className="border-amber-500/30 bg-amber-500/10 font-mono text-[10px] text-amber-300">
					{lineLatestTag}
				</Badge>
			)}
			{showSameMajorTarget && (
				<Badge
					variant="outline"
					className="border-violet-500/30 bg-violet-500/10 font-mono text-[10px] text-violet-300"
				>
					{sameMajorTag}
				</Badge>
			)}
			{audit.major_update_available && audit.new_major_tag && (
				<Badge variant="outline" className="border-sky-500/30 bg-sky-500/10 font-mono text-[10px] text-sky-300">
					<PartyPopperIcon className="mr-1 size-3" aria-hidden="true" />
					<span>{audit.new_major_tag}</span>
				</Badge>
			)}
			<Tooltip>
				<TooltipTrigger asChild>
					<button
						type="button"
						className="inline-flex size-[15px] shrink-0 cursor-default items-center justify-center rounded-full border border-border/50 bg-background/80 text-[9px] font-bold text-muted-foreground opacity-60 transition-opacity group-hover:opacity-100 hover:border-border hover:text-foreground focus-visible:opacity-100 focus-visible:outline-none"
						onClick={(e) => e.stopPropagation()}
						tabIndex={-1}
					>
						i
					</button>
				</TooltipTrigger>
				<TooltipContent side="bottom" className="max-w-[min(36rem,calc(100vw-2rem))] min-w-[240px] p-2.5">
					<div className="space-y-1.5">
						{rows.map((r, i) => (
							<div key={i} className="flex justify-between gap-4 text-xs">
								<span className="text-muted-foreground">{r.label}</span>
								<span className="max-w-[22rem] break-all text-right font-medium">{r.value}</span>
							</div>
						))}
					</div>
				</TooltipContent>
			</Tooltip>
		</div>
	)
}

function ImageCell({ container }: { container: ContainerFleetEntry }) {
	const { t } = useLingui()
	const imageRef = displayImageRef(container)
	const resetTimeoutRef = useRef<number | null>(null)
	const [copied, setCopied] = useState(false)

	useEffect(() => {
		return () => {
			if (resetTimeoutRef.current !== null) {
				window.clearTimeout(resetTimeoutRef.current)
			}
		}
	}, [])

	if (imageRef === "—") {
		return <span className="font-mono text-xs">{imageRef}</span>
	}

	async function handleCopy() {
		await copyToClipboard(imageRef)
		setCopied(true)
		if (resetTimeoutRef.current !== null) {
			window.clearTimeout(resetTimeoutRef.current)
		}
		resetTimeoutRef.current = window.setTimeout(() => {
			setCopied(false)
			resetTimeoutRef.current = null
		}, 1500)
	}

	return (
		<div className="flex max-w-[24rem] items-center gap-1.5">
			<span className="truncate font-mono text-xs">{imageRef}</span>
			<Tooltip disableHoverableContent={true}>
				<TooltipTrigger asChild>
					<Button
						type="button"
						variant="ghost"
						size="icon"
						className={cn("h-5 w-5 shrink-0", copied && "text-emerald-500 hover:text-emerald-500")}
						onClick={handleCopy}
						aria-label={copied ? t`Copied to clipboard` : t`Click to copy`}
					>
						{copied ? <CheckIcon className="h-3 w-3" /> : <CopyIcon className="h-3 w-3" />}
					</Button>
				</TooltipTrigger>
				<TooltipContent>
					<p>{copied ? t`Copied to clipboard` : t`Click to copy`}</p>
				</TooltipContent>
			</Tooltip>
		</div>
	)
}

function SortBtn({ column, children }: { column: Column<ContainerFleetEntry, unknown>; children: React.ReactNode }) {
	const sorted = column.getIsSorted()
	return (
		<button
			type="button"
			className="flex items-center gap-1 text-left text-xs font-medium tracking-wide text-muted-foreground hover:text-foreground"
			onClick={() => column.toggleSorting(sorted === "asc")}
		>
			{children}
			<span className="text-[10px] opacity-60">{sorted === "asc" ? "↑" : sorted === "desc" ? "↓" : "↕"}</span>
		</button>
	)
}

// ── props ─────────────────────────────────────────────────────────────────────

interface ContainersTableProps {
	containers: ContainerFleetEntry[]
	filters: ContainersFilters
	onFiltersChange: (next: ContainersFilters) => void
}

// ── main component ────────────────────────────────────────────────────────────

export const ContainersTable = memo(function ContainersTable({
	containers,
	filters,
	onFiltersChange,
}: ContainersTableProps) {
	const { t } = useLingui()

	const [sorting, setSorting] = useState<SortingState>([{ id: "status_rank", desc: true }])
	const [pagination, setPagination] = useState<PaginationState>({ pageIndex: 0, pageSize: 10 })
	const [search, setSearch] = useState("")

	useEffect(() => {
		setPagination((p) => ({ ...p, pageIndex: 0 }))
	}, [filters])

	const filteredContainers = useMemo(() => {
		const result = applyContainersFilters(containers, filters)
		if (!search) return result
		const q = search.toLowerCase()
		return result.filter((c) =>
			[
				c.host_name,
				c.host_ip,
				c.name,
				c.id,
				c.image,
				c.image_ref,
				c.status_text,
				c.ports,
				c.image_audit?.latest_tag,
				c.image_audit?.status,
			].some((v) => v?.toLowerCase().includes(q))
		)
	}, [containers, filters, search])

	function handleSearch(value: string) {
		setSearch(value)
		setPagination((p) => ({ ...p, pageIndex: 0 }))
	}

	const columns: ColumnDef<ContainerFleetEntry>[] = useMemo(
		() => [
			{
				id: "host",
				accessorFn: (c) => c.host_name || c.host_id || "",
				header: ({ column }) => (
					<SortBtn column={column}>
						<Trans>Host</Trans>
					</SortBtn>
				),
				cell: ({ row: { original: c } }) => (
					<div>
						<div className="font-semibold">{c.host_name || c.host_id}</div>
						{c.host_ip && <div className="font-mono text-xs text-muted-foreground">{c.host_ip}</div>}
					</div>
				),
			},
			{
				id: "name",
				accessorFn: (c) => c.name || "",
				header: ({ column }) => (
					<SortBtn column={column}>
						<Trans>Container</Trans>
					</SortBtn>
				),
				cell: ({ row: { original: c } }) => (
					<div>
						<div className="font-semibold">{c.name || c.id || "—"}</div>
						{c.id && <div className="font-mono text-xs text-muted-foreground">{c.id.slice(0, 12)}</div>}
					</div>
				),
			},
			{
				id: "image",
				accessorFn: (c) => displayImageRef(c),
				header: ({ column }) => (
					<SortBtn column={column}>
						<Trans>Image</Trans>
					</SortBtn>
				),
				cell: ({ row: { original: c } }) => <ImageCell container={c} />,
			},
			{
				id: "image_audit",
				accessorFn: (c) => c.image_audit?.status || "not_checked",
				header: ({ column }) => (
					<SortBtn column={column}>
						<Trans>Image audit</Trans>
					</SortBtn>
				),
				cell: ({ row: { original: c } }) => <ImageAuditBadge container={c} />,
			},
			{
				id: "status_rank",
				accessorFn: (c) => containerStatusRank(c.status),
				header: ({ column }) => (
					<SortBtn column={column}>
						<Trans>Status</Trans>
					</SortBtn>
				),
				cell: ({ row: { original: c } }) => <StatusCell container={c} />,
			},
			{
				id: "uptime",
				accessorFn: (c) => containerStatusRank(c.status),
				header: ({ column }) => (
					<SortBtn column={column}>
						<Trans>Uptime</Trans>
					</SortBtn>
				),
				cell: ({ row: { original: c } }) => (
					<span className="tabular-nums text-sm text-muted-foreground">
						{containerUptime(c.status, c.status_text) || "—"}
					</span>
				),
			},
			{
				id: "ports",
				accessorFn: (c) => c.ports || "",
				header: ({ column }) => (
					<SortBtn column={column}>
						<Trans>Ports</Trans>
					</SortBtn>
				),
				cell: ({ row: { original: c } }) => (
					<span className="font-mono text-xs text-muted-foreground">{c.ports || "—"}</span>
				),
			},
		],
		[t]
	)

	const table = useReactTable({
		data: filteredContainers,
		columns,
		state: { sorting, pagination },
		onSortingChange: setSorting,
		onPaginationChange: setPagination,
		getCoreRowModel: getCoreRowModel(),
		getSortedRowModel: getSortedRowModel(),
		getPaginationRowModel: getPaginationRowModel(),
		manualFiltering: true,
		autoResetPageIndex: false,
	})

	return (
		<div className="space-y-3">
			<div className="flex flex-col gap-2 sm:flex-row sm:flex-wrap sm:items-center">
				<Input
					placeholder={t`Search container, image, host, ports…`}
					value={search}
					onChange={(e) => handleSearch(e.target.value)}
					className="sm:max-w-[280px]"
				/>
				<ContainersFilterSheet
					filters={filters}
					onFiltersChange={onFiltersChange}
					search={search}
					onSearchChange={handleSearch}
				/>
				<div className="ml-auto flex shrink-0 items-center gap-2">
					<span className="text-xs text-muted-foreground">
						<Trans>Rows</Trans>
					</span>
					<Select
						value={String(pagination.pageSize)}
						onValueChange={(v) => setPagination({ pageIndex: 0, pageSize: Number(v) })}
					>
						<SelectTrigger className="w-[72px]">
							<SelectValue />
						</SelectTrigger>
						<SelectContent>
							{[10, 25, 50, 100].map((n) => (
								<SelectItem key={n} value={String(n)}>
									{n}
								</SelectItem>
							))}
						</SelectContent>
					</Select>
				</div>
			</div>

			<div className="overflow-x-auto rounded-md border border-border/60">
				<Table>
					<TableHeader>
						{table.getHeaderGroups().map((hg) => (
							<TableRow key={hg.id}>
								{hg.headers.map((header) => (
									<TableHead key={header.id}>
										{header.isPlaceholder ? null : flexRender(header.column.columnDef.header, header.getContext())}
									</TableHead>
								))}
							</TableRow>
						))}
					</TableHeader>
					<TableBody>
						{table.getRowModel().rows.length === 0 ? (
							<TableRow>
								<TableCell colSpan={columns.length} className="h-16 text-center text-sm text-muted-foreground">
									<Trans>No containers match the current filter.</Trans>
								</TableCell>
							</TableRow>
						) : (
							table.getRowModel().rows.map((row) => (
								<TableRow key={row.id}>
									{row.getVisibleCells().map((cell) => (
										<TableCell key={cell.id}>{flexRender(cell.column.columnDef.cell, cell.getContext())}</TableCell>
									))}
								</TableRow>
							))
						)}
					</TableBody>
				</Table>
			</div>

			<div className="flex items-center justify-between text-xs text-muted-foreground">
				<span>
					{filteredContainers.length} container{filteredContainers.length !== 1 ? "s" : ""}
				</span>
				<div className="flex items-center gap-2">
					<Button
						variant="outline"
						size="sm"
						onClick={() => table.previousPage()}
						disabled={!table.getCanPreviousPage()}
					>
						<ChevronDownIcon className="size-3 rotate-90" />
					</Button>
					<span>
						{pagination.pageIndex + 1} / {Math.max(1, table.getPageCount())}
					</span>
					<Button variant="outline" size="sm" onClick={() => table.nextPage()} disabled={!table.getCanNextPage()}>
						<ChevronDownIcon className="size-3 -rotate-90" />
					</Button>
				</div>
			</div>
		</div>
	)
})
