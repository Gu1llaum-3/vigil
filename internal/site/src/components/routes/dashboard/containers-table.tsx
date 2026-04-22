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
import { ChevronDownIcon } from "lucide-react"
import { memo, useEffect, useMemo, useState } from "react"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table"
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip"
import { cn } from "@/lib/utils"
import type { ContainerFleetEntry } from "@/lib/dashboard-types"

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

function isStoppedContainer(status: string): boolean {
	return status === "exited" || status === "dead"
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

function ContainerStatusBadge({ status }: { status: string }) {
	const { t } = useLingui()
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

	const cls =
		status === "running"
			? "border-emerald-500/30 bg-emerald-500/10 text-emerald-400"
			: status === "restarting"
				? "border-amber-500/30 bg-amber-500/10 text-amber-400"
				: isStoppedContainer(status)
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

	const badge = <ContainerStatusBadge status={container.status} />

	if (isRunning) return badge

	const popoverRows: Array<{ label: string; value: string }> = [{ label: t`State`, value: container.status }]
	if (isStoppedContainer(container.status)) {
		popoverRows.push({ label: t`Exited since`, value: uptime !== "—" ? uptime : t`unknown` })
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

	const cls =
		audit.status === "up_to_date"
			? "border-emerald-500/30 bg-emerald-500/10 text-emerald-400"
			: audit.status === "update_available"
				? "border-amber-500/30 bg-amber-500/10 text-amber-400"
				: audit.status === "check_failed"
					? "border-red-500/30 bg-red-500/10 text-red-400"
					: "border-border/50 text-muted-foreground"

	const label =
		audit.status === "up_to_date"
			? t`Up to date`
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
		{ label: t`Local image id`, value: audit.local_image_id || "—" },
		{ label: t`Latest image id`, value: audit.latest_image_id || "—" },
		{ label: t`Latest tag`, value: audit.latest_tag || "—" },
		{ label: t`Checked`, value: audit.checked_at ? new Date(audit.checked_at).toLocaleString() : "—" },
	]
	if (audit.error) {
		rows.push({ label: t`Error`, value: audit.error })
	}

	return (
		<div className="group flex items-center">
			<Badge variant="outline" className={cn("text-[10px]", cls)}>
				{label}
			</Badge>
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
	chipFilter: string
	onChipFilterChange: (filter: string) => void
}

// ── main component ────────────────────────────────────────────────────────────

export const ContainersTable = memo(function ContainersTable({
	containers,
	chipFilter,
	onChipFilterChange,
}: ContainersTableProps) {
	const { t } = useLingui()

	const chips = useMemo(
		() => [
			{ key: "all", label: t`All` },
			{ key: "running", label: t`Running` },
			{ key: "stopped", label: t`Stopped` },
			{ key: "restarting", label: t`Restarting` },
			{ key: "paused", label: t`Paused` },
			{ key: "created", label: t`Created` },
			{ key: "updates", label: t`Updates` },
		],
		[t]
	)

	const [sorting, setSorting] = useState<SortingState>([{ id: "status_rank", desc: true }])
	const [pagination, setPagination] = useState<PaginationState>({ pageIndex: 0, pageSize: 10 })
	const [search, setSearch] = useState("")

	useEffect(() => {
		setPagination((p) => ({ ...p, pageIndex: 0 }))
	}, [chipFilter])

	const filteredContainers = useMemo(() => {
		let result = containers
			switch (chipFilter) {
			case "running":
				result = containers.filter((c) => c.status === "running")
				break
			case "stopped":
				result = containers.filter((c) => isStoppedContainer(c.status))
				break
			case "restarting":
				result = containers.filter((c) => c.status === "restarting")
				break
			case "paused":
				result = containers.filter((c) => c.status === "paused")
				break
			case "created":
				result = containers.filter((c) => c.status === "created")
				break
			case "updates":
				result = containers.filter((c) => c.image_audit?.status === "update_available")
				break
		}
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
	}, [containers, chipFilter, search])

	function handleChip(key: string) {
		onChipFilterChange(key)
		setPagination((p) => ({ ...p, pageIndex: 0 }))
	}

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
				cell: ({ row: { original: c } }) => <span className="font-mono text-xs">{displayImageRef(c)}</span>,
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
				<div className="flex flex-wrap gap-1.5">
					{chips.map((chip) => (
						<button
							key={chip.key}
							type="button"
							onClick={() => handleChip(chip.key)}
							className={cn(
								"rounded-full border px-3 py-1 text-xs font-semibold transition-colors",
								chipFilter === chip.key
									? "border-primary/60 bg-primary/10 text-foreground"
									: "border-border/60 bg-muted/30 text-muted-foreground hover:border-border hover:text-foreground"
							)}
						>
							{chip.label}
						</button>
					))}
				</div>
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
