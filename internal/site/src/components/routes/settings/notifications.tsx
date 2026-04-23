import { Trans, useLingui } from "@lingui/react/macro"
import { redirectPage } from "@nanostores/router"
import { FlaskConicalIcon, Loader2Icon, MoreHorizontalIcon, PencilIcon, PlusIcon, Trash2Icon } from "lucide-react"
import { memo, useCallback, useEffect, useState } from "react"
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
import {
	DropdownMenu,
	DropdownMenuContent,
	DropdownMenuItem,
	DropdownMenuSeparator,
	DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"
import { Separator } from "@/components/ui/separator"
import { Switch } from "@/components/ui/switch"
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs"
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table"
import { Textarea } from "@/components/ui/textarea"
import { toast } from "@/components/ui/use-toast"
import { isAdmin, pb } from "@/lib/api"
import type { NotificationChannel, NotificationKind, NotificationRule } from "@/types"
import NotificationHistory from "./notifications/history.tsx"

const ALL_KINDS: NotificationKind[] = ["email", "webhook", "slack", "teams", "gchat", "ntfy", "gotify", "in-app"]
const ALL_EVENTS = ["monitor.down", "monitor.up", "agent.offline", "agent.online", "container_image.update_available"]
// --- API helpers ---

function apiGet<T>(path: string): Promise<T> {
	return pb.send(path, { method: "GET" }) as Promise<T>
}

function apiPost<T>(path: string, body: unknown): Promise<T> {
	return pb.send(path, {
		method: "POST",
		body: JSON.stringify(body),
		headers: { "Content-Type": "application/json" },
	}) as Promise<T>
}

function apiPatch<T>(path: string, body: unknown): Promise<T> {
	return pb.send(path, {
		method: "PATCH",
		body: JSON.stringify(body),
		headers: { "Content-Type": "application/json" },
	}) as Promise<T>
}

async function apiDelete(path: string): Promise<void> {
	await pb.send(path, { method: "DELETE" })
}

// --- Channel config form per kind ---

function EmailConfigFields({
	config,
	onChange,
}: {
	config: Record<string, string>
	onChange: (k: string, v: string) => void
}) {
	const { t } = useLingui()
	return (
		<>
			<div className="space-y-1">
				<Label>{t`To (comma-separated)`}</Label>
				<Input
					value={config.to ?? ""}
					onChange={(e) => onChange("to", e.target.value)}
					placeholder="user@example.com"
				/>
			</div>
			<div className="space-y-1">
				<Label>{t`CC`}</Label>
				<Input value={config.cc ?? ""} onChange={(e) => onChange("cc", e.target.value)} placeholder={t`Optional`} />
			</div>
			<div className="space-y-1">
				<Label>{t`BCC`}</Label>
				<Input value={config.bcc ?? ""} onChange={(e) => onChange("bcc", e.target.value)} placeholder={t`Optional`} />
			</div>
		</>
	)
}

function UrlConfigField({
	config,
	onChange,
	label = "Webhook URL",
	sensitiveNote,
}: {
	config: Record<string, string>
	onChange: (k: string, v: string) => void
	label?: string
	sensitiveNote?: boolean
}) {
	return (
		<div className="space-y-1">
			<Label>{label}</Label>
			<Input
				value={config.url ?? ""}
				onChange={(e) => onChange("url", e.target.value)}
				placeholder="https://"
				type="url"
			/>
			{sensitiveNote && config.url === "**REDACTED**" && (
				<p className="text-xs text-muted-foreground">
					<Trans>Leave as-is to keep the existing value.</Trans>
				</p>
			)}
		</div>
	)
}

function SlackConfigFields({
	config,
	onChange,
}: {
	config: Record<string, string>
	onChange: (k: string, v: string) => void
}) {
	const { t } = useLingui()
	return (
		<>
			<UrlConfigField config={config} onChange={onChange} label="Webhook URL" sensitiveNote />
			<div className="space-y-1">
				<Label>{t`Channel (optional)`}</Label>
				<Input
					value={config.channel ?? ""}
					onChange={(e) => onChange("channel", e.target.value)}
					placeholder="#alerts"
				/>
			</div>
			<div className="space-y-1">
				<Label>{t`Username (optional)`}</Label>
				<Input
					value={config.username ?? ""}
					onChange={(e) => onChange("username", e.target.value)}
					placeholder="Vigil"
				/>
			</div>
		</>
	)
}

function NtfyConfigFields({
	config,
	onChange,
}: {
	config: Record<string, string>
	onChange: (k: string, v: string) => void
}) {
	const { t } = useLingui()
	return (
		<>
			<UrlConfigField config={config} onChange={onChange} label={t`Topic URL`} />
			<div className="space-y-1">
				<Label>{t`Token (optional)`}</Label>
				<Input
					value={config.token ?? ""}
					onChange={(e) => onChange("token", e.target.value)}
					placeholder={t`Bearer token for protected topics`}
					type="password"
					autoComplete="off"
				/>
			</div>
			<div className="space-y-1">
				<Label>{t`Priority (1–5, default 3)`}</Label>
				<Input
					value={config.priority ?? ""}
					onChange={(e) => onChange("priority", e.target.value)}
					placeholder="3"
					type="number"
					min={1}
					max={5}
				/>
			</div>
		</>
	)
}

function GotifyConfigFields({
	config,
	onChange,
}: {
	config: Record<string, string>
	onChange: (k: string, v: string) => void
}) {
	const { t } = useLingui()
	return (
		<>
			<UrlConfigField config={config} onChange={onChange} label={t`Server URL`} />
			<div className="space-y-1">
				<Label>{t`App Token`}</Label>
				<Input
					value={config.token ?? ""}
					onChange={(e) => onChange("token", e.target.value)}
					placeholder="xxxxxxxxxxxxxxxx"
					type="password"
					autoComplete="off"
				/>
			</div>
			<div className="space-y-1">
				<Label>{t`Priority (default 5)`}</Label>
				<Input
					value={config.priority ?? ""}
					onChange={(e) => onChange("priority", e.target.value)}
					placeholder="5"
					type="number"
					min={0}
				/>
			</div>
		</>
	)
}

function WebhookConfigFields({
	config,
	onChange,
}: {
	config: Record<string, string>
	onChange: (k: string, v: string) => void
}) {
	const { t } = useLingui()
	return (
		<>
			<UrlConfigField config={config} onChange={onChange} label="URL" sensitiveNote />
			<div className="space-y-1">
				<Label>{t`Method (default POST)`}</Label>
				<Input value={config.method ?? ""} onChange={(e) => onChange("method", e.target.value)} placeholder="POST" />
			</div>
			<div className="space-y-1">
				<Label>{t`Headers (JSON object, optional)`}</Label>
				<Textarea
					value={
						typeof config.headers === "string"
							? config.headers
							: config.headers
								? JSON.stringify(config.headers, null, 2)
								: ""
					}
					onChange={(e) => onChange("headers", e.target.value)}
					placeholder={'{"Authorization": "Bearer ..."}'}
					rows={3}
					className="font-mono text-xs"
				/>
			</div>
		</>
	)
}

function ConfigFields({
	kind,
	config,
	onChange,
}: {
	kind: NotificationKind
	config: Record<string, string>
	onChange: (k: string, v: string) => void
}) {
	switch (kind) {
		case "email":
			return <EmailConfigFields config={config} onChange={onChange} />
		case "webhook":
			return <WebhookConfigFields config={config} onChange={onChange} />
		case "slack":
			return <SlackConfigFields config={config} onChange={onChange} />
		case "teams":
		case "gchat":
			return <UrlConfigField config={config} onChange={onChange} label="Webhook URL" sensitiveNote />
		case "ntfy":
			return <NtfyConfigFields config={config} onChange={onChange} />
		case "gotify":
			return <GotifyConfigFields config={config} onChange={onChange} />
		case "in-app":
			return (
				<p className="rounded-md border border-dashed px-3 py-2 text-sm text-muted-foreground">
					<Trans>
						No external configuration is required. Matching events will appear as in-app toasts for the rule owner.
					</Trans>
				</p>
			)
	}
}

// --- Channel Dialog ---

type ChannelDialogState = {
	open: boolean
	editing: NotificationChannel | null
}

function buildConfigPayload(kind: NotificationKind, config: Record<string, string>): Record<string, unknown> {
	if (kind === "in-app") {
		return {}
	}
	if (kind === "webhook") {
		const result: Record<string, unknown> = {}
		if (config.url) result.url = config.url
		if (config.method) result.method = config.method
		if (config.headers) {
			try {
				result.headers = JSON.parse(config.headers)
			} catch {
				result.headers = config.headers
			}
		}
		return result
	}
	// For numeric fields
	const numericKeys = ["priority"]
	const result: Record<string, unknown> = {}
	for (const [k, v] of Object.entries(config)) {
		if (v === "") continue
		if (numericKeys.includes(k)) {
			const n = Number(v)
			result[k] = Number.isNaN(n) ? v : n
		} else {
			result[k] = v
		}
	}
	return result
}

const ChannelDialog = memo(
	({
		state,
		onClose,
		onSaved,
	}: {
		state: ChannelDialogState
		onClose: () => void
		onSaved: (ch: NotificationChannel) => void
	}) => {
		const { t } = useLingui()
		const isEdit = !!state.editing
		const [name, setName] = useState("")
		const [kind, setKind] = useState<NotificationKind>("webhook")
		const [enabled, setEnabled] = useState(true)
		const [config, setConfig] = useState<Record<string, string>>({})
		const [saving, setSaving] = useState(false)

		useEffect(() => {
			if (state.open) {
				if (state.editing) {
					setName(state.editing.name)
					setKind(state.editing.kind)
					setEnabled(state.editing.enabled)
					// Flatten config for form fields (stringify non-string values)
					const flat: Record<string, string> = {}
					for (const [k, v] of Object.entries(state.editing.config ?? {})) {
						flat[k] = typeof v === "object" ? JSON.stringify(v, null, 2) : String(v ?? "")
					}
					setConfig(flat)
				} else {
					setName("")
					setKind("webhook")
					setEnabled(true)
					setConfig({})
				}
				setSaving(false)
			}
		}, [state.open, state.editing])

		const handleConfigChange = useCallback((k: string, v: string) => {
			setConfig((prev) => ({ ...prev, [k]: v }))
		}, [])

		async function handleSubmit() {
			if (!name.trim()) return
			setSaving(true)
			try {
				const body = { name: name.trim(), kind, enabled, config: buildConfigPayload(kind, config) }
				let saved: NotificationChannel
				if (isEdit && state.editing) {
					saved = await apiPatch<NotificationChannel>(`/api/app/notifications/channels/${state.editing.id}`, body)
				} else {
					saved = await apiPost<NotificationChannel>("/api/app/notifications/channels", body)
				}
				onSaved(saved)
				onClose()
			} catch (e: unknown) {
				toast({ title: t`Error`, description: (e as Error).message, variant: "destructive" })
			} finally {
				setSaving(false)
			}
		}

		return (
			<Dialog open={state.open} onOpenChange={(open) => !open && onClose()}>
				<DialogContent className="sm:max-w-md">
					<DialogHeader>
						<DialogTitle>{isEdit ? <Trans>Edit channel</Trans> : <Trans>Add channel</Trans>}</DialogTitle>
						<DialogDescription>
							<Trans>Configure a notification delivery destination.</Trans>
						</DialogDescription>
					</DialogHeader>
					<div className="space-y-4 py-2">
						<div className="space-y-1">
							<Label>{t`Name`}</Label>
							<Input value={name} onChange={(e) => setName(e.target.value)} placeholder={t`My Slack alerts`} />
						</div>
						{!isEdit && (
							<div className="space-y-1">
								<Label>{t`Kind`}</Label>
								<Select
									value={kind}
									onValueChange={(v) => {
										setKind(v as NotificationKind)
										setConfig({})
									}}
								>
									<SelectTrigger>
										<SelectValue />
									</SelectTrigger>
									<SelectContent>
										{ALL_KINDS.map((k) => (
											<SelectItem key={k} value={k}>
												{k}
											</SelectItem>
										))}
									</SelectContent>
								</Select>
							</div>
						)}
						<div className="flex items-center gap-3">
							<Switch checked={enabled} onCheckedChange={setEnabled} />
							<Label>{t`Enabled`}</Label>
						</div>
						<Separator />
						<ConfigFields kind={kind} config={config} onChange={handleConfigChange} />
					</div>
					<DialogFooter>
						<Button variant="outline" onClick={onClose}>
							<Trans>Cancel</Trans>
						</Button>
						<Button onClick={handleSubmit} disabled={saving || !name.trim()}>
							{saving && <Loader2Icon className="me-2 size-4 animate-spin" />}
							{isEdit ? <Trans>Save</Trans> : <Trans>Add</Trans>}
						</Button>
					</DialogFooter>
				</DialogContent>
			</Dialog>
		)
	}
)

// --- Rule Dialog ---

type RuleDialogState = {
	open: boolean
	editing: NotificationRule | null
}

const RuleDialog = memo(
	({
		state,
		channels,
		onClose,
		onSaved,
	}: {
		state: RuleDialogState
		channels: NotificationChannel[]
		onClose: () => void
		onSaved: (r: NotificationRule) => void
	}) => {
		const { t } = useLingui()
		const isEdit = !!state.editing
		const [name, setName] = useState("")
		const [enabled, setEnabled] = useState(true)
		const [events, setEvents] = useState<string[]>([])
		const [selectedChannels, setSelectedChannels] = useState<string[]>([])
		const [throttle, setThrottle] = useState("0")
		const [saving, setSaving] = useState(false)

		useEffect(() => {
			if (state.open) {
				if (state.editing) {
					setName(state.editing.name)
					setEnabled(state.editing.enabled)
					setEvents(state.editing.events ?? [])
					setSelectedChannels(state.editing.channels ?? [])
					setThrottle(String(state.editing.throttle_seconds ?? 0))
				} else {
					setName("")
					setEnabled(true)
					setEvents(["monitor.down", "agent.offline"])
					setSelectedChannels([])
					setThrottle("0")
				}
				setSaving(false)
			}
		}, [state.open, state.editing])

		function toggleEvent(ev: string) {
			setEvents((prev) => (prev.includes(ev) ? prev.filter((e) => e !== ev) : [...prev, ev]))
		}

		function toggleChannel(id: string) {
			setSelectedChannels((prev) => (prev.includes(id) ? prev.filter((c) => c !== id) : [...prev, id]))
		}

		async function handleSubmit() {
			if (!name.trim()) return
			setSaving(true)
			try {
				const body = {
					name: name.trim(),
					enabled,
					events,
					channels: selectedChannels,
					min_severity: "info",
					throttle_seconds: Number(throttle) || 0,
				}
				let saved: NotificationRule
				if (isEdit && state.editing) {
					saved = await apiPatch<NotificationRule>(`/api/app/notifications/rules/${state.editing.id}`, body)
				} else {
					saved = await apiPost<NotificationRule>("/api/app/notifications/rules", body)
				}
				onSaved(saved)
				onClose()
			} catch (e: unknown) {
				toast({ title: t`Error`, description: (e as Error).message, variant: "destructive" })
			} finally {
				setSaving(false)
			}
		}

		return (
			<Dialog open={state.open} onOpenChange={(open) => !open && onClose()}>
				<DialogContent className="sm:max-w-md">
					<DialogHeader>
						<DialogTitle>{isEdit ? <Trans>Edit rule</Trans> : <Trans>Add rule</Trans>}</DialogTitle>
						<DialogDescription>
							<Trans>Define when and where notifications are sent.</Trans>
						</DialogDescription>
					</DialogHeader>
					<div className="space-y-4 py-2 max-h-[60vh] overflow-y-auto pr-1">
						<div className="space-y-1">
							<Label>{t`Name`}</Label>
							<Input value={name} onChange={(e) => setName(e.target.value)} placeholder={t`Critical alerts`} />
						</div>
						<div className="flex items-center gap-3">
							<Switch checked={enabled} onCheckedChange={setEnabled} />
							<Label>{t`Enabled`}</Label>
						</div>
						<Separator />
						<div className="space-y-2">
							<Label>{t`Events`}</Label>
							<div className="grid grid-cols-2 gap-2">
								{ALL_EVENTS.map((ev) => (
									<label key={ev} className="flex items-center gap-2 cursor-pointer">
										<input
											type="checkbox"
											checked={events.includes(ev)}
											onChange={() => toggleEvent(ev)}
											className="accent-primary"
										/>
										<span className="text-sm font-mono">{ev}</span>
									</label>
								))}
							</div>
						</div>
						<Separator />
						<div className="space-y-2">
							<Label>{t`Channels`}</Label>
							<p className="text-xs text-muted-foreground">
								<Trans>You can select multiple channels for the same rule.</Trans>
							</p>
							{channels.length === 0 ? (
								<p className="text-sm text-muted-foreground">
									<Trans>No channels configured yet.</Trans>
								</p>
							) : (
								<div className="space-y-1.5 max-h-36 overflow-y-auto border rounded-md p-2">
									{channels.map((ch) => (
										<div key={ch.id} className="flex items-center gap-2 rounded-sm px-1 py-1.5">
											<Checkbox
												id={`rule-channel-${ch.id}`}
												checked={selectedChannels.includes(ch.id)}
												onCheckedChange={() => toggleChannel(ch.id)}
											/>
											<Label htmlFor={`rule-channel-${ch.id}`} className="text-sm truncate cursor-pointer flex-1">
												{ch.name}
											</Label>
											<Badge variant="outline" className="text-xs ml-auto shrink-0">
												{ch.kind}
											</Badge>
										</div>
									))}
								</div>
							)}
						</div>
						<div className="space-y-1">
							<Label>{t`Throttle (seconds, 0 = no throttle)`}</Label>
							<Input
								value={throttle}
								onChange={(e) => setThrottle(e.target.value)}
								type="number"
								min={0}
								placeholder="0"
							/>
						</div>
					</div>
					<DialogFooter>
						<Button variant="outline" onClick={onClose}>
							<Trans>Cancel</Trans>
						</Button>
						<Button onClick={handleSubmit} disabled={saving || !name.trim()}>
							{saving && <Loader2Icon className="me-2 size-4 animate-spin" />}
							{isEdit ? <Trans>Save</Trans> : <Trans>Add</Trans>}
						</Button>
					</DialogFooter>
				</DialogContent>
			</Dialog>
		)
	}
)

// --- Channels section ---

const SectionChannels = memo(
	({
		channels,
		onChannelsChange,
	}: {
		channels: NotificationChannel[]
		onChannelsChange: (channels: NotificationChannel[]) => void
	}) => {
		const { t } = useLingui()
		const [dialog, setDialog] = useState<ChannelDialogState>({ open: false, editing: null })
		const [testing, setTesting] = useState<string | null>(null)

		function openCreate() {
			setDialog({ open: true, editing: null })
		}

		function openEdit(ch: NotificationChannel) {
			setDialog({ open: true, editing: ch })
		}

		function closeDialog() {
			setDialog({ open: false, editing: null })
		}

		function handleSaved(saved: NotificationChannel) {
			if (dialog.editing) {
				onChannelsChange(channels.map((c) => (c.id === saved.id ? saved : c)))
			} else {
				onChannelsChange([...channels, saved])
			}
		}

		async function handleDelete(ch: NotificationChannel) {
			try {
				await apiDelete(`/api/app/notifications/channels/${ch.id}`)
				onChannelsChange(channels.filter((c) => c.id !== ch.id))
			} catch (e: unknown) {
				toast({ title: t`Error`, description: (e as Error).message, variant: "destructive" })
			}
		}

		async function handleTest(ch: NotificationChannel) {
			setTesting(ch.id)
			try {
				const result = (await apiPost(`/api/app/notifications/channels/${ch.id}/test`, {})) as {
					ok: boolean
					error?: string
					preview?: string
				}
				if (result.ok) {
					toast({ title: t`Test sent`, description: result.preview ?? t`Message delivered` })
				} else {
					toast({ title: t`Test failed`, description: result.error, variant: "destructive" })
				}
			} catch (e: unknown) {
				toast({ title: t`Error`, description: (e as Error).message, variant: "destructive" })
			} finally {
				setTesting(null)
			}
		}

		async function toggleEnabled(ch: NotificationChannel) {
			try {
				const saved = await apiPatch<NotificationChannel>(`/api/app/notifications/channels/${ch.id}`, {
					enabled: !ch.enabled,
				})
				onChannelsChange(channels.map((c) => (c.id === saved.id ? saved : c)))
			} catch (e: unknown) {
				toast({ title: t`Error`, description: (e as Error).message, variant: "destructive" })
			}
		}

		return (
			<div>
				<div className="flex items-center justify-between mb-3">
					<div>
						<h3 className="text-xl font-medium">
							<Trans>Channels</Trans>
						</h3>
						<p className="text-sm text-muted-foreground mt-0.5">
							<Trans>Notification delivery destinations (email, webhooks, chat services).</Trans>
						</p>
					</div>
					<Button size="sm" onClick={openCreate}>
						<PlusIcon className="me-1.5 size-4" />
						<Trans>Add</Trans>
					</Button>
				</div>
				{channels.length === 0 ? (
					<p className="text-sm text-muted-foreground py-4">
						<Trans>No channels configured. Add one to start sending notifications.</Trans>
					</p>
				) : (
					<div className="rounded-md border overflow-hidden">
						<Table>
							<TableHeader>
								<TableRow className="border-border/50">
									<TableHead>
										<Trans>Name</Trans>
									</TableHead>
									<TableHead>
										<Trans>Kind</Trans>
									</TableHead>
									<TableHead>
										<Trans>Enabled</Trans>
									</TableHead>
									<TableHead className="w-0">
										<span className="sr-only">
											<Trans>Actions</Trans>
										</span>
									</TableHead>
								</TableRow>
							</TableHeader>
							<TableBody>
								{channels.map((ch) => (
									<TableRow key={ch.id}>
										<TableCell className="font-medium py-2 ps-5">{ch.name}</TableCell>
										<TableCell className="py-2">
											<Badge variant="secondary">{ch.kind}</Badge>
										</TableCell>
										<TableCell className="py-2">
											<Switch checked={ch.enabled} onCheckedChange={() => toggleEnabled(ch)} />
										</TableCell>
										<TableCell className="py-2 px-4">
											<DropdownMenu>
												<DropdownMenuTrigger asChild>
													<Button variant="ghost" size="icon" data-nolink>
														<span className="sr-only">
															<Trans>Open menu</Trans>
														</span>
														{testing === ch.id ? (
															<Loader2Icon className="size-4 animate-spin" />
														) : (
															<MoreHorizontalIcon className="size-5" />
														)}
													</Button>
												</DropdownMenuTrigger>
												<DropdownMenuContent align="end">
													<DropdownMenuItem onClick={() => handleTest(ch)} disabled={testing === ch.id}>
														<FlaskConicalIcon className="me-2.5 size-4" />
														<Trans>Send test</Trans>
													</DropdownMenuItem>
													<DropdownMenuSeparator />
													<DropdownMenuItem onClick={() => openEdit(ch)}>
														<PencilIcon className="me-2.5 size-4" />
														<Trans>Edit</Trans>
													</DropdownMenuItem>
													<DropdownMenuSeparator />
													<DropdownMenuItem onClick={() => handleDelete(ch)} className="text-destructive">
														<Trash2Icon className="me-2.5 size-4" />
														<Trans>Delete</Trans>
													</DropdownMenuItem>
												</DropdownMenuContent>
											</DropdownMenu>
										</TableCell>
									</TableRow>
								))}
							</TableBody>
						</Table>
					</div>
				)}
				<ChannelDialog state={dialog} onClose={closeDialog} onSaved={handleSaved} />
			</div>
		)
	}
)

// --- Rules section ---

const SectionRules = memo(
	({
		rules,
		channels,
		onRulesChange,
	}: {
		rules: NotificationRule[]
		channels: NotificationChannel[]
		onRulesChange: (rules: NotificationRule[]) => void
	}) => {
		const { t } = useLingui()
		const [dialog, setDialog] = useState<RuleDialogState>({ open: false, editing: null })

		function openCreate() {
			setDialog({ open: true, editing: null })
		}

		function openEdit(r: NotificationRule) {
			setDialog({ open: true, editing: r })
		}

		function closeDialog() {
			setDialog({ open: false, editing: null })
		}

		function handleSaved(saved: NotificationRule) {
			if (dialog.editing) {
				onRulesChange(rules.map((r) => (r.id === saved.id ? saved : r)))
			} else {
				onRulesChange([...rules, saved])
			}
		}

		async function handleDelete(r: NotificationRule) {
			try {
				await apiDelete(`/api/app/notifications/rules/${r.id}`)
				onRulesChange(rules.filter((x) => x.id !== r.id))
			} catch (e: unknown) {
				toast({ title: t`Error`, description: (e as Error).message, variant: "destructive" })
			}
		}

		async function toggleEnabled(r: NotificationRule) {
			try {
				const saved = await apiPatch<NotificationRule>(`/api/app/notifications/rules/${r.id}`, {
					enabled: !r.enabled,
					name: r.name,
					events: r.events,
					channels: r.channels,
					min_severity: "info",
					throttle_seconds: r.throttle_seconds,
				})
				onRulesChange(rules.map((x) => (x.id === saved.id ? saved : x)))
			} catch (e: unknown) {
				toast({ title: t`Error`, description: (e as Error).message, variant: "destructive" })
			}
		}

		const channelName = (id: string) => channels.find((c) => c.id === id)?.name ?? id.slice(0, 8)

		return (
			<div>
				<div className="flex items-center justify-between mb-3">
					<div>
						<h3 className="text-xl font-medium">
							<Trans>Rules</Trans>
						</h3>
						<p className="text-sm text-muted-foreground mt-0.5">
							<Trans>Routing rules: which events trigger which channels.</Trans>
						</p>
					</div>
					<Button size="sm" onClick={openCreate}>
						<PlusIcon className="me-1.5 size-4" />
						<Trans>Add</Trans>
					</Button>
				</div>
				{rules.length === 0 ? (
					<p className="text-sm text-muted-foreground py-4">
						<Trans>No rules configured. Add a rule to start routing notifications.</Trans>
					</p>
				) : (
					<div className="rounded-md border overflow-hidden">
						<Table>
							<TableHeader>
								<TableRow className="border-border/50">
									<TableHead>
										<Trans>Name</Trans>
									</TableHead>
									<TableHead>
										<Trans>Events</Trans>
									</TableHead>
									<TableHead>
										<Trans>Channels</Trans>
									</TableHead>
									<TableHead>
										<Trans>Enabled</Trans>
									</TableHead>
									<TableHead className="w-0">
										<span className="sr-only">
											<Trans>Actions</Trans>
										</span>
									</TableHead>
								</TableRow>
							</TableHeader>
							<TableBody>
								{rules.map((r) => (
									<TableRow key={r.id}>
										<TableCell className="font-medium py-2 ps-5">{r.name}</TableCell>
										<TableCell className="py-2">
											<div className="flex flex-wrap gap-1">
												{(r.events ?? []).map((ev) => (
													<Badge key={ev} variant="outline" className="text-xs font-mono">
														{ev}
													</Badge>
												))}
											</div>
										</TableCell>
										<TableCell className="py-2">
											<div className="flex flex-wrap gap-1">
												{(r.channels ?? []).map((id) => (
													<Badge key={id} variant="secondary" className="text-xs">
														{channelName(id)}
													</Badge>
												))}
											</div>
										</TableCell>
										<TableCell className="py-2">
											<Switch checked={r.enabled} onCheckedChange={() => toggleEnabled(r)} />
										</TableCell>
										<TableCell className="py-2 px-4">
											<DropdownMenu>
												<DropdownMenuTrigger asChild>
													<Button variant="ghost" size="icon" data-nolink>
														<span className="sr-only">
															<Trans>Open menu</Trans>
														</span>
														<MoreHorizontalIcon className="size-5" />
													</Button>
												</DropdownMenuTrigger>
												<DropdownMenuContent align="end">
													<DropdownMenuItem onClick={() => openEdit(r)}>
														<PencilIcon className="me-2.5 size-4" />
														<Trans>Edit</Trans>
													</DropdownMenuItem>
													<DropdownMenuSeparator />
													<DropdownMenuItem onClick={() => handleDelete(r)} className="text-destructive">
														<Trash2Icon className="me-2.5 size-4" />
														<Trans>Delete</Trans>
													</DropdownMenuItem>
												</DropdownMenuContent>
											</DropdownMenu>
										</TableCell>
									</TableRow>
								))}
							</TableBody>
						</Table>
					</div>
				)}
				<RuleDialog state={dialog} channels={channels} onClose={closeDialog} onSaved={handleSaved} />
			</div>
		)
	}
)

// --- Main page ---

const NotificationsSettings = memo(() => {
	const admin = isAdmin()
	const [channels, setChannels] = useState<NotificationChannel[]>([])
	const [rules, setRules] = useState<NotificationRule[]>([])

	useEffect(() => {
		if (!admin) {
			redirectPage($router, "settings", { name: "general" })
			return
		}
		apiGet<NotificationChannel[]>("/api/app/notifications/channels")
			.then(setChannels)
			.catch(() => {})
		apiGet<NotificationRule[]>("/api/app/notifications/rules")
			.then(setRules)
			.catch(() => {})
	}, [admin])

	if (!admin) {
		return null
	}

	return (
		<>
			<div>
				<h3 className="text-xl font-medium mb-1">
					<Trans>Notifications</Trans>
				</h3>
				<p className="text-sm text-muted-foreground leading-relaxed">
					<Trans>
						Configure notification channels and routing rules. Notifications are sent when monitors, agents, or container images change
						state.
					</Trans>
				</p>
			</div>
			<Separator className="my-4" />
			<Tabs defaultValue="config" className="space-y-4">
				<TabsList>
					<TabsTrigger value="config">
						<Trans>Configuration</Trans>
					</TabsTrigger>
					<TabsTrigger value="history">
						<Trans>History</Trans>
					</TabsTrigger>
				</TabsList>
				<TabsContent value="config" className="space-y-6">
					<SectionChannels channels={channels} onChannelsChange={setChannels} />
					<Separator className="my-6" />
					<SectionRules rules={rules} channels={channels} onRulesChange={setRules} />
				</TabsContent>
				<TabsContent value="history">
					<NotificationHistory rules={rules} channels={channels} />
				</TabsContent>
			</Tabs>
		</>
	)
})

export default NotificationsSettings
