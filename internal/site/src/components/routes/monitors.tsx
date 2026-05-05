import { Trans, useLingui } from "@lingui/react/macro"
import { getPagePath } from "@nanostores/router"
import {
	ActivityIcon,
	CheckCircle2Icon,
	ClockIcon,
	CopyIcon,
	FolderIcon,
	MoreHorizontalIcon,
	PencilIcon,
	PlusIcon,
	ChevronDownIcon,
	ChevronRightIcon,
	Trash2Icon,
	XCircleIcon,
} from "lucide-react"
import { memo, useCallback, useEffect, useRef, useState } from "react"
import { PageHeader } from "@/components/page-header"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Dialog, DialogContent, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog"
import {
	AlertDialog,
	AlertDialogAction,
	AlertDialogCancel,
	AlertDialogContent,
	AlertDialogDescription,
	AlertDialogFooter,
	AlertDialogHeader,
	AlertDialogTitle,
} from "@/components/ui/alert-dialog"
import {
	DropdownMenu,
	DropdownMenuContent,
	DropdownMenuItem,
	DropdownMenuLabel,
	DropdownMenuSub,
	DropdownMenuSubContent,
	DropdownMenuSubTrigger,
	DropdownMenuSeparator,
	DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"
import { Switch } from "@/components/ui/switch"
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table"
import { toast } from "@/components/ui/use-toast"
import { isReadOnlyUser, pb } from "@/lib/api"
import { copyToClipboard } from "@/lib/utils"
import { $router, Link } from "@/components/router"
import {
	defaultMonitorForm,
	type MonitorFormData,
	type MonitorGroupRecord,
	type MonitorGroupResponse,
	type MonitorRecord,
	type MonitorType,
} from "@/lib/monitor-types"

// ─── Status helpers ──────────────────────────────────────────────────────────

function StatusBadge({ status, lastCheckedAt }: { status: number; lastCheckedAt: string }) {
	if (status === -1 || !lastCheckedAt) {
		return (
			<Badge variant="outline" className="text-muted-foreground text-xs">
				Pending
			</Badge>
		)
	}
	if (status === 1) {
		return (
			<Badge className="bg-green-500/15 text-green-700 dark:text-green-400 border-green-500/30 text-xs gap-1">
				<CheckCircle2Icon className="h-3 w-3" />
				<Trans>Up</Trans>
			</Badge>
		)
	}
	return (
		<Badge className="bg-red-500/15 text-red-700 dark:text-red-400 border-red-500/30 text-xs gap-1">
			<XCircleIcon className="h-3 w-3" />
			<Trans>Down</Trans>
		</Badge>
	)
}

function TypeBadge({ type }: { type: MonitorType }) {
	return (
		<Badge variant="outline" className="text-xs font-mono uppercase">
			{type}
		</Badge>
	)
}

function monitorTarget(m: MonitorRecord): string {
	switch (m.type) {
		case "http":
			return m.url || ""
		case "ping":
			return m.hostname || ""
		case "tcp":
			return m.hostname ? `${m.hostname}:${m.port}` : ""
		case "dns":
			return m.dns_host || ""
		case "push":
			return m.push_url || ""
		default:
			return ""
	}
}

function formatAge(ts: string): string {
	if (!ts) return "—"
	const diff = Date.now() - new Date(ts).getTime()
	const s = Math.floor(diff / 1000)
	if (s < 60) return `${s}s ago`
	if (s < 3600) return `${Math.floor(s / 60)}m ago`
	if (s < 86400) return `${Math.floor(s / 3600)}h ago`
	return `${Math.floor(s / 86400)}d ago`
}

function formatLatencyMs(ms?: number): string {
	if (ms == null) return "N/A"
	return `${Math.round(ms * 10) / 10}ms`
}

function formatPercent(value?: number): string {
	if (value == null) return "N/A"
	return `${Math.round(value * 10) / 10}%`
}

function checkBarClass(status: number): string {
	switch (status) {
		case 1:
			return "bg-green-500"
		case 0:
			return "bg-red-500"
		default:
			return "bg-muted-foreground/40"
	}
}

const monitorGroupStateKey = "vigil.monitors.open-groups"
const ungroupedGroupStateKey = "__ungrouped__"

function readOpenGroups(): Record<string, boolean> {
	if (typeof window === "undefined") return {}
	try {
		const raw = window.localStorage.getItem(monitorGroupStateKey)
		return raw ? JSON.parse(raw) : {}
	} catch {
		return {}
	}
}

function writeOpenGroups(value: Record<string, boolean>) {
	try {
		window.localStorage.setItem(monitorGroupStateKey, JSON.stringify(value))
	} catch {
		// ignore storage failures
	}
}

// ─── Monitor form ─────────────────────────────────────────────────────────────

interface MonitorDialogProps {
	open: boolean
	onClose: () => void
	onSaved: () => void
	monitor?: MonitorRecord | null
	groups: MonitorGroupRecord[]
	defaultGroupId?: string
}

function MonitorDialog({ open, onClose, onSaved, monitor, groups, defaultGroupId }: MonitorDialogProps) {
	const { t } = useLingui()
	const [saving, setSaving] = useState(false)
	const [form, setForm] = useState<MonitorFormData>(defaultMonitorForm)

	useEffect(() => {
		if (open) {
			if (monitor) {
				setForm({
					name: monitor.name,
					type: monitor.type,
					group: monitor.group || "",
					active: monitor.active,
					interval: monitor.interval || 60,
					timeout: monitor.timeout || 10,
					url: monitor.url || "",
					http_method: monitor.http_method || "GET",
					keyword: monitor.keyword || "",
					keyword_invert: monitor.keyword_invert || false,
					hostname: monitor.hostname || "",
					port: monitor.port || "",
					dns_host: monitor.dns_host || "",
					dns_type: monitor.dns_type || "A",
					dns_server: monitor.dns_server || "",
					failure_threshold: monitor.failure_threshold ?? 3,
					ping_count: monitor.ping_count ?? 1,
					ping_per_request_timeout: monitor.ping_per_request_timeout ?? 2,
					ping_ip_family: monitor.ping_ip_family || "",
				})
			} else {
				setForm({ ...defaultMonitorForm, group: defaultGroupId || "" })
			}
		}
	}, [open, monitor, defaultGroupId])

	const set = <K extends keyof MonitorFormData>(key: K, value: MonitorFormData[K]) =>
		setForm((f) => ({ ...f, [key]: value }))

	async function handleSave() {
		if (!form.name.trim()) {
			toast({ title: t`Name is required`, variant: "destructive" })
			return
		}
		setSaving(true)
		try {
			const payload: Record<string, unknown> = {
				name: form.name,
				type: form.type,
				group: form.group || "",
				active: form.active,
				interval: Number(form.interval) || 60,
				timeout: Number(form.timeout) || 10,
				failure_threshold: Number(form.failure_threshold) || 0,
			}
			switch (form.type) {
				case "http":
					payload.url = form.url
					payload.http_method = form.http_method
					payload.keyword = form.keyword
					payload.keyword_invert = form.keyword_invert
					break
				case "ping":
					payload.hostname = form.hostname
					payload.ping_count = Number(form.ping_count) || 1
					payload.ping_per_request_timeout = Number(form.ping_per_request_timeout) || 2
					payload.ping_ip_family = form.ping_ip_family || ""
					break
				case "tcp":
					payload.hostname = form.hostname
					payload.port = Number(form.port) || 0
					break
				case "dns":
					payload.dns_host = form.dns_host
					payload.dns_type = form.dns_type
					payload.dns_server = form.dns_server
					break
			}

			if (monitor) {
				await pb.send(`/api/app/monitors/${monitor.id}`, {
					method: "PUT",
					body: JSON.stringify(payload),
					headers: { "Content-Type": "application/json" },
				})
				toast({ title: t`Monitor updated` })
			} else {
				await pb.send("/api/app/monitors", {
					method: "POST",
					body: JSON.stringify(payload),
					headers: { "Content-Type": "application/json" },
				})
				toast({ title: t`Monitor created` })
			}
			onSaved()
			onClose()
		} catch {
			toast({ title: t`Failed to save monitor`, variant: "destructive" })
		} finally {
			setSaving(false)
		}
	}

	return (
		<Dialog open={open} onOpenChange={(o) => !o && onClose()}>
			<DialogContent className="max-w-lg max-h-[90vh] overflow-y-auto">
				<DialogHeader>
					<DialogTitle>{monitor ? <Trans>Edit monitor</Trans> : <Trans>Add monitor</Trans>}</DialogTitle>
				</DialogHeader>

				<div className="grid gap-4 py-2">
					{/* Name */}
					<div className="grid gap-1.5">
						<Label>
							<Trans>Name</Trans>
						</Label>
						<Input value={form.name} onChange={(e) => set("name", e.target.value)} placeholder="My service" />
					</div>

					{/* Type */}
					<div className="grid gap-1.5">
						<Label>
							<Trans>Type</Trans>
						</Label>
						<Select value={form.type} onValueChange={(v) => set("type", v as MonitorType)}>
							<SelectTrigger>
								<SelectValue />
							</SelectTrigger>
							<SelectContent>
								<SelectItem value="http">HTTP / HTTPS</SelectItem>
								<SelectItem value="ping">Ping</SelectItem>
								<SelectItem value="tcp">TCP</SelectItem>
								<SelectItem value="dns">DNS</SelectItem>
								<SelectItem value="push">Push</SelectItem>
							</SelectContent>
						</Select>
					</div>

					{/* Group */}
					<div className="grid gap-1.5">
						<Label>
							<Trans>Group</Trans>
						</Label>
						<Select value={form.group || "__none__"} onValueChange={(v) => set("group", v === "__none__" ? "" : v)}>
							<SelectTrigger>
								<SelectValue placeholder={t`No group`} />
							</SelectTrigger>
							<SelectContent>
								<SelectItem value="__none__">
									<Trans>No group</Trans>
								</SelectItem>
								{groups.map((g) => (
									<SelectItem key={g.id} value={g.id}>
										{g.name}
									</SelectItem>
								))}
							</SelectContent>
						</Select>
					</div>

					{/* Interval + Timeout */}
					<div className="grid grid-cols-2 gap-3">
						<div className="grid gap-1.5">
							<Label>
								<Trans>Interval (s)</Trans>
							</Label>
							<Input
								type="number"
								min={30}
								value={form.interval}
								onChange={(e) => set("interval", Number(e.target.value))}
							/>
						</div>
						<div className="grid gap-1.5">
							<Label>
								<Trans>Timeout (s)</Trans>
							</Label>
							<Input
								type="number"
								min={1}
								max={60}
								value={form.timeout}
								onChange={(e) => set("timeout", Number(e.target.value))}
							/>
						</div>
					</div>

					<div className="grid gap-1.5">
						<Label>
							<Trans>Failures before down</Trans>
						</Label>
						<Input
							type="number"
							min={0}
							value={form.failure_threshold}
							onChange={(e) => set("failure_threshold", Number(e.target.value))}
						/>
						<p className="text-xs text-muted-foreground">
							<Trans>0 means instant down. Default is 3.</Trans>
						</p>
					</div>

					{/* HTTP fields */}
					{form.type === "http" && (
						<>
							<div className="grid gap-1.5">
								<Label>
									<Trans>URL</Trans>
								</Label>
								<Input
									value={form.url}
									onChange={(e) => set("url", e.target.value)}
									placeholder="https://example.com"
								/>
							</div>
							<div className="grid gap-1.5">
								<Label>
									<Trans>Method</Trans>
								</Label>
								<Select value={form.http_method} onValueChange={(v) => set("http_method", v)}>
									<SelectTrigger>
										<SelectValue />
									</SelectTrigger>
									<SelectContent>
										<SelectItem value="GET">GET</SelectItem>
										<SelectItem value="HEAD">HEAD</SelectItem>
										<SelectItem value="POST">POST</SelectItem>
									</SelectContent>
								</Select>
							</div>
							<div className="grid gap-1.5">
								<Label>
									<Trans>Keyword in response body (optional)</Trans>
								</Label>
								<Input value={form.keyword} onChange={(e) => set("keyword", e.target.value)} placeholder="" />
							</div>
						</>
					)}

					{/* Ping fields */}
					{form.type === "ping" && (
						<>
							<div className="grid gap-1.5">
								<Label>
									<Trans>Hostname</Trans>
								</Label>
								<Input value={form.hostname} onChange={(e) => set("hostname", e.target.value)} placeholder="1.1.1.1" />
							</div>
							<div className="grid grid-cols-3 gap-3">
								<div className="grid gap-1.5">
									<Label>
										<Trans>Count</Trans>
									</Label>
									<Input
										type="number"
										min={1}
										value={form.ping_count}
										onChange={(e) => set("ping_count", Number(e.target.value))}
									/>
								</div>
								<div className="grid gap-1.5">
									<Label>
										<Trans>Per-request timeout</Trans>
									</Label>
									<Input
										type="number"
										min={1}
										value={form.ping_per_request_timeout}
										onChange={(e) => set("ping_per_request_timeout", Number(e.target.value))}
									/>
								</div>
								<div className="grid gap-1.5">
									<Label>
										<Trans>IP family</Trans>
									</Label>
									<Select
										value={form.ping_ip_family || "__auto__"}
										onValueChange={(v) => set("ping_ip_family", v === "__auto__" ? "" : (v as "ipv4" | "ipv6"))}
									>
										<SelectTrigger>
											<SelectValue placeholder={t`Auto`} />
										</SelectTrigger>
										<SelectContent>
											<SelectItem value="__auto__">
												<Trans>Auto</Trans>
											</SelectItem>
											<SelectItem value="ipv4">IPv4</SelectItem>
											<SelectItem value="ipv6">IPv6</SelectItem>
										</SelectContent>
									</Select>
								</div>
							</div>
						</>
					)}

					{/* TCP fields */}
					{form.type === "tcp" && (
						<div className="grid grid-cols-3 gap-3">
							<div className="col-span-2 grid gap-1.5">
								<Label>
									<Trans>Hostname</Trans>
								</Label>
								<Input
									value={form.hostname}
									onChange={(e) => set("hostname", e.target.value)}
									placeholder="db.example.com"
								/>
							</div>
							<div className="grid gap-1.5">
								<Label>
									<Trans>Port</Trans>
								</Label>
								<Input
									type="number"
									min={1}
									max={65535}
									value={form.port}
									onChange={(e) => set("port", e.target.value === "" ? "" : Number(e.target.value))}
									placeholder="5432"
								/>
							</div>
						</div>
					)}

					{/* DNS fields */}
					{form.type === "dns" && (
						<>
							<div className="grid gap-1.5">
								<Label>
									<Trans>Hostname to resolve</Trans>
								</Label>
								<Input
									value={form.dns_host}
									onChange={(e) => set("dns_host", e.target.value)}
									placeholder="example.com"
								/>
							</div>
							<div className="grid grid-cols-2 gap-3">
								<div className="grid gap-1.5">
									<Label>
										<Trans>Record type</Trans>
									</Label>
									<Select value={form.dns_type} onValueChange={(v) => set("dns_type", v)}>
										<SelectTrigger>
											<SelectValue />
										</SelectTrigger>
										<SelectContent>
											{["A", "AAAA", "CNAME", "MX", "NS", "TXT"].map((t) => (
												<SelectItem key={t} value={t}>
													{t}
												</SelectItem>
											))}
										</SelectContent>
									</Select>
								</div>
								<div className="grid gap-1.5">
									<Label>
										<Trans>DNS server (optional)</Trans>
									</Label>
									<Input
										value={form.dns_server}
										onChange={(e) => set("dns_server", e.target.value)}
										placeholder="1.1.1.1"
									/>
								</div>
							</div>
						</>
					)}

					{/* Push info */}
					{form.type === "push" && (
						<p className="text-sm text-muted-foreground">
							<Trans>
								A unique push URL will be generated automatically. Use it in a cron job or script to send heartbeats.
							</Trans>
						</p>
					)}

					{/* Active */}
					<div className="flex items-center gap-2">
						<Switch checked={form.active} onCheckedChange={(v) => set("active", v)} id="monitor-active" />
						<Label htmlFor="monitor-active">
							<Trans>Active</Trans>
						</Label>
					</div>
				</div>

				<DialogFooter>
					<Button variant="outline" onClick={onClose}>
						<Trans>Cancel</Trans>
					</Button>
					<Button onClick={handleSave} disabled={saving}>
						{saving ? <Trans>Saving…</Trans> : <Trans>Save</Trans>}
					</Button>
				</DialogFooter>
			</DialogContent>
		</Dialog>
	)
}

// ─── Group form ───────────────────────────────────────────────────────────────

interface GroupDialogProps {
	open: boolean
	onClose: () => void
	onSaved: () => void
	group?: MonitorGroupRecord | null
}

function GroupDialog({ open, onClose, onSaved, group }: GroupDialogProps) {
	const { t } = useLingui()
	const [saving, setSaving] = useState(false)
	const [name, setName] = useState("")

	useEffect(() => {
		if (open) setName(group?.name || "")
	}, [open, group])

	async function handleSave() {
		if (!name.trim()) {
			toast({ title: t`Name is required`, variant: "destructive" })
			return
		}
		setSaving(true)
		try {
			if (group) {
				await pb.send(`/api/app/monitor-groups/${group.id}`, {
					method: "PUT",
					body: JSON.stringify({ name }),
					headers: { "Content-Type": "application/json" },
				})
				toast({ title: t`Group updated` })
			} else {
				await pb.send("/api/app/monitor-groups", {
					method: "POST",
					body: JSON.stringify({ name, weight: 0 }),
					headers: { "Content-Type": "application/json" },
				})
				toast({ title: t`Group created` })
			}
			onSaved()
			onClose()
		} catch {
			toast({ title: t`Failed to save group`, variant: "destructive" })
		} finally {
			setSaving(false)
		}
	}

	return (
		<Dialog open={open} onOpenChange={(o) => !o && onClose()}>
			<DialogContent className="max-w-sm">
				<DialogHeader>
					<DialogTitle>{group ? <Trans>Edit group</Trans> : <Trans>Add group</Trans>}</DialogTitle>
				</DialogHeader>
				<div className="grid gap-1.5 py-2">
					<Label>
						<Trans>Name</Trans>
					</Label>
					<Input value={name} onChange={(e) => setName(e.target.value)} placeholder="Production" autoFocus />
				</div>
				<DialogFooter>
					<Button variant="outline" onClick={onClose}>
						<Trans>Cancel</Trans>
					</Button>
					<Button onClick={handleSave} disabled={saving}>
						{saving ? <Trans>Saving…</Trans> : <Trans>Save</Trans>}
					</Button>
				</DialogFooter>
			</DialogContent>
		</Dialog>
	)
}

// ─── Main page ────────────────────────────────────────────────────────────────

export default memo(function MonitorsPage() {
	const { t } = useLingui()
	const readonly = isReadOnlyUser()

	const [groups, setGroups] = useState<MonitorGroupResponse[]>([])
	const [groupList, setGroupList] = useState<MonitorGroupRecord[]>([])
	const [loading, setLoading] = useState(true)

	const debounceRef = useRef<ReturnType<typeof setTimeout> | null>(null)

	const [monitorDialog, setMonitorDialog] = useState(false)
	const [editMonitor, setEditMonitor] = useState<MonitorRecord | null>(null)
	const [monitorDefaultGroupId, setMonitorDefaultGroupId] = useState("")
	const [groupDialog, setGroupDialog] = useState(false)
	const [editGroup, setEditGroup] = useState<MonitorGroupRecord | null>(null)
	const [deleteGroupConfirm, setDeleteGroupConfirm] = useState<MonitorGroupResponse | null>(null)
	const [openGroups, setOpenGroups] = useState<Record<string, boolean>>(() => readOpenGroups())

	const fetchMonitors = useCallback(async () => {
		try {
			const data = await pb.send<MonitorGroupResponse[]>("/api/app/monitors", { method: "GET" })
			setGroups(data ?? [])
			const gl = await pb.send<MonitorGroupRecord[]>("/api/app/monitor-groups", { method: "GET" })
			setGroupList(gl ?? [])
		} catch {
			// silently ignore
		} finally {
			setLoading(false)
		}
	}, [])

	useEffect(() => {
		document.title = `${t`Monitors`} / Vigil`
		fetchMonitors()
	}, [t, fetchMonitors])

	// Realtime updates — debounced to avoid a cascade of fetches when
	// the scheduler updates status every few seconds
	useEffect(() => {
		let unsub: (() => void) | undefined
		;(async () => {
			unsub = await pb.collection("monitors").subscribe("*", () => {
				if (debounceRef.current) clearTimeout(debounceRef.current)
				debounceRef.current = setTimeout(fetchMonitors, 1000)
			})
		})()
		return () => {
			unsub?.()
			if (debounceRef.current) clearTimeout(debounceRef.current)
		}
	}, [fetchMonitors])

	async function deleteMonitor(id: string) {
		try {
			await pb.send(`/api/app/monitors/${id}`, { method: "DELETE" })
			toast({ title: t`Monitor deleted` })
			fetchMonitors()
		} catch {
			toast({ title: t`Failed to delete monitor`, variant: "destructive" })
		}
	}

	async function deleteGroup(id: string) {
		try {
			await pb.send(`/api/app/monitor-groups/${id}`, { method: "DELETE" })
			toast({ title: t`Group deleted` })
			fetchMonitors()
		} catch {
			toast({ title: t`Failed to delete group`, variant: "destructive" })
		}
	}

	function requestDeleteGroup(group: MonitorGroupResponse) {
		if (group.monitors.length > 0) {
			setDeleteGroupConfirm(group)
			return
		}
		deleteGroup(group.id)
	}

	async function moveMonitorToGroup(monitorId: string, group: string) {
		try {
			await pb.send(`/api/app/monitors/${monitorId}/move`, {
				method: "POST",
				body: JSON.stringify({ group } satisfies MonitorMovePayload),
				headers: { "Content-Type": "application/json" },
			})
			toast({ title: t`Monitor moved` })
			fetchMonitors()
		} catch {
			toast({ title: t`Failed to move monitor`, variant: "destructive" })
		}
	}

	const allMonitors = groups.flatMap((g) => g.monitors)
	const upCount = allMonitors.filter((m) => m.last_checked_at && m.status === 1).length
	const downCount = allMonitors.filter((m) => m.last_checked_at && m.status !== 1).length
	const orderedGroups = [...groups.filter((group) => !group.id), ...groups.filter((group) => group.id)]

	function toggleGroup(id: string) {
		setOpenGroups((current) => {
			const next = {
				...current,
				[id]: !current[id],
			}
			writeOpenGroups(next)
			return next
		})
	}

	function setAllGroupsOpen(open: boolean) {
		const next = Object.fromEntries(orderedGroups.map((group) => [group.id || ungroupedGroupStateKey, open]))
		setOpenGroups(next)
		writeOpenGroups(next)
	}

	return (
		<div className="pb-14">
			<PageHeader
				className="mb-6"
				icon={ActivityIcon}
				title={<Trans>Monitors</Trans>}
				meta={
					!loading && allMonitors.length > 0 ? (
						<span className="flex flex-wrap items-center gap-2">
							<span className="font-medium text-green-600 dark:text-green-400">
								{upCount} <Trans>up</Trans>
							</span>
							<span>·</span>
							{downCount > 0 && (
								<>
									<span className="font-medium text-red-600 dark:text-red-400">
										{downCount} <Trans>down</Trans>
									</span>
									<span>·</span>
								</>
							)}
							<span>
								{allMonitors.length} <Trans>total</Trans>
							</span>
						</span>
					) : undefined
				}
				actions={
					!readonly ? (
						<>
							<Button
								variant="outline"
								size="sm"
								onClick={() => setAllGroupsOpen(true)}
								disabled={orderedGroups.length === 0}
							>
								<ChevronDownIcon className="h-4 w-4 me-1.5" />
								<Trans>Expand all</Trans>
							</Button>
							<Button
								variant="outline"
								size="sm"
								onClick={() => setAllGroupsOpen(false)}
								disabled={orderedGroups.length === 0}
							>
								<ChevronRightIcon className="h-4 w-4 me-1.5" />
								<Trans>Collapse all</Trans>
							</Button>
							<Button
								variant="outline"
								size="sm"
								onClick={() => {
									setEditGroup(null)
									setGroupDialog(true)
								}}
							>
								<FolderIcon className="h-4 w-4 me-1.5" />
								<Trans>Add group</Trans>
							</Button>
							<Button
								size="sm"
								onClick={() => {
									setEditMonitor(null)
									setMonitorDefaultGroupId("")
									setMonitorDialog(true)
								}}
							>
								<PlusIcon className="h-4 w-4 me-1.5" />
								<Trans>Add monitor</Trans>
							</Button>
						</>
					) : undefined
				}
			/>

			{/* Empty state */}
			{!loading && allMonitors.length === 0 && (
				<div className="flex flex-col items-center justify-center py-20 text-center text-muted-foreground">
					<ActivityIcon className="h-10 w-10 mb-4 opacity-30" />
					<p className="text-lg font-medium mb-1">
						<Trans>No monitors yet</Trans>
					</p>
					{!readonly && (
						<p className="text-sm">
							<Trans>Click "Add monitor" to start monitoring your services.</Trans>
						</p>
					)}
				</div>
			)}

			{/* Groups + monitors */}
			<div className="flex flex-col gap-6">
				{orderedGroups.map((group) => (
					<MonitorGroupSection
						key={group.id || ungroupedGroupStateKey}
						group={group}
						readonly={readonly}
						open={openGroups[group.id || ungroupedGroupStateKey] ?? !group.id}
						onToggle={() => toggleGroup(group.id || ungroupedGroupStateKey)}
						onEditMonitor={(m) => {
							setEditMonitor(m)
							setMonitorDefaultGroupId("")
							setMonitorDialog(true)
						}}
						onAddMonitor={() => {
							setEditMonitor(null)
							setMonitorDefaultGroupId(group.id)
							setMonitorDialog(true)
						}}
						onDeleteMonitor={deleteMonitor}
						onEditGroup={() => {
							if (group.id) {
								setEditGroup({ id: group.id, name: group.name, weight: group.weight })
								setGroupDialog(true)
							}
						}}
						onDeleteGroup={() => requestDeleteGroup(group)}
						onMoveMonitor={moveMonitorToGroup}
						availableGroups={groupList}
					/>
				))}
			</div>

			{/* Dialogs */}
			<MonitorDialog
				open={monitorDialog}
				onClose={() => {
					setMonitorDialog(false)
					setMonitorDefaultGroupId("")
				}}
				onSaved={fetchMonitors}
				monitor={editMonitor}
				groups={groupList}
				defaultGroupId={monitorDefaultGroupId}
			/>
			<GroupDialog open={groupDialog} onClose={() => setGroupDialog(false)} onSaved={fetchMonitors} group={editGroup} />
			<AlertDialog open={deleteGroupConfirm != null} onOpenChange={(open) => !open && setDeleteGroupConfirm(null)}>
				<AlertDialogContent>
					<AlertDialogHeader>
						<AlertDialogTitle>
							<Trans>Delete group?</Trans>
						</AlertDialogTitle>
						<AlertDialogDescription>
							{deleteGroupConfirm && (
								<Trans>
									This group contains {deleteGroupConfirm.monitors.length} monitor(s). They will be moved to No group.
								</Trans>
							)}
						</AlertDialogDescription>
					</AlertDialogHeader>
					<AlertDialogFooter>
						<AlertDialogCancel onClick={() => setDeleteGroupConfirm(null)}>
							<Trans>Cancel</Trans>
						</AlertDialogCancel>
						<AlertDialogAction
							onClick={() => {
								if (!deleteGroupConfirm) return
								const group = deleteGroupConfirm
								setDeleteGroupConfirm(null)
								deleteGroup(group.id)
							}}
						>
							<Trans>Delete</Trans>
						</AlertDialogAction>
					</AlertDialogFooter>
				</AlertDialogContent>
			</AlertDialog>
		</div>
	)
})

// ─── Group section ────────────────────────────────────────────────────────────

interface MonitorGroupSectionProps {
	group: MonitorGroupResponse
	availableGroups: MonitorGroupRecord[]
	readonly: boolean
	open: boolean
	onToggle: () => void
	onEditMonitor: (m: MonitorRecord) => void
	onAddMonitor: () => void
	onDeleteMonitor: (id: string) => void
	onEditGroup: () => void
	onDeleteGroup: () => void
	onMoveMonitor: (monitorId: string, groupId: string) => void
}

function MonitorGroupSection({
	group,
	availableGroups,
	readonly,
	open,
	onToggle,
	onEditMonitor,
	onAddMonitor,
	onDeleteMonitor,
	onEditGroup,
	onDeleteGroup,
	onMoveMonitor,
}: MonitorGroupSectionProps) {
	if (group.monitors.length === 0 && !group.id) return null

	const isUngrouped = !group.id
	const title = isUngrouped ? <Trans>No group</Trans> : group.name

	return (
		<div className="rounded-md border border-border/60 overflow-hidden bg-card">
			{/* Group header */}
			<div className="grid grid-cols-[auto_minmax(0,1fr)_auto] items-center gap-3 border-b border-border/60 bg-muted/20 px-4 py-3">
				<Button
					variant="ghost"
					size="sm"
					className="h-8 justify-start px-2 text-sm font-medium text-muted-foreground hover:text-foreground"
					onClick={onToggle}
				>
					{open ? <ChevronDownIcon className="h-4 w-4 me-1" /> : <ChevronRightIcon className="h-4 w-4 me-1" />}
					<FolderIcon className="h-4 w-4 me-2" />
					{title}
				</Button>
				<div className="flex items-center gap-2 justify-self-start text-xs whitespace-nowrap text-muted-foreground">
					<span className="text-green-600 dark:text-green-400 font-medium">
						{group.monitors.filter((m) => m.last_checked_at && m.status === 1).length} <Trans>up</Trans>
					</span>
					<span>·</span>
					<span>
						{group.monitors.length} <Trans>total</Trans>
					</span>
				</div>
				<div className="justify-self-end">
					{group.id && !readonly && (
						<DropdownMenu>
							<DropdownMenuTrigger asChild>
								<Button variant="ghost" size="icon" className="h-7 w-7 shrink-0">
									<MoreHorizontalIcon className="h-4 w-4" />
								</Button>
							</DropdownMenuTrigger>
							<DropdownMenuContent align="end">
								<DropdownMenuItem onClick={onAddMonitor}>
									<PlusIcon className="h-4 w-4 me-2" />
									<Trans>Add monitor here</Trans>
								</DropdownMenuItem>
								<DropdownMenuSeparator />
								<DropdownMenuItem onClick={onEditGroup}>
									<PencilIcon className="h-4 w-4 me-2" />
									<Trans>Edit group</Trans>
								</DropdownMenuItem>
								<DropdownMenuSeparator />
								<DropdownMenuItem className="text-destructive focus:text-destructive" onClick={onDeleteGroup}>
									<Trash2Icon className="h-4 w-4 me-2" />
									<Trans>Delete group</Trans>
								</DropdownMenuItem>
							</DropdownMenuContent>
						</DropdownMenu>
					)}
				</div>
			</div>

			{open ? (
				<div>
					<Table>
						<TableHeader>
							<TableRow className="bg-muted/30">
								<TableHead className="w-20">
									<Trans>Status</Trans>
								</TableHead>
								<TableHead>
									<Trans>Name</Trans>
								</TableHead>
								<TableHead className="hidden sm:table-cell w-16">
									<Trans>Type</Trans>
								</TableHead>
								<TableHead className="hidden md:table-cell">
									<Trans>Target</Trans>
								</TableHead>
								<TableHead className="hidden lg:table-cell w-28 text-right">
									<Trans>Latency</Trans>
								</TableHead>
								<TableHead className="hidden xl:table-cell w-28 text-right">
									<Trans>Avg 24h</Trans>
								</TableHead>
								<TableHead className="hidden xl:table-cell w-28 text-right">
									<Trans>Uptime 24h</Trans>
								</TableHead>
								<TableHead className="hidden xl:table-cell w-28 text-right">
									<Trans>Uptime 30d</Trans>
								</TableHead>
								<TableHead className="hidden lg:table-cell w-28 text-right">
									<Trans>Last check</Trans>
								</TableHead>
								{!readonly && <TableHead className="w-10" />}
							</TableRow>
						</TableHeader>
						<TableBody>
							{group.monitors.length === 0 ? (
								<TableRow>
									<TableCell colSpan={readonly ? 9 : 10} className="text-center text-muted-foreground text-sm py-6">
										<Trans>No monitors in this group.</Trans>
									</TableCell>
								</TableRow>
							) : (
								group.monitors.map((m) => (
									<MonitorRow
										key={m.id}
										monitor={m}
										availableGroups={availableGroups}
										readonly={readonly}
										onMoveMonitor={onMoveMonitor}
										onEdit={() => onEditMonitor(m)}
										onDelete={() => onDeleteMonitor(m.id)}
									/>
								))
							)}
						</TableBody>
					</Table>
				</div>
			) : null}
		</div>
	)
}

// ─── Monitor row ──────────────────────────────────────────────────────────────

interface MonitorRowProps {
	monitor: MonitorRecord
	availableGroups: MonitorGroupRecord[]
	readonly: boolean
	onMoveMonitor: (monitorId: string, groupId: string) => void
	onEdit: () => void
	onDelete: () => void
}

function MonitorRow({ monitor: m, availableGroups, readonly, onMoveMonitor, onEdit, onDelete }: MonitorRowProps) {
	const target = monitorTarget(m)
	const canVisitTarget = m.type === "http" && /^https?:\/\//i.test(target)
	const currentGroupId = m.group || ungroupedGroupStateKey

	return (
		<TableRow>
			<TableCell>
				<StatusBadge status={m.status} lastCheckedAt={m.last_checked_at} />
			</TableCell>
			<TableCell>
				<Link href={getPagePath($router, "monitor", { id: m.id })} className="font-medium text-sm hover:underline">
					{m.name}
				</Link>
				{m.last_msg && <div className="text-xs text-muted-foreground truncate max-w-xs mt-0.5">{m.last_msg}</div>}
			</TableCell>
			<TableCell className="hidden sm:table-cell">
				<TypeBadge type={m.type} />
			</TableCell>
			<TableCell className="hidden md:table-cell">
				{m.type === "push" && m.push_url ? (
					<div className="flex items-center gap-1.5 max-w-xs">
						<span className="text-xs text-muted-foreground font-mono truncate">{m.push_url}</span>
						<Button
							variant="ghost"
							size="icon"
							className="h-5 w-5 shrink-0"
							onClick={() => copyToClipboard(m.push_url)}
						>
							<CopyIcon className="h-3 w-3" />
						</Button>
					</div>
				) : canVisitTarget ? (
					<a
						href={target}
						target="_blank"
						rel="noreferrer"
						className="text-sm text-muted-foreground font-mono truncate max-w-xs block underline-offset-4 hover:text-foreground hover:underline"
					>
						{target}
					</a>
				) : (
					<span className="text-sm text-muted-foreground font-mono truncate max-w-xs block">{target}</span>
				)}
			</TableCell>
			<TableCell className="hidden lg:table-cell text-right text-sm text-muted-foreground">
				{m.last_checked_at && m.last_latency_ms > 0 ? `${m.last_latency_ms}ms` : "—"}
			</TableCell>
			<TableCell className="hidden xl:table-cell text-right text-sm text-muted-foreground">
				{formatLatencyMs(m.avg_latency_24h_ms)}
			</TableCell>
			<TableCell className="hidden xl:table-cell text-right text-sm text-muted-foreground">
				{formatPercent(m.uptime_24h)}
			</TableCell>
			<TableCell className="hidden xl:table-cell text-right text-sm text-muted-foreground">
				{formatPercent(m.uptime_30d)}
			</TableCell>
			<TableCell className="hidden lg:table-cell text-right">
				<div className="flex flex-col items-end gap-1">
					<div className="flex items-center justify-end gap-0.5">
						{(m.recent_checks ?? []).length > 0 ? (
							(m.recent_checks ?? []).map((check, index) => (
								<span
									key={`${check.checked_at}-${index}`}
									title={`${check.status === 1 ? "Up" : check.status === 0 ? "Down" : "Pending"}`}
									className={`h-3 w-1.5 rounded-full ${checkBarClass(check.status)}`}
								/>
							))
						) : (
							<span className="text-xs text-muted-foreground">—</span>
						)}
					</div>
					<span className="text-xs text-muted-foreground flex items-center justify-end gap-1">
						<ClockIcon className="h-3 w-3" />
						{formatAge(m.last_checked_at)}
					</span>
				</div>
			</TableCell>
			{!readonly && (
				<TableCell>
					<DropdownMenu>
						<DropdownMenuTrigger asChild>
							<Button variant="ghost" size="icon" className="h-7 w-7">
								<MoreHorizontalIcon className="h-4 w-4" />
							</Button>
						</DropdownMenuTrigger>
						<DropdownMenuContent align="end">
							<DropdownMenuSub>
								<DropdownMenuSubTrigger>
									<Trans>Move to</Trans>
								</DropdownMenuSubTrigger>
								<DropdownMenuSubContent>
									<DropdownMenuLabel>
										<Trans>Destination</Trans>
									</DropdownMenuLabel>
									<DropdownMenuSeparator />
									<DropdownMenuItem
										disabled={currentGroupId === ungroupedGroupStateKey}
										onClick={() => onMoveMonitor(m.id, "")}
									>
										<Trans>No group</Trans>
									</DropdownMenuItem>
									{availableGroups.map((group) => (
										<DropdownMenuItem
											key={group.id}
											disabled={currentGroupId === group.id}
											onClick={() => onMoveMonitor(m.id, group.id)}
										>
											{group.name}
										</DropdownMenuItem>
									))}
								</DropdownMenuSubContent>
							</DropdownMenuSub>
							<DropdownMenuSeparator />
							<DropdownMenuItem onClick={onEdit}>
								<PencilIcon className="h-4 w-4 me-2" />
								<Trans>Edit</Trans>
							</DropdownMenuItem>
							<DropdownMenuSeparator />
							<DropdownMenuItem className="text-destructive focus:text-destructive" onClick={onDelete}>
								<Trash2Icon className="h-4 w-4 me-2" />
								<Trans>Delete</Trans>
							</DropdownMenuItem>
						</DropdownMenuContent>
					</DropdownMenu>
				</TableCell>
			)}
		</TableRow>
	)
}
