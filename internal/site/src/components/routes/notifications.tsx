import { Trans, useLingui } from "@lingui/react/macro"
import { useStore } from "@nanostores/react"
import { BellIcon, CheckCheckIcon, Loader2Icon, SearchIcon, XIcon } from "lucide-react"
import { useEffect, useState } from "react"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table"
import { pb } from "@/lib/api"
import { $systemNotificationsReadStamp, bumpSystemNotificationsReadStamp } from "@/lib/stores"
import type { SystemNotification, SystemNotificationCategory, SystemNotificationsPage } from "@/types"

const ALL_FILTERS = "__all__"
const CATEGORIES: SystemNotificationCategory[] = ["monitors", "agents", "container_images"]
const SEVERITIES = ["info", "warning", "critical"] as const

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

function notificationDescription(item: SystemNotification) {
	if (!item.message) return ""
	if (item.message.trim() === item.title.trim()) return ""
	return item.message
}

export default function NotificationsPage() {
	const { t } = useLingui()
	const [category, setCategory] = useState(ALL_FILTERS)
	const [severity, setSeverity] = useState(ALL_FILTERS)
	const [status, setStatus] = useState("unread")
	const [search, setSearch] = useState("")
	const [page, setPage] = useState(1)
	const [result, setResult] = useState<SystemNotificationsPage>({ items: [], page: 1, limit: 25, has_more: false })
	const [loading, setLoading] = useState(true)
	const [saving, setSaving] = useState(false)
	const [error, setError] = useState("")
	const readStamp = useStore($systemNotificationsReadStamp)

	async function fetchNotifications() {
		setLoading(true)
		setError("")
		try {
			const query: Record<string, string | number> = { page, limit: 25 }
			if (category !== ALL_FILTERS) query.category = category
			if (severity !== ALL_FILTERS) query.severity = severity
			if (status !== ALL_FILTERS) query.status = status
			if (search.trim()) query.q = search.trim()
			const data = await pb.send<SystemNotificationsPage>("/api/app/system-notifications", { method: "GET", query })
			setResult(data)
		} catch (e) {
			setError((e as Error).message)
		} finally {
			setLoading(false)
		}
	}

	useEffect(() => {
		fetchNotifications()
	}, [category, severity, status, search, page, readStamp])

	async function markAllRead() {
		setSaving(true)
		try {
			const query = category !== ALL_FILTERS ? { category } : undefined
			await pb.send("/api/app/system-notifications/read-all", { method: "POST", query })
			// The fetchNotifications re-run is driven by the readStamp-bumped useEffect
			// below; awaiting it here as well races the same URL and the PocketBase SDK
			// auto-cancels the older request.
			bumpSystemNotificationsReadStamp()
		} finally {
			setSaving(false)
		}
	}

	function resetFilters() {
		setPage(1)
		setCategory(ALL_FILTERS)
		setSeverity(ALL_FILTERS)
		setStatus("unread")
		setSearch("")
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

	function statusLabel(value: string) {
		return value === "unread" ? t`Unread` : t`All statuses`
	}

	const hasActiveFilters = Boolean(
		search.trim() || category !== ALL_FILTERS || severity !== ALL_FILTERS || status !== "unread"
	)

	return (
		<div className="space-y-5">
			<div>
				<h1 className="flex items-center gap-2 text-2xl font-semibold tracking-tight">
					<BellIcon className="size-5" />
					<Trans>Notifications</Trans>
				</h1>
				<p className="mt-1 text-sm text-muted-foreground">
					<Trans>Review system events from monitors, agents, and container image audits.</Trans>
				</p>
			</div>

			<div className="space-y-3">
				<div className="flex flex-col gap-2 sm:flex-row sm:flex-wrap sm:items-center">
					<div className="relative sm:w-[280px]">
						<SearchIcon className="absolute left-2.5 top-1/2 size-4 -translate-y-1/2 text-muted-foreground" />
						<Input
							value={search}
							onChange={(event) => {
								setPage(1)
								setSearch(event.target.value)
							}}
							placeholder={t`Search notifications…`}
							className="pl-8"
						/>
					</div>

					<Select
						value={category}
						onValueChange={(value) => {
							setPage(1)
							setCategory(value)
						}}
					>
						<SelectTrigger className="sm:w-[180px]">
							<SelectValue />
						</SelectTrigger>
						<SelectContent>
							<SelectItem value={ALL_FILTERS}>{t`All categories`}</SelectItem>
							{CATEGORIES.map((item) => (
								<SelectItem key={item} value={item}>
									{categoryLabel(item)}
								</SelectItem>
							))}
						</SelectContent>
					</Select>

					<Select
						value={status}
						onValueChange={(value) => {
							setPage(1)
							setStatus(value)
						}}
					>
						<SelectTrigger className="sm:w-[150px]">
							<SelectValue />
						</SelectTrigger>
						<SelectContent>
							<SelectItem value="unread">{t`Unread`}</SelectItem>
							<SelectItem value={ALL_FILTERS}>{t`All statuses`}</SelectItem>
						</SelectContent>
					</Select>

					<Select
						value={severity}
						onValueChange={(value) => {
							setPage(1)
							setSeverity(value)
						}}
					>
						<SelectTrigger className="sm:w-[160px]">
							<SelectValue />
						</SelectTrigger>
						<SelectContent>
							<SelectItem value={ALL_FILTERS}>{t`All severities`}</SelectItem>
							{SEVERITIES.map((item) => (
								<SelectItem key={item} value={item}>
									{item}
								</SelectItem>
							))}
						</SelectContent>
					</Select>

					<Button variant="outline" onClick={resetFilters} disabled={!hasActiveFilters}>
						<Trans>Reset filters</Trans>
					</Button>

					<Button onClick={markAllRead} disabled={saving} className="sm:ml-auto">
						{saving ? <Loader2Icon className="me-2 size-4 animate-spin" /> : <CheckCheckIcon className="me-2 size-4" />}
						<Trans>Mark all as read</Trans>
					</Button>
				</div>

				{hasActiveFilters ? (
					<div className="flex flex-wrap gap-1.5">
						{search.trim() ? (
							<FilterPill
								label={`"${search.trim()}"`}
								onRemove={() => {
									setPage(1)
									setSearch("")
								}}
							/>
						) : null}
						{category !== ALL_FILTERS ? (
							<FilterPill
								label={categoryLabel(category as SystemNotificationCategory)}
								onRemove={() => {
									setPage(1)
									setCategory(ALL_FILTERS)
								}}
							/>
						) : null}
						{status !== "unread" ? (
							<FilterPill
								label={statusLabel(status)}
								onRemove={() => {
									setPage(1)
									setStatus("unread")
								}}
							/>
						) : null}
						{severity !== ALL_FILTERS ? (
							<FilterPill
								label={severity}
								onRemove={() => {
									setPage(1)
									setSeverity(ALL_FILTERS)
								}}
							/>
						) : null}
					</div>
				) : null}
			</div>

			{error ? <p className="text-sm text-destructive">{error}</p> : null}

			<div className="overflow-x-auto rounded-md border border-border/60">
				{loading ? (
					<div className="flex h-32 items-center justify-center text-sm text-muted-foreground">
						<Loader2Icon className="me-2 size-4 animate-spin" />
						<Trans>Loading notifications…</Trans>
					</div>
				) : (
					<Table className="min-w-[880px]">
						<TableHeader>
							<TableRow>
								<TableHead>
									<Trans>Notification</Trans>
								</TableHead>
								<TableHead>
									<Trans>Resource</Trans>
								</TableHead>
								<TableHead>
									<Trans>Category</Trans>
								</TableHead>
								<TableHead>
									<Trans>Status</Trans>
								</TableHead>
								<TableHead>
									<Trans>Severity</Trans>
								</TableHead>
								<TableHead className="text-right">
									<Trans>Date</Trans>
								</TableHead>
							</TableRow>
						</TableHeader>
						<TableBody>
							{result.items.length === 0 ? (
								<TableRow>
									<TableCell colSpan={6} className="h-24 text-center text-sm text-muted-foreground">
										<div className="flex flex-col items-center gap-2">
											<Trans>No notifications match the current filters.</Trans>
											<Button variant="link" size="sm" className="h-auto p-0 text-xs" onClick={resetFilters}>
												<Trans>Reset filters</Trans>
											</Button>
										</div>
									</TableCell>
								</TableRow>
							) : (
								result.items.map((item) => {
									const description = notificationDescription(item)
									return (
										<TableRow key={item.id}>
											<TableCell className="max-w-[34rem]">
												<div className="font-medium">{item.title}</div>
												{description ? (
													<div className="mt-1 truncate text-xs text-muted-foreground">{description}</div>
												) : null}
											</TableCell>
											<TableCell>
												<div className="font-mono text-xs">{item.resource_name || item.resource_id || "-"}</div>
												{item.resource_type ? (
													<div className="text-xs text-muted-foreground">{item.resource_type}</div>
												) : null}
											</TableCell>
											<TableCell>
												<Badge variant="outline" className="border-border/50 text-[10px]">
													{categoryLabel(item.category)}
												</Badge>
											</TableCell>
											<TableCell>
												{item.read ? (
													<span className="text-xs text-muted-foreground">{t`Read`}</span>
												) : (
													<Badge variant="default" className="text-[10px]">
														{t`Unread`}
													</Badge>
												)}
											</TableCell>
											<TableCell>
												<Badge variant={severityVariant(item.severity)} className="text-[10px] uppercase">
													{item.severity}
												</Badge>
											</TableCell>
											<TableCell className="text-right text-xs text-muted-foreground">
												{formatDate(item.occurred_at)}
											</TableCell>
										</TableRow>
									)
								})
							)}
						</TableBody>
					</Table>
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
