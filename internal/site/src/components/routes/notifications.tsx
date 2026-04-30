import { Trans, useLingui } from "@lingui/react/macro"
import { BellIcon, CheckCheckIcon, Loader2Icon } from "lucide-react"
import { useEffect, useState } from "react"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Checkbox } from "@/components/ui/checkbox"
import { Label } from "@/components/ui/label"
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"
import { pb } from "@/lib/api"
import type {
	SystemNotification,
	SystemNotificationCategory,
	SystemNotificationPreferences,
	SystemNotificationsPage,
} from "@/types"

const ALL_FILTERS = "__all__"
const CATEGORIES: SystemNotificationCategory[] = ["monitors", "agents", "container_images"]
const SEVERITIES = ["info", "warning", "critical"] as const

function formatDate(value: string) {
	if (!value) return "-"
	const parsed = new Date(value)
	if (Number.isNaN(parsed.getTime())) return value
	return parsed.toLocaleString()
}

function severityVariant(severity: SystemNotification["severity"]) {
	switch (severity) {
		case "critical":
			return "danger" as const
		case "warning":
			return "warning" as const
		default:
			return "secondary" as const
	}
}

export default function NotificationsPage() {
	const { t } = useLingui()
	const [category, setCategory] = useState(ALL_FILTERS)
	const [severity, setSeverity] = useState(ALL_FILTERS)
	const [status, setStatus] = useState("unread")
	const [page, setPage] = useState(1)
	const [result, setResult] = useState<SystemNotificationsPage>({ items: [], page: 1, limit: 25, has_more: false })
	const [preferences, setPreferences] = useState<SystemNotificationPreferences>({
		enabled_categories: { monitors: true, agents: true, container_images: true },
	})
	const [loading, setLoading] = useState(true)
	const [saving, setSaving] = useState(false)
	const [error, setError] = useState("")

	async function fetchNotifications() {
		setLoading(true)
		setError("")
		try {
			const query: Record<string, string | number> = { page, limit: 25 }
			if (category !== ALL_FILTERS) query.category = category
			if (severity !== ALL_FILTERS) query.severity = severity
			if (status !== ALL_FILTERS) query.status = status
			const data = await pb.send<SystemNotificationsPage>("/api/app/system-notifications", { method: "GET", query })
			setResult(data)
		} catch (e) {
			setError((e as Error).message)
		} finally {
			setLoading(false)
		}
	}

	async function fetchPreferences() {
		try {
			const data = await pb.send<SystemNotificationPreferences>("/api/app/system-notifications/preferences", {
				method: "GET",
			})
			setPreferences(data)
		} catch {
			// keep defaults
		}
	}

	useEffect(() => {
		fetchPreferences()
	}, [])

	useEffect(() => {
		fetchNotifications()
	}, [category, severity, status, page])

	async function markAllRead() {
		setSaving(true)
		try {
			const query = category !== ALL_FILTERS ? { category } : undefined
			await pb.send("/api/app/system-notifications/read-all", { method: "POST", query })
			await fetchNotifications()
		} finally {
			setSaving(false)
		}
	}

	async function toggleCategory(category: SystemNotificationCategory, enabled: boolean) {
		const next = {
			enabled_categories: {
				...preferences.enabled_categories,
				[category]: enabled,
			},
		}
		setPreferences(next)
		try {
			const saved = await pb.send<SystemNotificationPreferences>("/api/app/system-notifications/preferences", {
				method: "PATCH",
				body: JSON.stringify(next),
				headers: { "Content-Type": "application/json" },
			})
			setPreferences(saved)
			await fetchNotifications()
		} catch {
			await fetchPreferences()
		}
	}

	function resetFilters() {
		setPage(1)
		setCategory(ALL_FILTERS)
		setSeverity(ALL_FILTERS)
		setStatus("unread")
	}

	function categoryLabel(category: SystemNotificationCategory) {
		switch (category) {
			case "monitors":
				return t`Monitors`
			case "agents":
				return t`Agents`
			case "container_images":
				return t`Container images`
		}
	}

	return (
		<div className="space-y-5">
			<div className="flex flex-col gap-3 sm:flex-row sm:items-start sm:justify-between">
				<div>
					<h1 className="flex items-center gap-2 text-2xl font-semibold tracking-tight">
						<BellIcon className="size-5" />
						<Trans>Notifications</Trans>
					</h1>
					<p className="mt-1 text-sm text-muted-foreground">
						<Trans>Review system events from monitors, agents, and container image audits.</Trans>
					</p>
				</div>
				<Button onClick={markAllRead} disabled={saving}>
					{saving ? <Loader2Icon className="me-2 size-4 animate-spin" /> : <CheckCheckIcon className="me-2 size-4" />}
					<Trans>Mark all as read</Trans>
				</Button>
			</div>

			<div className="rounded-md border p-4">
				<h2 className="text-lg font-medium">
					<Trans>Notification preferences</Trans>
				</h2>
				<p className="mt-1 text-sm text-muted-foreground">
					<Trans>Choose which categories appear in the navbar bell.</Trans>
				</p>
				<div className="mt-4 grid gap-3 sm:grid-cols-3">
					{CATEGORIES.map((item) => {
						const id = `system-notification-category-${item}`
						return (
							<div key={item} className="flex items-center gap-2 rounded-md border px-3 py-2">
								<Checkbox
									id={id}
									checked={preferences.enabled_categories[item]}
									onCheckedChange={(checked) => toggleCategory(item, checked === true)}
								/>
								<Label htmlFor={id} className="text-sm">
									{categoryLabel(item)}
								</Label>
							</div>
						)
					})}
				</div>
			</div>

			<div className="grid gap-3 rounded-md border p-4 md:grid-cols-4">
				<div className="space-y-1">
					<Label>{t`Category`}</Label>
					<Select
						value={category}
						onValueChange={(value) => {
							setPage(1)
							setCategory(value)
						}}
					>
						<SelectTrigger>
							<SelectValue />
						</SelectTrigger>
						<SelectContent>
							<SelectItem value={ALL_FILTERS}>{t`All`}</SelectItem>
							{CATEGORIES.map((item) => (
								<SelectItem key={item} value={item}>
									{categoryLabel(item)}
								</SelectItem>
							))}
						</SelectContent>
					</Select>
				</div>
				<div className="space-y-1">
					<Label>{t`Status`}</Label>
					<Select
						value={status}
						onValueChange={(value) => {
							setPage(1)
							setStatus(value)
						}}
					>
						<SelectTrigger>
							<SelectValue />
						</SelectTrigger>
						<SelectContent>
							<SelectItem value="unread">{t`Unread`}</SelectItem>
							<SelectItem value={ALL_FILTERS}>{t`All`}</SelectItem>
						</SelectContent>
					</Select>
				</div>
				<div className="space-y-1">
					<Label>{t`Severity`}</Label>
					<Select
						value={severity}
						onValueChange={(value) => {
							setPage(1)
							setSeverity(value)
						}}
					>
						<SelectTrigger>
							<SelectValue />
						</SelectTrigger>
						<SelectContent>
							<SelectItem value={ALL_FILTERS}>{t`All`}</SelectItem>
							{SEVERITIES.map((item) => (
								<SelectItem key={item} value={item}>
									{item}
								</SelectItem>
							))}
						</SelectContent>
					</Select>
				</div>
				<div className="flex items-end">
					<Button variant="outline" onClick={resetFilters}>
						<Trans>Reset filters</Trans>
					</Button>
				</div>
			</div>

			{error ? <p className="text-sm text-destructive">{error}</p> : null}

			<div className="rounded-md border">
				{loading ? (
					<div className="flex h-32 items-center justify-center text-sm text-muted-foreground">
						<Loader2Icon className="me-2 size-4 animate-spin" />
						<Trans>Loading notifications…</Trans>
					</div>
				) : result.items.length === 0 ? (
					<div className="h-32 p-6 text-sm text-muted-foreground">
						<Trans>No notifications match the current filters.</Trans>
					</div>
				) : (
					<div className="divide-y">
						{result.items.map((item) => (
							<div key={item.id} className="p-4">
								<div className="flex flex-col gap-2 sm:flex-row sm:items-start sm:justify-between">
									<div className="min-w-0">
										<div className="flex flex-wrap items-center gap-2">
											<h2 className="font-medium">{item.title}</h2>
											{item.read ? null : <Badge variant="default">{t`Unread`}</Badge>}
										</div>
										<p className="mt-1 text-sm text-muted-foreground">{item.message}</p>
										<p className="mt-2 text-xs text-muted-foreground">
											{categoryLabel(item.category)} · {item.resource_name || item.resource_id || item.resource_type} ·{" "}
											{formatDate(item.occurred_at)}
										</p>
									</div>
									<Badge variant={severityVariant(item.severity)} className="shrink-0 uppercase">
										{item.severity}
									</Badge>
								</div>
							</div>
						))}
					</div>
				)}
			</div>

			<div className="flex items-center justify-between text-xs text-muted-foreground">
				<span>
					<Trans>Page {result.page}</Trans>
				</span>
				<div className="flex items-center gap-2">
					<Button
						variant="outline"
						size="sm"
						onClick={() => setPage((current) => current - 1)}
						disabled={page <= 1 || loading}
					>
						<Trans>Previous</Trans>
					</Button>
					<Button
						variant="outline"
						size="sm"
						onClick={() => setPage((current) => current + 1)}
						disabled={!result.has_more || loading}
					>
						<Trans>Next</Trans>
					</Button>
				</div>
			</div>
		</div>
	)
}
