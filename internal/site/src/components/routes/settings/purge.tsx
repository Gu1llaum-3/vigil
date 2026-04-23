import { Trans, useLingui } from "@lingui/react/macro"
import { redirectPage } from "@nanostores/router"
import { Loader2Icon, Trash2Icon } from "lucide-react"
import { memo, useEffect, useMemo, useState } from "react"
import { $router } from "@/components/router"
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
import { Button } from "@/components/ui/button"
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { Separator } from "@/components/ui/separator"
import { toast } from "@/components/ui/use-toast"
import { isAdmin, pb } from "@/lib/api"
import { cn } from "@/lib/utils"
import type { PurgeRunResponse, PurgeSettings } from "@/types"

const RETENTION_OPTIONS = [30, 90, 180, 360] as const

type PurgeScope = "monitor_events" | "notification_logs" | "offline_agents"
type PurgeMode = "older_than_days" | "all"

type ConfirmState = {
	open: boolean
	scope: PurgeScope
	mode: PurgeMode
	days?: number
	title: string
	description: string
}

const defaultConfirmState: ConfirmState = {
	open: false,
	scope: "monitor_events",
	mode: "older_than_days",
	title: "",
	description: "",
}

function apiGet<T>(path: string): Promise<T> {
	return pb.send(path, { method: "GET" }) as Promise<T>
}

function apiPatch<T>(path: string, body: unknown): Promise<T> {
	return pb.send(path, {
		method: "PATCH",
		body: JSON.stringify(body),
		headers: { "Content-Type": "application/json" },
	}) as Promise<T>
}

function apiPost<T>(path: string, body: unknown): Promise<T> {
	return pb.send(path, {
		method: "POST",
		body: JSON.stringify(body),
		headers: { "Content-Type": "application/json" },
	}) as Promise<T>
}

