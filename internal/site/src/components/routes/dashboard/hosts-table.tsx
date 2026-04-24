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
import { memo, useMemo, useState } from "react"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table"
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip"
import { cn } from "@/lib/utils"
import type { DashboardHost } from "@/lib/dashboard-types"

// ── helpers ──────────────────────────────────────────────────────────────────

function formatUptime(s: number): string {
	if (!s || s <= 0) return "—"
	const d = Math.floor(s / 86400)
	const h = Math.floor((s % 86400) / 3600)
	const m = Math.floor((s % 3600) / 60)
	if (d > 0) return `${d}d ${h}h`
	if (h > 0) return `${h}h ${m}m`
	return `${m}m`
}

function formatRam(mb: number): string {
	if (!mb || mb <= 0) return "—"
	return mb >= 1024 ? `${Math.round(mb / 1024)} GB` : `${Math.round(mb)} MB`
}

function formatAgeDays(days: number, known: boolean): string {
	if (!known) return "—"
	if (!days || days <= 0) return "today"
	return `${days}d ago`
}

function patchStatusRank(h: DashboardHost): number {
	if (h.status !== "connected") return -1
	if (h.reboot?.required) return 4
	if ((h.packages?.security_count ?? 0) > 0) return 3
	if ((h.packages?.outdated_count ?? 0) > 0 && (h.packages?.last_upgrade_age_days ?? 0) > 30) return 2
	if ((h.packages?.outdated_count ?? 0) > 0 && !h.packages?.last_upgrade_known) return 1
	return 0
}

// ── sub-components ────────────────────────────────────────────────────────────

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

