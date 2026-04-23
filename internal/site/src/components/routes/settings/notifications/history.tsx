import { Trans, useLingui } from "@lingui/react/macro"
import { Loader2Icon } from "lucide-react"
import { memo, useEffect, useMemo, useState } from "react"
import { Button } from "@/components/ui/button"
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from "@/components/ui/dialog"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table"
import { pb } from "@/lib/api"
import type { NotificationChannel, NotificationLog, NotificationLogsPage, NotificationRule } from "@/types"

const ALL_FILTERS = "__all__"
const STATUS_VALUES = ["sent", "failed", "throttled"] as const
const EVENT_VALUES = [
	"monitor.down",
	"monitor.up",
	"agent.offline",
	"agent.online",
	"container_image.update_available",
] as const

type NotificationHistoryProps = {
	rules: NotificationRule[]
	channels: NotificationChannel[]
}

type HistoryFilters = {
	ruleId: string
	status: string
	eventKind: string
	since: string
	until: string
}

const defaultFilters: HistoryFilters = {
	ruleId: ALL_FILTERS,
	status: ALL_FILTERS,
	eventKind: ALL_FILTERS,
	since: "",
	until: "",
}

function toQuery(filters: HistoryFilters, page: number, limit: number) {
	const query: Record<string, string | number> = { page, limit }
	if (filters.ruleId !== ALL_FILTERS) query.rule_id = filters.ruleId
	if (filters.status !== ALL_FILTERS) query.status = filters.status
	if (filters.eventKind !== ALL_FILTERS) query.event_kind = filters.eventKind
	if (filters.since) {
		const parsed = new Date(filters.since)
		if (!Number.isNaN(parsed.getTime())) query.since = parsed.toISOString()
	}
	if (filters.until) {
		const parsed = new Date(filters.until)
		if (!Number.isNaN(parsed.getTime())) query.until = parsed.toISOString()
	}
	return query
}

function formatSentAt(sentAt: string) {
	if (!sentAt) return "-"
	const parsed = new Date(sentAt)
	if (Number.isNaN(parsed.getTime())) return sentAt
	return parsed.toLocaleString()
}

function StatusBadge({ status }: { status: NotificationLog["status"] }) {
	const styles = {
		sent: "border-emerald-500/30 bg-emerald-500/10 text-emerald-700 dark:text-emerald-300",
		failed: "border-red-500/30 bg-red-500/10 text-red-700 dark:text-red-300",
		throttled: "border-amber-500/30 bg-amber-500/10 text-amber-700 dark:text-amber-300",
	}

	return (
		<span className={`inline-flex rounded-full border px-2 py-0.5 text-xs font-medium ${styles[status]}`}>
			{status}
		</span>
	)
}

