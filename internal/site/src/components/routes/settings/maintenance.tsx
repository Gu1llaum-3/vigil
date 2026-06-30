import { Trans, useLingui } from "@lingui/react/macro"
import { redirectPage } from "@nanostores/router"
import { Loader2Icon, PencilIcon, PlusIcon, Trash2Icon, WrenchIcon } from "lucide-react"
import { type FormEvent, memo, useEffect, useMemo, useState } from "react"
import { $router } from "@/components/router"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Checkbox } from "@/components/ui/checkbox"
import {
	Dialog,
	DialogContent,
	DialogDescription,
	DialogFooter,
	DialogHeader,
	DialogTitle,
} from "@/components/ui/dialog"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"
import { Separator } from "@/components/ui/separator"
import { Switch } from "@/components/ui/switch"
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table"
import { Textarea } from "@/components/ui/textarea"
import { toast } from "@/components/ui/use-toast"
import { apiDelete, apiGet, apiPost, apiPut, isAdmin, pb } from "@/lib/api"

type Severity = "info" | "warning" | "critical"
type Strategy = "single" | "recurring"

interface MaintenanceScope {
	monitor_ids?: string[]
	agent_ids?: string[]
}

interface MaintenanceWindow {
	id: string
	title: string
	description: string
	enabled: boolean
	severity: Severity
	strategy: Strategy
	start_at: string
	end_at: string
	start_time: string
	end_time: string
	weekdays: number[]
	active_from: string
	active_to: string
	timezone: string
	scope: MaintenanceScope
}

interface NamedRef {
	id: string
	name: string
}

const browserTimezone = (): string => {
	try {
		return Intl.DateTimeFormat().resolvedOptions().timeZone || "UTC"
	} catch {
		return "UTC"
	}
}

