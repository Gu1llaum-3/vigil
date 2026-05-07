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
import { ChevronDownIcon, XIcon } from "lucide-react"
import { memo, useEffect, useMemo, useState } from "react"
import { getPagePath } from "@nanostores/router"
import { $router, Link } from "@/components/router"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table"
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip"
import { cn } from "@/lib/utils"
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

function formatPercent(value?: number | null): string {
	if (value == null) return "—"
	return `${Math.round(value * 10) / 10}%`
}

function formatBytesPerSecond(bytesPerSecond?: number): string {
	if (!bytesPerSecond || bytesPerSecond <= 0) return "0 B/s"
	const units = ["B/s", "KB/s", "MB/s", "GB/s"]
	let value = bytesPerSecond
	let unit = 0
	while (value >= 1024 && unit < units.length - 1) {
		value /= 1024
		unit++
	}
	const digits = unit === 0 ? 0 : unit === 1 ? 1 : 2
	return `${value.toFixed(digits)} ${units[unit]}`
}

function statusRank(host: HostsOverviewRecord): number {
	return host.status === "connected" ? 1 : 0
}

function metricPercentValue(host: HostsOverviewRecord, selector: (metrics: HostMetrics) => number): number {
	return host.metrics ? selector(host.metrics) : -1
}

function MetricBar({ value, tone = "emerald" }: { value?: number | null; tone?: "emerald" | "amber" }) {
	const percent = Math.max(0, Math.min(100, value ?? 0))
	const barClass = tone === "amber" ? "bg-amber-500/80" : "bg-emerald-500/80"
	return (
		<div className="flex min-w-[180px] items-center gap-3">
			<span className="w-12 shrink-0 text-xs font-medium tabular-nums">{formatPercent(value)}</span>
			<div className="h-2.5 flex-1 overflow-hidden rounded-full bg-muted">
				<div className={cn("h-full rounded-full transition-all", barClass)} style={{ width: `${percent}%` }} />
			</div>
		</div>
	)
}

interface HostsTableProps {
	hosts: HostsOverviewRecord[]
	filters: HostsFilters
	onFiltersChange: (next: HostsFilters) => void
}

export const HostsTable = memo(function HostsTable({ hosts, filters, onFiltersChange }: HostsTableProps) {
	const { t } = useLingui()
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
			[h.name, h.hostname, h.primary_ip, h.os?.name, h.kernel, h.version].some((v) => v?.toLowerCase().includes(q))
		)
	}, [hosts, filters, search])

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
							<div className="text-xs text-muted-foreground">
								{[h.hostname && h.hostname !== h.name ? h.hostname : "", h.primary_ip].filter(Boolean).join(" · ") || "—"}
							</div>
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
					<MetricBar value={h.metrics?.disk_used_percent} tone={h.metrics && h.metrics.disk_used_percent >= 75 ? "amber" : "emerald"} />
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
						<div>{formatBytesPerSecond(h.metrics?.network_rx_bps)} ↓</div>
						<div className="text-muted-foreground">{formatBytesPerSecond(h.metrics?.network_tx_bps)} ↑</div>
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
		],
		[t]
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
				/>
				<div className="ml-auto flex shrink-0 items-center gap-2">
					<span className="text-xs text-muted-foreground">
						<Trans>Rows</Trans>
					</span>
					<Select value={String(pagination.pageSize)} onValueChange={(v) => setPagination({ pageIndex: 0, pageSize: Number(v) })}>
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

			{(search || filters.connection !== "all" || filters.compliance.size > 0 || filters.features.size > 0) && (
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
					<Button variant="outline" size="sm" onClick={() => table.previousPage()} disabled={!table.getCanPreviousPage()}>
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