const PurgeSettingsPage = memo(() => {
	const { t } = useLingui()
	const admin = isAdmin()
	const [settings, setSettings] = useState<PurgeSettings | null>(null)
	const [loading, setLoading] = useState(true)
	const [saving, setSaving] = useState(false)
	const [running, setRunning] = useState<PurgeScope | null>(null)
	const [confirm, setConfirm] = useState<ConfirmState>(defaultConfirmState)

	const [monitorRetention, setMonitorRetention] = useState("30")
	const [notificationRetention, setNotificationRetention] = useState("30")
	const [monitorManualDays, setMonitorManualDays] = useState("30")
	const [notificationManualDays, setNotificationManualDays] = useState("30")
	const [offlineAgentsManualDays, setOfflineAgentsManualDays] = useState("180")

	useEffect(() => {
		if (!admin) {
			redirectPage($router, "settings", { name: "general" })
			return
		}
		apiGet<PurgeSettings>("/api/app/purge/settings")
			.then((res) => {
				setSettings(res)
				setMonitorRetention(String(res.monitor_events_retention_days))
				setNotificationRetention(String(res.notification_logs_retention_days))
				setMonitorManualDays(String(res.monitor_events_retention_days))
				setNotificationManualDays(String(res.notification_logs_retention_days))
				setOfflineAgentsManualDays(String(res.offline_agents_manual_default_days))
			})
			.catch((error: unknown) => {
				toast({
					title: t`Failed to load purge settings`,
					description: (error as Error).message,
					variant: "destructive",
				})
			})
			.finally(() => setLoading(false))
	}, [admin, t])

	const hasChanges = useMemo(() => {
		if (!settings) return false
		return (
			monitorRetention !== String(settings.monitor_events_retention_days) ||
			notificationRetention !== String(settings.notification_logs_retention_days)
		)
	}, [settings, monitorRetention, notificationRetention])

	useEffect(() => {
		setMonitorManualDays(monitorRetention)
	}, [monitorRetention])

	useEffect(() => {
		setNotificationManualDays(notificationRetention)
	}, [notificationRetention])

	if (!admin) {
		return null
	}

	const save = async () => {
		setSaving(true)
		try {
			const updated = await apiPatch<PurgeSettings>("/api/app/purge/settings", {
				monitor_events_retention_days: Number(monitorRetention),
				notification_logs_retention_days: Number(notificationRetention),
				monitor_events_manual_default_days: settings?.monitor_events_manual_default_days ?? Number(monitorRetention),
				notification_logs_manual_default_days:
					settings?.notification_logs_manual_default_days ?? Number(notificationRetention),
				offline_agents_manual_default_days: settings?.offline_agents_manual_default_days ?? 180,
			})
			setSettings(updated)
			setMonitorManualDays(String(updated.monitor_events_retention_days))
			setNotificationManualDays(String(updated.notification_logs_retention_days))
			toast({
				title: t`Settings saved`,
				description: t`Automatic retention settings have been updated.`,
			})
		} catch (error: unknown) {
			toast({
				title: t`Failed to save purge settings`,
				description: (error as Error).message,
				variant: "destructive",
			})
		} finally {
			setSaving(false)
		}
	}

	const openConfirm = (state: Omit<ConfirmState, "open">) => setConfirm({ open: true, ...state })

	const runPurge = async () => {
		setRunning(confirm.scope)
		try {
			const res = await apiPost<PurgeRunResponse>("/api/app/purge/run", {
				scope: confirm.scope,
				mode: confirm.mode,
				days: confirm.days,
			})
			toast({
				title: t`Purge completed`,
				description: t`${res.deleted_count} record(s) deleted.`,
			})
			setConfirm(defaultConfirmState)
		} catch (error: unknown) {
			toast({
				title: t`Purge failed`,
				description: (error as Error).message,
				variant: "destructive",
			})
		} finally {
			setRunning(null)
		}
	}

	const monitorDays = Number(monitorManualDays) || 0
	const notificationDays = Number(notificationManualDays) || 0
	const offlineDays = Number(offlineAgentsManualDays) || 0

	return (
		<>
			<div>
				<h3 className="text-xl font-medium mb-1">
					<Trans>Purge</Trans>
				</h3>
				<p className="text-sm text-muted-foreground leading-relaxed">
					<Trans>
						Configure automatic retention for monitoring and notification history, then run manual cleanups when needed.
					</Trans>
				</p>
			</div>
			<Separator className="my-4" />
			{loading ? (
				<div className="flex items-center gap-2 text-sm text-muted-foreground">
					<Loader2Icon className="size-4 animate-spin" />
					<Trans>Loading purge settings…</Trans>
				</div>
			) : (
				<div className="space-y-6">
					<Card>
						<CardHeader>
							<CardTitle>
								<Trans>Automatic Retention</Trans>
							</CardTitle>
							<CardDescription>
								<Trans>
									A background job runs daily and deletes monitoring events and notification logs older than the
									configured retention window.
								</Trans>
							</CardDescription>
						</CardHeader>
						<CardContent className="space-y-5">
							<div className="grid gap-2">
								<Label>
									<Trans>Probe History Retention (days)</Trans>
								</Label>
								<RetentionSegment value={monitorRetention} onChange={setMonitorRetention} />
							</div>
							<div className="grid gap-2">
								<Label>
									<Trans>Notification History Retention (days)</Trans>
								</Label>
								<RetentionSegment value={notificationRetention} onChange={setNotificationRetention} />
							</div>
							<div className="flex justify-end">
								<Button disabled={!hasChanges || saving} onClick={save}>
									{saving && <Loader2Icon className="mr-2 size-4 animate-spin" />}
									<Trans>Save Retention Settings</Trans>
								</Button>
							</div>
						</CardContent>
					</Card>

					<Card>
						<CardHeader>
							<CardTitle>
								<Trans>Manual Purge</Trans>
							</CardTitle>
							<CardDescription>
								<Trans>Run manual cleanups for old history or remove all offline hosts when needed.</Trans>
							</CardDescription>
						</CardHeader>
						<CardContent className="space-y-6">
							<PurgeSection
								title={t`Probes`}
								description={t`Delete monitoring events older than the chosen number of days, or wipe the entire probe history.`}
								days={monitorManualDays}
								onDaysChange={setMonitorManualDays}
								running={running === "monitor_events"}
								daysLabel={t`Days to keep`}
								onPurge={() =>
									openConfirm({
										scope: "monitor_events",
										mode: "older_than_days",
										days: monitorDays,
										title: t`Purge old probe history?`,
										description: t`This will delete probe events older than ${monitorDays} days.`,
									})
								}
								onPurgeAll={() =>
									openConfirm({
										scope: "monitor_events",
										mode: "all",
										title: t`Delete all probe history?`,
										description: t`This will permanently delete all probe events.`,
									})
								}
							/>
							<Separator />
							<PurgeSection
								title={t`Notifications`}
								description={t`Delete notification delivery logs older than the chosen number of days, or wipe the full notification history.`}
								days={notificationManualDays}
								onDaysChange={setNotificationManualDays}
								running={running === "notification_logs"}
								daysLabel={t`Days to keep`}
								onPurge={() =>
									openConfirm({
										scope: "notification_logs",
										mode: "older_than_days",
										days: notificationDays,
										title: t`Purge old notification history?`,
										description: t`This will delete notification logs older than ${notificationDays} days.`,
									})
								}
								onPurgeAll={() =>
									openConfirm({
										scope: "notification_logs",
										mode: "all",
										title: t`Delete all notification history?`,
										description: t`This will permanently delete all notification logs.`,
									})
								}
							/>
							<Separator />
							<PurgeSection
								title={t`Offline Hosts`}
								description={t`Hosts are not historized. This cleanup deletes offline agent records and their current snapshots.`}
								days={offlineAgentsManualDays}
								onDaysChange={setOfflineAgentsManualDays}
								running={running === "offline_agents"}
								daysLabel={t`Offline for more than (days)`}
								purgeLabel={t`Purge Offline Hosts`}
								purgeAllLabel={t`Delete All Offline Hosts`}
								onPurge={() =>
									openConfirm({
										scope: "offline_agents",
										mode: "older_than_days",
										days: offlineDays,
										title: t`Purge old offline hosts?`,
										description: t`This will delete offline hosts whose last seen timestamp is older than ${offlineDays} days, plus their snapshots.`,
									})
								}
								onPurgeAll={() =>
									openConfirm({
										scope: "offline_agents",
										mode: "all",
										title: t`Delete all offline hosts?`,
										description: t`This will permanently delete all offline hosts and their snapshots. Connected hosts will not be touched.`,
									})
								}
							/>
						</CardContent>
					</Card>
				</div>
			)}

			<AlertDialog open={confirm.open} onOpenChange={(open) => setConfirm((state) => ({ ...state, open }))}>
				<AlertDialogContent>
					<AlertDialogHeader>
						<AlertDialogTitle>{confirm.title}</AlertDialogTitle>
						<AlertDialogDescription>{confirm.description}</AlertDialogDescription>
					</AlertDialogHeader>
					<AlertDialogFooter>
						<AlertDialogCancel>
							<Trans>Cancel</Trans>
						</AlertDialogCancel>
						<AlertDialogAction
							onClick={(e) => {
								e.preventDefault()
								runPurge()
							}}
						>
							{running === confirm.scope && <Loader2Icon className="mr-2 size-4 animate-spin" />}
							<Trans>Confirm Purge</Trans>
						</AlertDialogAction>
					</AlertDialogFooter>
				</AlertDialogContent>
			</AlertDialog>
		</>
	)
})

