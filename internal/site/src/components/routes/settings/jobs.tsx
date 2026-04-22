import { Trans, useLingui } from "@lingui/react/macro"
import { redirectPage } from "@nanostores/router"
import { Loader2Icon, PlayIcon } from "lucide-react"
import { memo, useEffect, useState } from "react"
import { $router } from "@/components/router"
import { Button } from "@/components/ui/button"
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"
import { Separator } from "@/components/ui/separator"
import { toast } from "@/components/ui/use-toast"
import { isAdmin, pb } from "@/lib/api"
import type { ScheduledJobRecord } from "@/types"

async function apiGet<T>(path: string): Promise<T> {
	return pb.send(path, { method: "GET" }) as Promise<T>
}

async function apiPost<T>(path: string): Promise<T> {
	return pb.send(path, { method: "POST" }) as Promise<T>
}

const JobsSettingsPage = memo(() => {
	const { t } = useLingui()
	const admin = isAdmin()
	const [jobs, setJobs] = useState<ScheduledJobRecord[]>([])
	const [loading, setLoading] = useState(true)
	const [runningKey, setRunningKey] = useState<string | null>(null)

	useEffect(() => {
		if (!admin) {
			redirectPage($router, "settings", { name: "general" })
			return
		}
		apiGet<ScheduledJobRecord[]>("/api/app/jobs")
			.then(setJobs)
			.catch((error: unknown) => {
				toast({
					title: t`Failed to load jobs`,
					description: (error as Error).message,
					variant: "destructive",
				})
			})
			.finally(() => setLoading(false))
	}, [admin, t])

	if (!admin) return null

	const runNow = async (jobKey: string) => {
		setRunningKey(jobKey)
		try {
			const updated = await apiPost<ScheduledJobRecord>(`/api/app/jobs/${jobKey}/run`)
			setJobs((current) => current.map((job) => (job.key === jobKey ? updated : job)))
			toast({
				title: t`Job completed`,
				description: updated.last_error || t`The job ran successfully.`,
				variant: updated.last_status === "failed" ? "destructive" : "default",
			})
		} catch (error: unknown) {
			toast({
				title: t`Failed to run job`,
				description: (error as Error).message,
				variant: "destructive",
			})
		} finally {
			setRunningKey(null)
		}
	}

	return (
		<>
			<div>
				<h3 className="text-xl font-medium mb-1">
					<Trans>Jobs</Trans>
				</h3>
				<p className="text-sm text-muted-foreground leading-relaxed">
					<Trans>Review active scheduled jobs, their usual schedule, latest execution state, and run them manually.</Trans>
				</p>
			</div>
			<Separator className="my-4" />
			{loading ? (
				<div className="flex items-center gap-2 text-sm text-muted-foreground">
					<Loader2Icon className="size-4 animate-spin" />
					<Trans>Loading jobs…</Trans>
				</div>
			) : (
				<div className="space-y-4">
					{jobs.map((job) => (
						<Card key={job.key}>
							<CardHeader>
								<div className="flex flex-col gap-3 sm:flex-row sm:items-start sm:justify-between">
									<div className="space-y-1">
										<CardTitle>
											{job.key === "vigilAutoRetention" ? t`Automatic Retention` : job.label || job.key}
										</CardTitle>
										<CardDescription>
											{job.key === "vigilAutoRetention"
												? t`Deletes old probe and notification history based on retention settings.`
												: job.description || job.key}
										</CardDescription>
									</div>
									<Button variant="outline" disabled={runningKey === job.key} onClick={() => void runNow(job.key)}>
										{runningKey === job.key ? <Loader2Icon className="mr-2 size-4 animate-spin" /> : <PlayIcon className="mr-2 size-4" />}
										<Trans>Run Now</Trans>
									</Button>
								</div>
							</CardHeader>
							<CardContent className="space-y-3">
								<div className="grid gap-3 sm:grid-cols-2 xl:grid-cols-5">
									<StatusItem label={t`Schedule`} value={formatSchedule(job.schedule)} />
									<StatusItem label={t`Status`} value={job.last_status || t`Idle`} />
									<StatusItem label={t`Last Run`} value={formatDateTime(job.last_run_at)} />
									<StatusItem label={t`Last Success`} value={formatDateTime(job.last_success_at)} />
									<StatusItem label={t`Duration`} value={job.last_duration_ms > 0 ? `${job.last_duration_ms} ms` : t`Never`} />
								</div>
								{Object.keys(job.last_result || {}).length > 0 ? (
									<div className="rounded-md border bg-muted/20 px-3 py-2 text-sm">
										<p className="font-medium mb-1">
											<Trans>Last Result</Trans>
										</p>
										<pre className="whitespace-pre-wrap break-words text-xs">{JSON.stringify(job.last_result, null, 2)}</pre>
									</div>
								) : null}
								{job.last_error ? <p className="text-sm text-destructive">{job.last_error}</p> : null}
							</CardContent>
						</Card>
					))}
				</div>
			)}
		</>
	)
})

function StatusItem({ label, value }: { label: string; value: string }) {
	return (
		<div className="space-y-1 rounded-md border bg-background px-3 py-2">
			<p className="text-xs font-medium uppercase tracking-wide text-muted-foreground">{label}</p>
			<p className="text-sm">{value}</p>
		</div>
	)
}

function formatDateTime(value?: string) {
	if (!value) return "Never"
	const parsed = new Date(value)
	if (Number.isNaN(parsed.getTime())) return value
	return new Intl.DateTimeFormat(undefined, {
		year: "numeric",
		month: "2-digit",
		day: "2-digit",
		hour: "2-digit",
		minute: "2-digit",
		second: "2-digit",
		timeZoneName: "short",
	}).format(parsed)
}

function formatSchedule(cron: string) {
	return `${cron} (UTC)`
}

export default JobsSettingsPage