const NotificationHistory = memo(({ rules, channels }: NotificationHistoryProps) => {
	const { t } = useLingui()
	const [filters, setFilters] = useState<HistoryFilters>(defaultFilters)
	const [page, setPage] = useState(1)
	const [limit, setLimit] = useState(25)
	const [result, setResult] = useState<NotificationLogsPage>({ items: [], page: 1, limit: 25, has_more: false })
	const [loading, setLoading] = useState(true)
	const [error, setError] = useState("")
	const [selected, setSelected] = useState<NotificationLog | null>(null)

	const ruleNames = useMemo(() => new Map(rules.map((rule) => [rule.id, rule.name])), [rules])
	const channelNames = useMemo(() => new Map(channels.map((channel) => [channel.id, channel.name])), [channels])

	useEffect(() => {
		let cancelled = false
		setLoading(true)
		setError("")

		pb.send("/api/app/notifications/logs", {
			method: "GET",
			query: toQuery(filters, page, limit),
		})
			.then((data) => {
				if (!cancelled) {
					setResult(data as NotificationLogsPage)
				}
			})
			.catch((err: Error) => {
				if (!cancelled) {
					setError(err.message)
					setResult({ items: [], page, limit, has_more: false })
				}
			})
			.finally(() => {
				if (!cancelled) setLoading(false)
			})

		return () => {
			cancelled = true
		}
	}, [filters, page, limit])

	function updateFilter<K extends keyof HistoryFilters>(key: K, value: HistoryFilters[K]) {
		setPage(1)
		setFilters((current) => ({ ...current, [key]: value }))
	}

	function resetFilters() {
		setPage(1)
		setFilters(defaultFilters)
	}

	return (
		<div className="space-y-4">
			<div>
				<h3 className="text-xl font-medium">
					<Trans>History</Trans>
				</h3>
				<p className="mt-0.5 text-sm text-muted-foreground">
					<Trans>Review recent notification deliveries and inspect provider errors.</Trans>
				</p>
			</div>

			<div className="grid gap-3 rounded-md border p-4 md:grid-cols-2 xl:grid-cols-5">
				<div className="space-y-1">
					<Label>{t`Rule`}</Label>
					<Select value={filters.ruleId} onValueChange={(value) => updateFilter("ruleId", value)}>
						<SelectTrigger>
							<SelectValue />
						</SelectTrigger>
						<SelectContent>
							<SelectItem value={ALL_FILTERS}>{t`All rules`}</SelectItem>
							{rules.map((rule) => (
								<SelectItem key={rule.id} value={rule.id}>
									{rule.name}
								</SelectItem>
							))}
						</SelectContent>
					</Select>
				</div>

				<div className="space-y-1">
					<Label>{t`Status`}</Label>
					<Select value={filters.status} onValueChange={(value) => updateFilter("status", value)}>
						<SelectTrigger>
							<SelectValue />
						</SelectTrigger>
						<SelectContent>
							<SelectItem value={ALL_FILTERS}>{t`All statuses`}</SelectItem>
							{STATUS_VALUES.map((status) => (
								<SelectItem key={status} value={status}>
									{status}
								</SelectItem>
							))}
						</SelectContent>
					</Select>
				</div>

				<div className="space-y-1">
					<Label>{t`Event`}</Label>
					<Select value={filters.eventKind} onValueChange={(value) => updateFilter("eventKind", value)}>
						<SelectTrigger>
							<SelectValue />
						</SelectTrigger>
						<SelectContent>
							<SelectItem value={ALL_FILTERS}>{t`All events`}</SelectItem>
							{EVENT_VALUES.map((eventKind) => (
								<SelectItem key={eventKind} value={eventKind}>
									{eventKind}
								</SelectItem>
							))}
						</SelectContent>
					</Select>
				</div>

				<div className="space-y-1">
					<Label>{t`From`}</Label>
					<Input type="datetime-local" value={filters.since} onChange={(e) => updateFilter("since", e.target.value)} />
				</div>

				<div className="space-y-1">
					<Label>{t`To`}</Label>
					<Input type="datetime-local" value={filters.until} onChange={(e) => updateFilter("until", e.target.value)} />
				</div>

				<div className="flex items-end md:col-span-2 xl:col-span-5">
					<Button variant="outline" onClick={resetFilters}>
						<Trans>Reset filters</Trans>
					</Button>
				</div>
			</div>

			{error ? <p className="text-sm text-destructive">{error}</p> : null}

			<div className="space-y-3">
				<div className="flex items-center justify-between gap-3">
					<p className="text-sm text-muted-foreground">
						<Trans>{result.items.length} entries on this page</Trans>
					</p>
					<div className="flex items-center gap-2">
						<span className="text-xs text-muted-foreground">
							<Trans>Rows</Trans>
						</span>
						<Select
							value={String(limit)}
							onValueChange={(value) => {
								setPage(1)
								setLimit(Number(value))
							}}
						>
							<SelectTrigger className="w-[84px]">
								<SelectValue />
							</SelectTrigger>
							<SelectContent>
								{[10, 25, 50, 100].map((value) => (
									<SelectItem key={value} value={String(value)}>
										{value}
									</SelectItem>
								))}
							</SelectContent>
						</Select>
					</div>
				</div>

				<div className="overflow-x-auto rounded-md border border-border/60">
					<Table>
						<TableHeader>
							<TableRow>
								<TableHead>{t`Sent`}</TableHead>
								<TableHead>{t`Event`}</TableHead>
								<TableHead>{t`Rule`}</TableHead>
								<TableHead>{t`Channel`}</TableHead>
								<TableHead>{t`Status`}</TableHead>
								<TableHead>{t`Resource`}</TableHead>
								<TableHead className="w-0">
									<span className="sr-only">{t`Details`}</span>
								</TableHead>
							</TableRow>
						</TableHeader>
						<TableBody>
							{loading ? (
								<TableRow>
									<TableCell colSpan={7} className="h-24 text-center text-sm text-muted-foreground">
										<span className="inline-flex items-center gap-2">
											<Loader2Icon className="size-4 animate-spin" />
											<Trans>Loading history…</Trans>
										</span>
									</TableCell>
								</TableRow>
							) : result.items.length === 0 ? (
								<TableRow>
									<TableCell colSpan={7} className="h-24 text-center text-sm text-muted-foreground">
										<Trans>No notification logs match the current filters.</Trans>
									</TableCell>
								</TableRow>
							) : (
								result.items.map((log) => (
									<TableRow key={log.id}>
										<TableCell className="whitespace-nowrap">{formatSentAt(log.sent_at)}</TableCell>
										<TableCell className="font-mono text-xs">{log.event_kind}</TableCell>
										<TableCell>{ruleNames.get(log.rule) ?? log.rule.slice(0, 8)}</TableCell>
										<TableCell>{channelNames.get(log.channel) ?? log.channel.slice(0, 8)}</TableCell>
										<TableCell>
											<StatusBadge status={log.status} />
										</TableCell>
										<TableCell className="text-xs text-muted-foreground">
											{log.resource_type}:{log.resource_id}
										</TableCell>
										<TableCell>
											<Button variant="ghost" size="sm" onClick={() => setSelected(log)}>
												<Trans>Details</Trans>
											</Button>
										</TableCell>
									</TableRow>
								))
							)}
						</TableBody>
					</Table>
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

			<Dialog open={!!selected} onOpenChange={(open) => !open && setSelected(null)}>
				<DialogContent className="sm:max-w-2xl">
					<DialogHeader>
						<DialogTitle>{selected?.event_kind ?? t`Notification details`}</DialogTitle>
						<DialogDescription>
							{selected ? `${formatSentAt(selected.sent_at)} · ${ruleNames.get(selected.rule) ?? selected.rule}` : ""}
						</DialogDescription>
					</DialogHeader>

					{selected ? (
						<div className="space-y-4">
							<div className="grid gap-3 sm:grid-cols-2">
								<div>
									<p className="text-xs font-medium uppercase tracking-wide text-muted-foreground">{t`Status`}</p>
									<div className="mt-1">
										<StatusBadge status={selected.status} />
									</div>
								</div>
								<div>
									<p className="text-xs font-medium uppercase tracking-wide text-muted-foreground">{t`Channel`}</p>
									<p className="mt-1 text-sm">{channelNames.get(selected.channel) ?? selected.channel}</p>
								</div>
								<div>
									<p className="text-xs font-medium uppercase tracking-wide text-muted-foreground">{t`Resource`}</p>
									<p className="mt-1 text-sm">
										{selected.resource_type}:{selected.resource_id}
									</p>
								</div>
								<div>
									<p className="text-xs font-medium uppercase tracking-wide text-muted-foreground">{t`Rule`}</p>
									<p className="mt-1 text-sm">{ruleNames.get(selected.rule) ?? selected.rule}</p>
								</div>
							</div>

							<div>
								<p className="text-xs font-medium uppercase tracking-wide text-muted-foreground">{t`Payload preview`}</p>
								<pre className="mt-1 max-h-60 overflow-auto rounded-md border bg-muted/30 p-3 text-xs whitespace-pre-wrap break-words">
									{selected.payload_preview || "-"}
								</pre>
							</div>

							<div>
								<p className="text-xs font-medium uppercase tracking-wide text-muted-foreground">{t`Error`}</p>
								<pre className="mt-1 max-h-60 overflow-auto rounded-md border bg-muted/30 p-3 text-xs whitespace-pre-wrap break-words">
									{selected.error || "-"}
								</pre>
							</div>
						</div>
					) : null}
				</DialogContent>
			</Dialog>
		</div>
	)
})

export default NotificationHistory