// RFC3339 ↔ <input type="datetime-local"> (local wall time).
function isoToLocalInput(iso: string): string {
	if (!iso) return ""
	const d = new Date(iso)
	if (Number.isNaN(d.getTime())) return ""
	const pad = (n: number) => String(n).padStart(2, "0")
	return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}T${pad(d.getHours())}:${pad(d.getMinutes())}`
}
function localInputToIso(local: string): string {
	if (!local) return ""
	const d = new Date(local)
	return Number.isNaN(d.getTime()) ? "" : d.toISOString()
}

const MaintenancePage = memo(() => {
	const { t } = useLingui()
	const admin = isAdmin()
	const [windows, setWindows] = useState<MaintenanceWindow[]>([])
	const [hosts, setHosts] = useState<NamedRef[]>([])
	const [monitors, setMonitors] = useState<NamedRef[]>([])
	const [loading, setLoading] = useState(true)
	const [editing, setEditing] = useState<MaintenanceWindow | null>(null)
	const [creating, setCreating] = useState(false)

	useEffect(() => {
		if (!admin) {
			redirectPage($router, "settings", { name: "general" })
			return
		}
		Promise.all([
			apiGet<MaintenanceWindow[]>("/api/app/maintenance-windows"),
			pb.collection("agents").getFullList<NamedRef>({ fields: "id,name" }),
			pb
				.send<Array<{ monitors: NamedRef[] }>>("/api/app/monitors", { method: "GET" })
				.then((groups) => (groups ?? []).flatMap((g) => g.monitors ?? []))
				.catch(() => [] as NamedRef[]),
		])
			.then(([w, agents, mons]) => {
				setWindows(w ?? [])
				setHosts(agents ?? [])
				setMonitors(mons ?? [])
			})
			.catch((error: unknown) => {
				toast({
					title: t`Failed to load maintenance windows`,
					description: (error as Error).message,
					variant: "destructive",
				})
			})
			.finally(() => setLoading(false))
	}, [admin, t])

	if (!admin) return null

	const handleSaved = (saved: MaintenanceWindow) => {
		setWindows((current) => {
			const idx = current.findIndex((w) => w.id === saved.id)
			if (idx >= 0) {
				const next = [...current]
				next[idx] = saved
				return next
			}
			return [...current, saved]
		})
	}

	const handleDelete = async (w: MaintenanceWindow) => {
		if (!window.confirm(t`Delete maintenance window "${w.title}"?`)) return
		try {
			await apiDelete(`/api/app/maintenance-windows/${w.id}`)
			setWindows((current) => current.filter((x) => x.id !== w.id))
			toast({ title: t`Maintenance window deleted` })
		} catch (error: unknown) {
			toast({
				title: t`Failed to delete maintenance window`,
				description: (error as Error).message,
				variant: "destructive",
			})
		}
	}

	return (
		<>
			<div className="flex flex-col gap-3 sm:flex-row sm:items-start sm:justify-between">
				<div>
					<h3 className="text-xl font-medium mb-1">
						<Trans>Maintenance windows</Trans>
					</h3>
					<p className="text-sm text-muted-foreground leading-relaxed max-w-prose">
						<Trans>
							During an active window, notifications for the covered resources are suppressed (bell and external
							channels) and a banner is shown to every signed-in user. Use a one-time window for a planned operation, or
							a recurring window for routine maintenance (e.g. nightly).
						</Trans>
					</p>
				</div>
				<Button onClick={() => setCreating(true)} className="shrink-0">
					<PlusIcon className="mr-2 size-4" />
					<Trans>Add window</Trans>
				</Button>
			</div>
			<Separator className="my-4" />
			{loading ? (
				<div className="flex items-center gap-2 text-sm text-muted-foreground">
					<Loader2Icon className="size-4 animate-spin" />
					<Trans>Loading…</Trans>
				</div>
			) : windows.length === 0 ? (
				<p className="text-sm text-muted-foreground">
					<Trans>No maintenance windows yet.</Trans>
				</p>
			) : (
				<Table>
					<TableHeader>
						<TableRow>
							<TableHead>
								<Trans>Title</Trans>
							</TableHead>
							<TableHead>
								<Trans>Schedule</Trans>
							</TableHead>
							<TableHead>
								<Trans>Scope</Trans>
							</TableHead>
							<TableHead>
								<Trans>Status</Trans>
							</TableHead>
							<TableHead className="text-right">
								<Trans>Actions</Trans>
							</TableHead>
						</TableRow>
					</TableHeader>
					<TableBody>
						{windows.map((w) => (
							<TableRow key={w.id}>
								<TableCell className="font-medium">
									<div className="flex items-center gap-2">
										<WrenchIcon className="size-3.5 text-muted-foreground" />
										{w.title}
									</div>
								</TableCell>
								<TableCell className="text-xs text-muted-foreground">{scheduleSummary(w)}</TableCell>
								<TableCell className="text-xs text-muted-foreground">{scopeSummary(w, t)}</TableCell>
								<TableCell>
									{w.enabled ? (
										<Badge variant="outline" className="border-emerald-500/40 text-emerald-500 text-[10px]">
											<Trans>Enabled</Trans>
										</Badge>
									) : (
										<Badge variant="outline" className="text-muted-foreground text-[10px]">
											<Trans>Disabled</Trans>
										</Badge>
									)}
								</TableCell>
								<TableCell className="text-right">
									<Button variant="ghost" size="icon" onClick={() => setEditing(w)}>
										<PencilIcon className="size-4" />
									</Button>
									<Button
										variant="ghost"
										size="icon"
										onClick={() => handleDelete(w)}
										className="text-destructive hover:text-destructive"
									>
										<Trash2Icon className="size-4" />
									</Button>
								</TableCell>
							</TableRow>
						))}
					</TableBody>
				</Table>
			)}
			<MaintenanceDialog
				open={creating || !!editing}
				window={editing}
				hosts={hosts}
				monitors={monitors}
				onClose={() => {
					setCreating(false)
					setEditing(null)
				}}
				onSaved={handleSaved}
			/>
		</>
	)
})

function scheduleSummary(w: MaintenanceWindow): string {
	if (w.strategy === "single") {
		const start = w.start_at ? new Date(w.start_at).toLocaleString() : "?"
		const end = w.end_at ? new Date(w.end_at).toLocaleString() : "?"
		return `${start} → ${end}`
	}
	const days = w.weekdays && w.weekdays.length > 0 ? w.weekdays.length : 7
	return `${w.start_time}–${w.end_time} ${w.timezone} (${days === 7 ? "daily" : `${days}×/wk`})`
}

function scopeSummary(w: MaintenanceWindow, t: (s: TemplateStringsArray) => string): string {
	const agents = w.scope?.agent_ids?.length ?? 0
	const mons = w.scope?.monitor_ids?.length ?? 0
	if (agents === 0 && mons === 0) return t`Global`
	const parts: string[] = []
	if (agents > 0) parts.push(`${agents} host(s)`)
	if (mons > 0) parts.push(`${mons} monitor(s)`)
	return parts.join(", ")
}

const WEEKDAY_ORDER = [1, 2, 3, 4, 5, 6, 0]

function MaintenanceDialog({
	open,
	window: win,
	hosts,
	monitors,
	onClose,
	onSaved,
}: {
	open: boolean
	window: MaintenanceWindow | null
	hosts: NamedRef[]
	monitors: NamedRef[]
	onClose: () => void
	onSaved: (saved: MaintenanceWindow) => void
}) {
	const { t } = useLingui()
	const [title, setTitle] = useState("")
	const [description, setDescription] = useState("")
	const [enabled, setEnabled] = useState(true)
	const [severity, setSeverity] = useState<Severity>("info")
	const [strategy, setStrategy] = useState<Strategy>("single")
	const [startAt, setStartAt] = useState("")
	const [endAt, setEndAt] = useState("")
	const [startTime, setStartTime] = useState("02:00")
	const [endTime, setEndTime] = useState("04:00")
	const [weekdays, setWeekdays] = useState<number[]>([])
	const [activeFrom, setActiveFrom] = useState("")
	const [activeTo, setActiveTo] = useState("")
	const [timezone, setTimezone] = useState(browserTimezone())
	const [scoped, setScoped] = useState(false)
	const [agentIds, setAgentIds] = useState<string[]>([])
	const [monitorIds, setMonitorIds] = useState<string[]>([])
	const [saving, setSaving] = useState(false)

	const weekdayLabels = useMemo<Record<number, string>>(
		() => ({
			1: t`Mon`,
			2: t`Tue`,
			3: t`Wed`,
			4: t`Thu`,
			5: t`Fri`,
			6: t`Sat`,
			0: t`Sun`,
		}),
		[t]
	)

	useEffect(() => {
		if (!open) return
		setTitle(win?.title ?? "")
		setDescription(win?.description ?? "")
		setEnabled(win?.enabled ?? true)
		setSeverity(win?.severity ?? "info")
		setStrategy(win?.strategy ?? "single")
		setStartAt(isoToLocalInput(win?.start_at ?? ""))
		setEndAt(isoToLocalInput(win?.end_at ?? ""))
		setStartTime(win?.start_time || "02:00")
		setEndTime(win?.end_time || "04:00")
		setWeekdays(win?.weekdays ?? [])
		setActiveFrom(win?.active_from ? win.active_from.slice(0, 10) : "")
		setActiveTo(win?.active_to ? win.active_to.slice(0, 10) : "")
		setTimezone(win?.timezone || browserTimezone())
		const a = win?.scope?.agent_ids ?? []
		const m = win?.scope?.monitor_ids ?? []
		setAgentIds(a)
		setMonitorIds(m)
		setScoped(a.length > 0 || m.length > 0)
	}, [open, win])

	const toggleWeekday = (d: number) =>
		setWeekdays((cur) => (cur.includes(d) ? cur.filter((x) => x !== d) : [...cur, d]))
	const toggleId = (list: string[], setList: (v: string[]) => void, id: string) =>
		setList(list.includes(id) ? list.filter((x) => x !== id) : [...list, id])

	const handleSubmit = async (e: FormEvent) => {
		e.preventDefault()
		if (!title.trim()) {
			toast({ title: t`Title is required`, variant: "destructive" })
			return
		}
		// An empty scope means "global" on the backend, so a scoped window with nothing
		// selected would silently suppress the whole fleet — block it here.
		if (scoped && agentIds.length === 0 && monitorIds.length === 0) {
			toast({ title: t`Select at least one host or monitor, or switch to global`, variant: "destructive" })
			return
		}
		const scope: MaintenanceScope = scoped ? { agent_ids: agentIds, monitor_ids: monitorIds } : {}
		const body: Record<string, unknown> = {
			title: title.trim(),
			description: description.trim(),
			enabled,
			severity,
			strategy,
			scope,
		}
		if (strategy === "single") {
			body.start_at = localInputToIso(startAt)
			body.end_at = localInputToIso(endAt)
		} else {
			body.start_time = startTime
			body.end_time = endTime
			body.weekdays = weekdays
			body.active_from = activeFrom
			body.active_to = activeTo
			body.timezone = timezone
		}
		setSaving(true)
		try {
			const saved = win
				? await apiPut<MaintenanceWindow>(`/api/app/maintenance-windows/${win.id}`, body)
				: await apiPost<MaintenanceWindow>("/api/app/maintenance-windows", body)
			onSaved(saved)
			toast({ title: win ? t`Maintenance window updated` : t`Maintenance window created` })
			onClose()
		} catch (error: unknown) {
			toast({
				title: t`Failed to save maintenance window`,
				description: (error as Error).message,
				variant: "destructive",
			})
		} finally {
			setSaving(false)
		}
	}

	return (
		<Dialog open={open} onOpenChange={(next) => !next && onClose()}>
			<DialogContent className="w-[92%] sm:max-w-[36rem] max-h-[90vh] overflow-y-auto rounded-lg">
				<DialogHeader>
					<DialogTitle>
						{win ? <Trans>Edit maintenance window</Trans> : <Trans>Add maintenance window</Trans>}
					</DialogTitle>
					<DialogDescription>
						<Trans>Notifications are suppressed while this window is active.</Trans>
					</DialogDescription>
				</DialogHeader>
				<form onSubmit={handleSubmit} className="space-y-4">
					<div className="space-y-2">
						<Label htmlFor="mw-title">
							<Trans>Title</Trans>
						</Label>
						<Input id="mw-title" value={title} onChange={(e) => setTitle(e.target.value)} autoFocus />
					</div>
					<div className="space-y-2">
						<Label htmlFor="mw-desc">
							<Trans>Description</Trans>
						</Label>
						<Textarea id="mw-desc" rows={2} value={description} onChange={(e) => setDescription(e.target.value)} />
					</div>

					<div className="grid grid-cols-2 gap-3">
						<div className="space-y-2">
							<Label>
								<Trans>Type</Trans>
							</Label>
							<Select value={strategy} onValueChange={(v) => setStrategy(v as Strategy)}>
								<SelectTrigger>
									<SelectValue />
								</SelectTrigger>
								<SelectContent>
									<SelectItem value="single">
										<Trans>One-time</Trans>
									</SelectItem>
									<SelectItem value="recurring">
										<Trans>Recurring</Trans>
									</SelectItem>
								</SelectContent>
							</Select>
						</div>
						<div className="space-y-2">
							<Label>
								<Trans>Severity</Trans>
							</Label>
							<Select value={severity} onValueChange={(v) => setSeverity(v as Severity)}>
								<SelectTrigger>
									<SelectValue />
								</SelectTrigger>
								<SelectContent>
									<SelectItem value="info">
										<Trans>Info</Trans>
									</SelectItem>
									<SelectItem value="warning">
										<Trans>Warning</Trans>
									</SelectItem>
									<SelectItem value="critical">
										<Trans>Critical</Trans>
									</SelectItem>
								</SelectContent>
							</Select>
						</div>
					</div>

					{strategy === "single" ? (
						<div className="grid grid-cols-2 gap-3">
							<div className="space-y-2">
								<Label htmlFor="mw-start">
									<Trans>Start</Trans>
								</Label>
								<Input
									id="mw-start"
									type="datetime-local"
									value={startAt}
									onChange={(e) => setStartAt(e.target.value)}
								/>
							</div>
							<div className="space-y-2">
								<Label htmlFor="mw-end">
									<Trans>End</Trans>
								</Label>
								<Input id="mw-end" type="datetime-local" value={endAt} onChange={(e) => setEndAt(e.target.value)} />
							</div>
						</div>
					) : (
						<>
							<div className="grid grid-cols-3 gap-3">
								<div className="space-y-2">
									<Label htmlFor="mw-stime">
										<Trans>Start time</Trans>
									</Label>
									<Input id="mw-stime" type="time" value={startTime} onChange={(e) => setStartTime(e.target.value)} />
								</div>
								<div className="space-y-2">
									<Label htmlFor="mw-etime">
										<Trans>End time</Trans>
									</Label>
									<Input id="mw-etime" type="time" value={endTime} onChange={(e) => setEndTime(e.target.value)} />
								</div>
								<div className="space-y-2">
									<Label htmlFor="mw-tz">
										<Trans>Timezone</Trans>
									</Label>
									<Input id="mw-tz" value={timezone} onChange={(e) => setTimezone(e.target.value)} placeholder="UTC" />
								</div>
							</div>
							<div className="space-y-2">
								<Label>
									<Trans>Days (none = every day)</Trans>
								</Label>
								<div className="flex flex-wrap gap-1.5">
									{WEEKDAY_ORDER.map((d) => (
										<Button
											key={d}
											type="button"
											size="sm"
											variant={weekdays.includes(d) ? "default" : "outline"}
											className="h-8 w-11 px-0 text-xs"
											onClick={() => toggleWeekday(d)}
										>
											{weekdayLabels[d]}
										</Button>
									))}
								</div>
							</div>
						</>
					)}

					<div className="flex items-center gap-2">
						<Switch id="mw-enabled" checked={enabled} onCheckedChange={setEnabled} />
						<Label htmlFor="mw-enabled">
							<Trans>Enabled</Trans>
						</Label>
					</div>

					<Separator />
					<div className="space-y-3">
						<div className="flex items-center gap-2">
							<Switch id="mw-scoped" checked={scoped} onCheckedChange={setScoped} />
							<Label htmlFor="mw-scoped">
								<Trans>Limit to specific hosts / monitors (otherwise global)</Trans>
							</Label>
						</div>
						{scoped && (
							<div className="grid gap-3 sm:grid-cols-2">
								<ScopeList
									title={t`Hosts`}
									idPrefix="mw-host"
									items={hosts}
									selected={agentIds}
									onToggle={(id) => toggleId(agentIds, setAgentIds, id)}
								/>
								<ScopeList
									title={t`Monitors`}
									idPrefix="mw-monitor"
									items={monitors}
									selected={monitorIds}
									onToggle={(id) => toggleId(monitorIds, setMonitorIds, id)}
								/>
							</div>
						)}
					</div>

					<DialogFooter>
						<Button type="button" variant="outline" onClick={onClose} disabled={saving}>
							<Trans>Cancel</Trans>
						</Button>
						<Button type="submit" disabled={saving || !title.trim()}>
							{saving ? <Loader2Icon className="mr-2 size-4 animate-spin" /> : null}
							<Trans>Save</Trans>
						</Button>
					</DialogFooter>
				</form>
			</DialogContent>
		</Dialog>
	)
}

function ScopeList({
	title,
	idPrefix,
	items,
	selected,
	onToggle,
}: {
	title: string
	idPrefix: string
	items: NamedRef[]
	selected: string[]
	onToggle: (id: string) => void
}) {
	return (
		<div className="rounded-md border border-border/60">
			<div className="border-b border-border/60 px-2.5 py-1.5 text-xs font-medium text-muted-foreground">{title}</div>
			<div className="max-h-40 overflow-y-auto p-1.5">
				{items.length === 0 ? (
					<p className="px-1 py-2 text-xs text-muted-foreground">
						<Trans>None available</Trans>
					</p>
				) : (
					items.map((it) => {
						const fieldId = `${idPrefix}-${it.id}`
						return (
							<label
								key={it.id}
								htmlFor={fieldId}
								className="flex cursor-pointer items-center gap-2 rounded px-1.5 py-1 text-sm hover:bg-accent/50"
							>
								<Checkbox id={fieldId} checked={selected.includes(it.id)} onCheckedChange={() => onToggle(it.id)} />
								<span className="truncate">{it.name || it.id}</span>
							</label>
						)
					})
				)}
			</div>
		</div>
	)
}

export default MaintenancePage