function PurgeSection({
	title,
	description,
	days,
	onDaysChange,
	onPurge,
	onPurgeAll,
	running,
	daysLabel,
	purgeLabel,
	purgeAllLabel,
}: {
	title: string
	description: string
	days: string
	onDaysChange: (value: string) => void
	onPurge: () => void
	onPurgeAll: () => void
	running: boolean
	daysLabel?: string
	purgeLabel?: string
	purgeAllLabel?: string
}) {
	const { t } = useLingui()
	const invalid = (Number(days) || 0) <= 0

	return (
		<div className="space-y-3">
			<div className="space-y-1">
				<h4 className="font-medium">{title}</h4>
				<p className="text-sm text-muted-foreground">{description}</p>
			</div>
			<div className="flex flex-col gap-3 sm:flex-row sm:items-end">
				<div className="grid gap-2 sm:max-w-xs w-full">
					<Label>{daysLabel || t`Days To Keep`}</Label>
					<Input type="number" min={1} value={days} onChange={(e) => onDaysChange(e.target.value)} />
				</div>
				<div className="flex flex-wrap gap-2">
					<Button disabled={invalid || running} onClick={onPurge}>
						{running && <Loader2Icon className="mr-2 size-4 animate-spin" />}
						{purgeLabel || t`Purge Older Records`}
					</Button>
					<Button variant="destructive" disabled={running} onClick={onPurgeAll}>
						<Trash2Icon className="mr-2 size-4" />
						{purgeAllLabel || t`Delete All`}
					</Button>
				</div>
			</div>
		</div>
	)
}

function RetentionSegment({ value, onChange }: { value: string; onChange: (value: string) => void }) {
	return (
		<div className="inline-flex w-fit rounded-lg border bg-muted/30 p-1">
			{RETENTION_OPTIONS.map((days) => {
				const selected = value === String(days)
				return (
					<button
						key={days}
						type="button"
						onClick={() => onChange(String(days))}
						className={cn(
							"rounded-md px-3 py-1.5 text-sm font-medium transition-colors",
							selected ? "bg-background text-foreground shadow-sm" : "text-muted-foreground hover:text-foreground"
						)}
					>
						{days}
					</button>
				)
			})}
		</div>
	)
}

export default PurgeSettingsPage
