import { Plural, Trans, useLingui } from "@lingui/react/macro"
import {
	type Column,
	type ColumnDef,
	type PaginationState,
	type SortingState,
	flexRender,
	getCoreRowModel,
	getPaginationRowModel,
	getSortedRowModel,
	useReactTable,
} from "@tanstack/react-table"
import { ChevronDownIcon, MoreHorizontalIcon, TagIcon, XIcon } from "lucide-react"
import { memo, useEffect, useMemo, useState } from "react"
import { getPagePath } from "@nanostores/router"
import { $router, Link } from "@/components/router"
import { TagsDialog } from "@/components/tags-dialog"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { CopyButton } from "@/components/ui/copy-button"
import { DropdownMenu, DropdownMenuContent, DropdownMenuItem, DropdownMenuTrigger } from "@/components/ui/dropdown-menu"
import { Input } from "@/components/ui/input"
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table"
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip"
import { MetricBar } from "@/components/metric-charts"
import { isReadOnlyUser } from "@/lib/api"
import { muteKey, useMutes } from "@/lib/mutes"
import { MuteBadge, MuteBellButton } from "@/components/mute-menu"
import { formatBytesPerSecond } from "@/lib/format"
import type { HostMetrics, HostsOverviewRecord } from "@/lib/dashboard-types"
import {
	applyHostsFilters,
	defaultHostsFilters,
	HostsFilterSheet,
	type HostsCompliance,
	type HostsFilters,
} from "./hosts-filter-sheet"

function FilterPill({ label, onRemove }: { label: string; onRemove: () => void }) {
	return (
		<span className="inline-flex items-center gap-1 rounded-full border border-border/60 bg-muted/40 px-2.5 py-0.5 text-xs font-medium text-foreground">
			{label}
			<button
				type="button"
				onClick={onRemove}
				className="ml-0.5 rounded-full text-muted-foreground transition-colors hover:text-foreground"
			>
				<XIcon className="size-3" />
				<span className="sr-only">
					<Trans>Remove</Trans>
				</span>
			</button>
		</span>
	)
}

function InfoBtn({ rows }: { rows: Array<{ label: string; value: string | number }> }) {
	return (
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
					{rows.map((r, i) => (
						<div key={i} className="flex justify-between gap-4 text-xs">
							<span className="text-muted-foreground">{r.label}</span>
							<span className="max-w-[18rem] break-all text-right font-medium">{String(r.value)}</span>
						</div>
					))}
				</div>
			</TooltipContent>
		</Tooltip>
	)
}

// Per-row quick actions on the hosts table — currently editing tags without
// leaving the overview. Reuses the shared TagsDialog; non-readonly only.
// (Notification muting lives in its own far-right bell column, not here.)
function HostRowActions({ host }: { host: HostsOverviewRecord }) {
	const { t } = useLingui()
	const [tagsOpen, setTagsOpen] = useState(false)
	return (
		<>
			<TagsDialog
				agentId={host.id}
				currentTags={host.tags ?? []}
				title={host.name || host.hostname || host.id}
				open={tagsOpen}
				onClose={() => setTagsOpen(false)}
			/>
			<DropdownMenu>
				<DropdownMenuTrigger asChild>
					<Button
						variant="ghost"
						size="icon"
						data-nolink
						className="ml-1 size-6 shrink-0 text-muted-foreground opacity-0 transition-opacity group-hover:opacity-100 focus-visible:opacity-100"
						onClick={(e) => e.stopPropagation()}
					>
						<span className="sr-only">{t`Host actions`}</span>
						<MoreHorizontalIcon className="size-4" />
					</Button>
				</DropdownMenuTrigger>
				<DropdownMenuContent align="end">
					<DropdownMenuItem onSelect={() => setTagsOpen(true)}>
						<TagIcon className="me-2.5 size-4" />
						<Trans>Edit tags</Trans>
					</DropdownMenuItem>
				</DropdownMenuContent>
			</DropdownMenu>
		</>
	)
}