function SortBtn({ column, children }: { column: Column<DashboardHost, unknown>; children: React.ReactNode }) {
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

interface HostsTableProps {
	hosts: DashboardHost[]
	activeFilter: string | null
	onFilterChange: (filter: string | null) => void
}

// ── main component ────────────────────────────────────────────────────────────

export const HostsTable = memo(function HostsTable({ hosts, activeFilter, onFilterChange }: HostsTableProps) {
	const { t } = useLingui()

	const chips = useMemo(
		() => [
			{ key: "all", label: t`All` },
			{ key: "connected", label: t`Online` },
			{ key: "offline", label: t`Offline` },
			{ key: "docker", label: t`Docker` },
			{ key: "reboot", label: t`Reboot req.` },
			{ key: "security", label: t`Security` },
			{ key: "stale", label: t`Out of SLA` },
			{ key: "unknown", label: t`Unknown` },
			{ key: "clean", label: t`Compliant` },
		],
		[t]
	)

	const [sorting, setSorting] = useState<SortingState>([{ id: "connection", desc: true }])
	const [pagination, setPagination] = useState<PaginationState>({ pageIndex: 0, pageSize: 10 })
	const [search, setSearch] = useState("")

	const activeChip = activeFilter || "all"

	const filteredHosts = useMemo(() => {
		let result = hosts
		switch (activeChip) {
			case "connected":
				result = hosts.filter((h) => h.status === "connected")
				break
			case "offline":
				result = hosts.filter((h) => h.status !== "connected")
				break
			case "security":
				result = hosts.filter((h) => (h.packages?.security_count ?? 0) > 0)
				break
			case "stale":
				result = hosts.filter(
					(h) => (h.packages?.outdated_count ?? 0) > 0 && (h.packages?.last_upgrade_age_days ?? 0) > 30
				)
				break
			case "unknown":
				result = hosts.filter((h) => (h.packages?.outdated_count ?? 0) > 0 && !h.packages?.last_upgrade_known)
				break
			case "reboot":
				result = hosts.filter((h) => h.reboot?.required)
				break
			case "docker":
				result = hosts.filter((h) => h.docker?.state === "available")
				break
			case "clean":
				result = hosts.filter(
					(h) =>
						!h.reboot?.required &&
						!(h.packages?.security_count ?? 0) &&
						!((h.packages?.outdated_count ?? 0) > 0 && (h.packages?.last_upgrade_age_days ?? 0) > 30) &&
						!((h.packages?.outdated_count ?? 0) > 0 && !h.packages?.last_upgrade_known)
				)
				break
		}
		if (!search) return result
		const q = search.toLowerCase()
		return result.filter((h) =>
			[h.name, h.hostname, h.primary_ip, h.network?.gateway, h.os?.name, h.kernel].some((v) =>
				v?.toLowerCase().includes(q)
			)
		)
	}, [hosts, activeChip, search])

	function handleFilterChange(key: string) {
		setPagination((p) => ({ ...p, pageIndex: 0 }))
		onFilterChange(key === "all" ? null : key)
	}

	function handleSearch(value: string) {
		setSearch(value)
		setPagination((p) => ({ ...p, pageIndex: 0 }))
	}

	const columns: ColumnDef<DashboardHost>[] = useMemo(
		() => [
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
							<div className="font-semibold">{h.name || h.hostname || h.id}</div>
							{h.hostname && h.name !== h.hostname && <div className="text-xs text-muted-foreground">{h.hostname}</div>}
						</div>
						<InfoBtn rows={[{ label: t`Hostname`, value: h.hostname || "—" }]} />
					</div>
				),
			},
			{
				id: "ip",
				accessorFn: (h) => h.primary_ip || "",
				header: ({ column }) => (
					<SortBtn column={column}>
						<Trans>Network</Trans>
					</SortBtn>
				),
				cell: ({ row: { original: h } }) => (
					<div className="group flex items-center">
						<span className="font-mono text-sm">{h.primary_ip || "—"}</span>
						<InfoBtn
							rows={[
								{ label: t`Gateway`, value: h.network?.gateway || "—" },
								{ label: t`DNS`, value: h.network?.dns_servers?.join(", ") || "—" },
							]}
						/>
					</div>
				),
			},
			{
				id: "os",
				accessorFn: (h) => h.os?.name || "",
				header: ({ column }) => (
					<SortBtn column={column}>
						<Trans>Platform</Trans>
					</SortBtn>
				),
				cell: ({ row: { original: h } }) => (
					<div className="text-sm">
						<div>{h.os ? `${h.os.name} ${h.os.version}`.trim() : "—"}</div>
						{h.kernel && <div className="font-mono text-[11px] text-muted-foreground">{h.kernel}</div>}
					</div>
				),
			},
			{
				id: "resources",
				accessorFn: (h) => h.resources?.ram_mb ?? 0,
				header: ({ column }) => (
					<SortBtn column={column}>
						<Trans>Resources</Trans>
					</SortBtn>
				),
				cell: ({ row: { original: h } }) => {
					const res = h.resources
					if (!res) return <span className="text-sm text-muted-foreground">—</span>
					return (
						<div className="group flex items-center">
							<span className="tabular-nums text-sm">
								{res.cpu_cores} vCPU / {formatRam(res.ram_mb)}
							</span>
							<InfoBtn
								rows={[
									{ label: t`CPU`, value: res.cpu_model || "—" },
									{ label: t`RAM`, value: formatRam(res.ram_mb) },
									{ label: t`Swap`, value: formatRam(res.swap_mb) },
									{ label: t`Arch`, value: h.architecture || "—" },
								]}
							/>
						</div>
					)
				},
			},
			{
				id: "packages",
				accessorFn: (h) => (h.packages?.security_count ?? 0) * 10000 + (h.packages?.outdated_count ?? 0),
				header: ({ column }) => (
					<SortBtn column={column}>
						<Trans>Packages</Trans>
					</SortBtn>
				),
				cell: ({ row: { original: h } }) => {
					if (h.status !== "connected") return <span className="text-xs text-muted-foreground">—</span>
					const pkg = h.packages
					if (!pkg) return <span className="text-xs text-muted-foreground">—</span>
					const sec = pkg.security_count ?? 0
					const upd = pkg.outdated_count ?? 0
					const pillCls =
						sec > 0
							? "border-red-500/30 bg-red-500/10 text-red-400"
							: upd > 0
								? "border-amber-500/30 bg-amber-500/10 text-amber-400"
								: "border-emerald-500/30 bg-emerald-500/10 text-emerald-400"
					return (
						<div className="group flex items-center">
							<Badge variant="outline" className={cn("tabular-nums text-[10px]", pillCls)}>
								{sec} sec / {upd} upd
							</Badge>
							<InfoBtn
								rows={[
									{ label: t`Security`, value: sec },
									{ label: t`Outdated`, value: upd },
									{
										label: t`Last upgrade`,
										value: pkg.last_upgrade_known
											? formatAgeDays(pkg.last_upgrade_age_days, pkg.last_upgrade_known)
											: t`unknown`,
									},
									{ label: t`Repos`, value: h.repositories?.length ?? 0 },
								]}
							/>
						</div>
					)
				},
			},
			{
				id: "docker",
				accessorFn: (h) => h.docker?.container_count ?? -1,
				header: ({ column }) => (
					<SortBtn column={column}>
						<Trans>Docker</Trans>
					</SortBtn>
				),
				cell: ({ row: { original: h } }) => {
					const docker = h.docker
					if (!docker || docker.state === "not_configured" || docker.state === "cli_missing")
						return <span className="text-xs text-muted-foreground">—</span>
					if (
						docker.state === "daemon_unreachable" ||
						docker.state === "permission_denied" ||
						docker.state === "error"
					) {
						return (
							<div className="group flex items-center">
								<Badge variant="outline" className="border-red-500/30 bg-red-500/10 text-[10px] text-red-400">
									{t`Error`}
								</Badge>
								<InfoBtn rows={[{ label: t`State`, value: docker.state.replace(/_/g, " ") }]} />
							</div>
						)
					}
					const allRunning = docker.running_count === docker.container_count && docker.container_count > 0
					const someDown = docker.running_count < docker.container_count
					const pillCls = allRunning
						? "border-emerald-500/30 bg-emerald-500/10 text-emerald-400"
						: someDown
							? "border-amber-500/30 bg-amber-500/10 text-amber-400"
							: "border-border/50 text-muted-foreground"
					return (
						<div className="group flex items-center">
							<Badge variant="outline" className={cn("tabular-nums text-[10px]", pillCls)}>
								{docker.running_count}/{docker.container_count}
							</Badge>
							<InfoBtn
								rows={[
									{ label: t`State`, value: docker.state.replace(/_/g, " ") },
									{ label: t`Running`, value: docker.running_count },
									{ label: t`Total`, value: docker.container_count },
								]}
							/>
						</div>
					)
				},
			},
			{
				id: "patch_status",
				accessorFn: patchStatusRank,
				header: ({ column }) => (
					<SortBtn column={column}>
						<Trans>Status</Trans>
					</SortBtn>
				),
				cell: ({ row: { original: h } }) => {
					if (h.status !== "connected")
						return (
							<Badge variant="outline" className="border-border/50 text-[10px] text-muted-foreground">
								<Trans>No data</Trans>
							</Badge>
						)
					if (h.reboot?.required)
						return (
							<Badge variant="outline" className="border-red-500/30 bg-red-500/10 text-[10px] text-red-400">
								<Trans>Reboot req.</Trans>
							</Badge>
						)
					if ((h.packages?.security_count ?? 0) > 0)
						return (
							<Badge variant="outline" className="border-red-500/30 bg-red-500/10 text-[10px] text-red-400">
								<Trans>Security upd.</Trans>
							</Badge>
						)
					if ((h.packages?.outdated_count ?? 0) > 0 && (h.packages?.last_upgrade_age_days ?? 0) > 30)
						return (
							<Badge variant="outline" className="border-amber-500/30 bg-amber-500/10 text-[10px] text-amber-400">
								<Trans>Out of SLA</Trans>
							</Badge>
						)
					if ((h.packages?.outdated_count ?? 0) > 0 && !h.packages?.last_upgrade_known)
						return (
							<Badge variant="outline" className="border-slate-500/30 bg-slate-500/10 text-[10px] text-slate-400">
								<Trans>Unknown</Trans>
							</Badge>
						)
					return (
						<Badge variant="outline" className="border-emerald-500/30 bg-emerald-500/10 text-[10px] text-emerald-400">
							<Trans>Compliant</Trans>
						</Badge>
					)
				},
			},
			{
				id: "connection",
				accessorFn: (h) => (h.status === "connected" ? 1 : 0),
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
				id: "uptime",
				accessorFn: (h) => h.uptime_seconds ?? 0,
				header: ({ column }) => (
					<SortBtn column={column}>
						<Trans>Uptime</Trans>
					</SortBtn>
				),
				cell: ({ row: { original: h } }) => (
					<span className="tabular-nums text-sm text-muted-foreground">
						{h.uptime_seconds ? formatUptime(h.uptime_seconds) : "—"}
					</span>
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
				<div className="flex flex-wrap gap-1.5">
					{chips.map((chip) => (
						<button
							key={chip.key}
							type="button"
							onClick={() => handleFilterChange(chip.key)}
							className={cn(
								"rounded-full border px-3 py-1 text-xs font-semibold transition-colors",
								activeChip === chip.key
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
								<TableCell colSpan={columns.length} className="h-20 text-center text-sm text-muted-foreground">
									<Trans>No hosts match the current filter.</Trans>
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
					{filteredHosts.length} host{filteredHosts.length !== 1 ? "s" : ""}
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