function SortBtn({ column, children }: { column: Column<HostsOverviewRecord, unknown>; children: React.ReactNode }) {
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

function statusRank(host: HostsOverviewRecord): number {
	return host.status === "connected" ? 1 : 0
}

function metricPercentValue(host: HostsOverviewRecord, selector: (metrics: HostMetrics) => number): number {
	return host.metrics ? selector(host.metrics) : -1
}

interface HostsTableProps {
	hosts: HostsOverviewRecord[]
	filters: HostsFilters
	onFiltersChange: (next: HostsFilters) => void
}

export const HostsTable = memo(function HostsTable({ hosts, filters, onFiltersChange }: HostsTableProps) {
	const { t } = useLingui()
	const readOnly = isReadOnlyUser()
	const mutes = useMutes()
	const [sorting, setSorting] = useState<SortingState>([{ id: "connection", desc: true }])
	const [pagination, setPagination] = useState<PaginationState>({ pageIndex: 0, pageSize: 10 })
	const [search, setSearch] = useState("")

	useEffect(() => {
		setPagination((p) => ({ ...p, pageIndex: 0 }))
	}, [filters])

	const filteredHosts = useMemo(() => {
		const result = applyHostsFilters(hosts, filters)
		if (!search) return result
		const q = search.toLowerCase()
		return result.filter((h) =>
			[h.name, h.hostname, h.primary_ip, h.network?.gateway, h.os?.name, h.kernel, h.version, ...(h.tags ?? [])].some(
				(v) => v?.toLowerCase().includes(q)
			)
		)
	}, [hosts, filters, search])

	const availableTags = useMemo(() => {
		const tags = new Set<string>()
		for (const h of hosts) for (const tag of h.tags ?? []) tags.add(tag)
		return Array.from(tags).sort()
	}, [hosts])

	function handleSearch(value: string) {
		setSearch(value)
		setPagination((p) => ({ ...p, pageIndex: 0 }))
	}

	function resetAll() {
		onFiltersChange(defaultHostsFilters)
		handleSearch("")
	}

	const complianceLabelMap = useMemo<Record<HostsCompliance, string>>(
		() => ({
			security: t`Security`,
			reboot: t`Reboot req.`,
			stale: t`Out of SLA`,
			unknown: t`Unknown`,
			clean: t`Compliant`,
		}),
		[t]
	)

	const columns: ColumnDef<HostsOverviewRecord>[] = useMemo(
		() => [
			{
				id: "connection",
				accessorFn: statusRank,
				header: ({ column }) => <SortBtn column={column}>UP</SortBtn>,
				cell: ({ row: { original: h } }) =>
					h.status === "connected" ? (
						<Badge variant="outline" className="border-emerald-500/40 bg-emerald-500/10 text-[10px] text-emerald-500">
							UP
						</Badge>
					) : (
						<Badge variant="outline" className="border-border/50 text-[10px] text-muted-foreground">
							DOWN
						</Badge>
					),
			},
			{
				accessorKey: "name",
				header: ({ column }) => (
					<SortBtn column={column}>
						<Trans>Host</Trans>
					</SortBtn>
				),
				cell: ({ row: { original: h } }) => (
					<div className="group flex items-center">
						<div>
							<Link href={getPagePath($router, "host", { id: h.id })} className="font-semibold hover:underline">
								{h.name || h.hostname || h.id}
							</Link>
							<div className="flex items-center gap-1.5 text-xs text-muted-foreground">
								{h.hostname && h.hostname !== h.name && <span>{h.hostname}</span>}
								{h.hostname && h.hostname !== h.name && h.primary_ip && <span aria-hidden>·</span>}
								{h.primary_ip ? (
									<span className="inline-flex items-center gap-1">
										{h.primary_ip}
										<CopyButton
											value={h.primary_ip}
											label={t`Copy IP`}
											className="opacity-0 transition group-hover:opacity-100"
										/>
									</span>
								) : (
									!(h.hostname && h.hostname !== h.name) && <span>—</span>
								)}
							</div>
							{h.tags && h.tags.length > 0 && (
								<div className="mt-1 flex flex-wrap gap-1">
									{h.tags.map((tag) => (
										<Badge key={tag} variant="secondary" className="px-1.5 py-0 text-[10px] font-normal">
											{tag}
										</Badge>
									))}
								</div>
							)}
						</div>
						<InfoBtn
							rows={[
								{ label: t`Platform`, value: h.os ? `${h.os.name} ${h.os.version}`.trim() : "—" },
								{ label: t`Kernel`, value: h.kernel || "—" },
								{ label: t`CPU`, value: h.resources?.cpu_model || "—" },
								{ label: t`Last snapshot`, value: h.collected_at || "—" },
								{ label: t`Last metrics`, value: h.metrics?.collected_at || "—" },
							]}
						/>
						{!readOnly && <HostRowActions host={h} />}
					</div>
				),
			},
			{
				id: "cpu",
				accessorFn: (h) => metricPercentValue(h, (metrics) => metrics.cpu_percent),
				header: ({ column }) => (
					<SortBtn column={column}>
						<Trans>CPU</Trans>
					</SortBtn>
				),
				cell: ({ row: { original: h } }) => <MetricBar value={h.metrics?.cpu_percent} />,
			},
			{
				id: "memory",
				accessorFn: (h) => metricPercentValue(h, (metrics) => metrics.memory_used_percent),
				header: ({ column }) => (
					<SortBtn column={column}>
						<Trans>Memory</Trans>
					</SortBtn>
				),
				cell: ({ row: { original: h } }) => <MetricBar value={h.metrics?.memory_used_percent} />,
			},
			{
				id: "disk",
				accessorFn: (h) => metricPercentValue(h, (metrics) => metrics.disk_used_percent),
				header: ({ column }) => (
					<SortBtn column={column}>
						<Trans>Disk</Trans>
					</SortBtn>
				),
				cell: ({ row: { original: h } }) => (
					<MetricBar
						value={h.metrics?.disk_used_percent}
						tone={h.metrics && h.metrics.disk_used_percent >= 75 ? "amber" : "emerald"}
					/>
				),
			},
			{
				id: "network",
				accessorFn: (h) => (h.metrics?.network_rx_bps ?? 0) + (h.metrics?.network_tx_bps ?? 0),
				header: ({ column }) => (
					<SortBtn column={column}>
						<Trans>Net</Trans>
					</SortBtn>
				),
				cell: ({ row: { original: h } }) => (
					<div className="space-y-0.5 text-xs tabular-nums">
						<div>{formatBytesPerSecond(h.metrics?.network_rx_bps ?? 0)} ↓</div>
						<div className="text-muted-foreground">{formatBytesPerSecond(h.metrics?.network_tx_bps ?? 0)} ↑</div>
					</div>
				),
			},
			{
				id: "agent",
				accessorFn: (h) => h.version || "",
				header: ({ column }) => (
					<SortBtn column={column}>
						<Trans>Agent</Trans>
					</SortBtn>
				),
				cell: ({ row: { original: h } }) => (
					<span className="font-mono text-xs text-muted-foreground">{h.version || "—"}</span>
				),
			},
			{
				id: "mute",
				enableSorting: false,
				header: () => (
					<span className="sr-only">
						<Trans>Notifications</Trans>
					</span>
				),
				cell: ({ row: { original: h } }) => {
					const activeMute = mutes.get(muteKey("agent", h.id))
					return (
						<div className="flex justify-end">
							{readOnly ? (
								<MuteBadge activeMute={activeMute} />
							) : (
								<MuteBellButton type="agent" id={h.id} activeMute={activeMute} />
							)}
						</div>
					)
				},
			},
		],
		[t, readOnly, mutes]
	)

	const table = useReactTable({
		data: filteredHosts,
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
					placeholder={t`Search host, IP, OS, kernel…`}
					value={search}
					onChange={(e) => handleSearch(e.target.value)}
					className="sm:max-w-[260px]"
				/>
				<HostsFilterSheet
					filters={filters}
					onFiltersChange={onFiltersChange}
					search={search}
					onSearchChange={handleSearch}
					availableTags={availableTags}
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

			{(search ||
				filters.connection !== "all" ||
				filters.compliance.size > 0 ||
				filters.features.size > 0 ||
				filters.tags.size > 0) && (
				<div className="flex flex-wrap gap-1.5">
					{search && <FilterPill label={`"${search}"`} onRemove={() => handleSearch("")} />}
					{filters.connection !== "all" && (
						<FilterPill
							label={filters.connection === "connected" ? t`Online` : t`Offline`}
							onRemove={() => onFiltersChange({ ...filters, connection: "all" })}
						/>
					)}
					{[...filters.compliance].map((flag) => (
						<FilterPill
							key={flag}
							label={complianceLabelMap[flag]}
							onRemove={() => {
								const next = new Set(filters.compliance)
								next.delete(flag)
								onFiltersChange({ ...filters, compliance: next })
							}}
						/>
					))}
					{[...filters.features].map((flag) => (
						<FilterPill
							key={flag}
							label={flag === "docker" ? t`Docker` : flag}
							onRemove={() => {
								const next = new Set(filters.features)
								next.delete(flag)
								onFiltersChange({ ...filters, features: next })
							}}
						/>
					))}
					{[...filters.tags].map((tag) => (
						<FilterPill
							key={tag}
							label={tag}
							onRemove={() => {
								const next = new Set(filters.tags)
								next.delete(tag)
								onFiltersChange({ ...filters, tags: next })
							}}
						/>
					))}
				</div>
			)}

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
								<TableCell colSpan={columns.length} className="h-24 text-center text-sm text-muted-foreground">
									<div className="flex flex-col items-center gap-2">
										<Trans>No hosts match the current filter.</Trans>
										<Button variant="link" size="sm" className="h-auto p-0 text-xs" onClick={resetAll}>
											<Trans>Reset filters</Trans>
										</Button>
									</div>
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
					<Plural value={filteredHosts.length} one="# host" other="# hosts" />
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
