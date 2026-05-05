import { Trans, useLingui } from "@lingui/react/macro"
import { useStore } from "@nanostores/react"
import { getPagePath } from "@nanostores/router"
import { RefreshCwIcon } from "lucide-react"
import { memo, useCallback, useEffect, useRef, useState } from "react"
import { $router, Link } from "@/components/router"
import Spinner from "@/components/spinner"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card"
import { pb, isReadOnlyUser } from "@/lib/api"
import { isErrorContainer, isWarningContainer } from "@/lib/container-status"
import { HourFormat } from "@/lib/enums"
import { $userSettings } from "@/lib/stores"
import { currentHour12 } from "@/lib/utils"
import type { DashboardResponse } from "@/lib/dashboard-types"
import { Charts } from "./dashboard/charts"
import { EmptyState } from "./dashboard/empty-state"
import { KpiCards } from "./dashboard/kpi-cards"

function formatRefreshDateTime(value: string, hour12: boolean) {
	const parsed = new Date(value)
	if (Number.isNaN(parsed.getTime())) {
		return "-"
	}

	return new Intl.DateTimeFormat(undefined, {
		dateStyle: "medium",
		timeStyle: "short",
		hour12,
	}).format(parsed)
}

export default memo(function Home() {
	const { t } = useLingui()
	const userSettings = useStore($userSettings)
	const [dashboard, setDashboard] = useState<DashboardResponse | null>(null)
	const [loading, setLoading] = useState(true)
	const [refreshing, setRefreshing] = useState(false)
	const snapshotDebounceRef = useRef<ReturnType<typeof setTimeout> | null>(null)
	const monitorDebounceRef = useRef<ReturnType<typeof setTimeout> | null>(null)
	const auditDebounceRef = useRef<ReturnType<typeof setTimeout> | null>(null)

	const fetchDashboard = useCallback(async () => {
		try {
			const data = await pb.send<DashboardResponse>("/api/app/dashboard", { method: "GET" })
			setDashboard(data)
		} catch (e) {
			console.error("dashboard fetch failed", e)
		} finally {
			setLoading(false)
		}
	}, [])

	useEffect(() => {
		document.title = `${t`Dashboard`} / Vigil`
		fetchDashboard()
	}, [t, fetchDashboard])

	// Realtime agent status updates
	useEffect(() => {
		let unsubscribe: (() => void) | undefined
		;(async () => {
			unsubscribe = await pb.collection("agents").subscribe("*", (res) => {
				setDashboard((current) => {
					if (!current) return current
					const updatedHosts = current.hosts.map((host) => {
						if (host.id === res.record.id) {
							return { ...host, status: res.record.status as string }
						}
						return host
					})
					const connectedHosts = updatedHosts.filter((h) => h.status === "connected").length
					return {
						...current,
						hosts: updatedHosts,
						summary: {
							...current.summary,
							connected_hosts: connectedHosts,
							offline_hosts: updatedHosts.length - connectedHosts,
						},
					}
				})
			})
		})()
		return () => unsubscribe?.()
	}, [])

	// Realtime snapshot updates — re-fetch dashboard when any snapshot changes.
	// Debounced to avoid N re-fetches when the backend ticker updates N agents simultaneously.
	useEffect(() => {
		let unsubscribe: (() => void) | undefined
		;(async () => {
			unsubscribe = await pb.collection("host_snapshots").subscribe("*", () => {
				if (snapshotDebounceRef.current) {
					clearTimeout(snapshotDebounceRef.current)
				}
				snapshotDebounceRef.current = setTimeout(() => {
					fetchDashboard()
				}, 1000)
			})
		})()
		return () => {
			unsubscribe?.()
			if (snapshotDebounceRef.current) {
				clearTimeout(snapshotDebounceRef.current)
			}
		}
	}, [fetchDashboard])

	// Realtime monitor updates — re-fetch dashboard to keep the monitor KPI card fresh.
	useEffect(() => {
		let unsubscribe: (() => void) | undefined
		;(async () => {
			unsubscribe = await pb.collection("monitors").subscribe("*", () => {
				if (monitorDebounceRef.current) {
					clearTimeout(monitorDebounceRef.current)
				}
				monitorDebounceRef.current = setTimeout(() => {
					fetchDashboard()
				}, 1000)
			})
		})()
		return () => {
			unsubscribe?.()
			if (monitorDebounceRef.current) {
				clearTimeout(monitorDebounceRef.current)
			}
		}
	}, [fetchDashboard])

	// Realtime image audit updates — re-fetch dashboard so the container table
	// reflects new audit results (after "Check images now" or an override
	// change). Heavily debounced because the audit job rewrites every record.
	useEffect(() => {
		let unsubscribe: (() => void) | undefined
		;(async () => {
			unsubscribe = await pb.collection("container_image_audits").subscribe("*", () => {
				if (auditDebounceRef.current) {
					clearTimeout(auditDebounceRef.current)
				}
				auditDebounceRef.current = setTimeout(() => {
					fetchDashboard()
				}, 1500)
			})
		})()
		return () => {
			unsubscribe?.()
			if (auditDebounceRef.current) {
				clearTimeout(auditDebounceRef.current)
			}
		}
	}, [fetchDashboard])

	const handleRefresh = useCallback(async () => {
		setRefreshing(true)
		try {
			await pb.send("/api/app/refresh-snapshots", { method: "POST" })
			await fetchDashboard()
		} catch (e) {
			console.error("refresh failed", e)
		} finally {
			setRefreshing(false)
		}
	}, [fetchDashboard])

	if (loading) {
		return (
			<div className="flex flex-1 items-center justify-center">
				<Spinner />
			</div>
		)
	}

	const isEmpty = !dashboard?.hosts || dashboard.hosts.length === 0
	if (isEmpty) {
		return (
			<div className="flex flex-1 py-6 sm:py-8">
				<div className="flex w-full flex-col">
					<EmptyState />
				</div>
			</div>
		)
	}

	const hasContainerErrors = (dashboard.containers ?? []).some(isErrorContainer)
	const hasContainerWarnings = (dashboard.containers ?? []).some(isWarningContainer)
	const hostRisks = dashboard.hosts
		.filter(
			(host) =>
				host.status !== "connected" ||
				host.reboot?.required ||
				(host.packages?.security_count ?? 0) > 0 ||
				((host.packages?.outdated_count ?? 0) > 0 && (host.packages?.last_upgrade_age_days ?? 0) > 30)
		)
		.slice(0, 6)
	const containerRisks = (dashboard.containers ?? [])
		.filter(
			(container) =>
				isErrorContainer(container) ||
				isWarningContainer(container) ||
				container.image_audit?.status === "update_available" ||
				container.image_audit?.status === "check_failed"
		)
		.slice(0, 6)
	const hour12 = userSettings.hourFormat ? userSettings.hourFormat === HourFormat["12h"] : Boolean(currentHour12())
	const lastRefreshAt = dashboard.hosts.reduce<string | null>((latest, host) => {
		if (!host.collected_at) {
			return latest
		}

		if (!latest || Date.parse(host.collected_at) > Date.parse(latest)) {
			return host.collected_at
		}

		return latest
	}, null)

	return (
		<div className="flex flex-1 flex-col gap-6">
			<div className="flex flex-wrap items-center justify-between gap-3">
				<h1 className="text-2xl font-semibold tracking-tight">
					<Trans>Dashboard</Trans>
				</h1>
				<div className="ml-auto flex flex-wrap items-center justify-end gap-3 sm:gap-4">
					{lastRefreshAt ? (
						<p className="text-right text-xs text-muted-foreground sm:text-sm">
							<Trans>Last refresh</Trans>
							{": "}
							{formatRefreshDateTime(lastRefreshAt, hour12)}
						</p>
					) : null}
					<Button
						variant="outline"
						size="sm"
						disabled={refreshing || isReadOnlyUser()}
						onClick={handleRefresh}
						className="gap-2"
					>
						<RefreshCwIcon className={`size-4 ${refreshing ? "animate-spin" : ""}`} />
						<Trans>Refresh</Trans>
					</Button>
				</div>
			</div>

			<KpiCards
				summary={dashboard.summary}
				hasContainerWarnings={hasContainerWarnings}
				hasContainerErrors={hasContainerErrors}
			/>

			<Charts summary={dashboard.summary} />

			<div className="grid gap-4 lg:grid-cols-2">
				<AttentionCard
					title={<Trans>Hosts needing attention</Trans>}
					href={getPagePath($router, "hosts")}
					empty={<Trans>No host needs attention.</Trans>}
				>
					{hostRisks.map((host) => (
						<Link
							key={host.id}
							href={getPagePath($router, "host", { id: host.id })}
							className="flex items-center justify-between gap-3 rounded-md border border-border/60 p-3 hover:bg-accent/40"
						>
							<div className="min-w-0">
								<div className="truncate text-sm font-medium">{host.name || host.hostname || host.id}</div>
								<div className="truncate text-xs text-muted-foreground">
									{host.primary_ip || host.hostname || host.id}
								</div>
							</div>
							<div className="flex shrink-0 flex-wrap justify-end gap-1">
								{host.status !== "connected" && (
									<Badge variant="outline" className="text-[10px]">
										<Trans>Offline</Trans>
									</Badge>
								)}
								{host.reboot?.required && (
									<Badge variant="danger" className="text-[10px]">
										<Trans>Reboot</Trans>
									</Badge>
								)}
								{(host.packages?.security_count ?? 0) > 0 && (
									<Badge variant="danger" className="text-[10px]">
										{host.packages.security_count} <Trans>security</Trans>
									</Badge>
								)}
							</div>
						</Link>
					))}
				</AttentionCard>

				<AttentionCard
					title={<Trans>Containers and images</Trans>}
					href={getPagePath($router, "containers")}
					empty={<Trans>No container issue detected.</Trans>}
				>
					{containerRisks.map((container) => (
						<Link
							key={`${container.host_id}-${container.id}`}
							href={getPagePath($router, "host", { id: container.host_id })}
							className="flex items-center justify-between gap-3 rounded-md border border-border/60 p-3 hover:bg-accent/40"
						>
							<div className="min-w-0">
								<div className="truncate text-sm font-medium">{container.name || container.id}</div>
								<div className="truncate text-xs text-muted-foreground">{container.host_name || container.host_id}</div>
							</div>
							<div className="flex shrink-0 flex-wrap justify-end gap-1">
								{isErrorContainer(container) && (
									<Badge variant="danger" className="text-[10px]">
										<Trans>Error</Trans>
									</Badge>
								)}
								{isWarningContainer(container) && (
									<Badge variant="warning" className="text-[10px]">
										<Trans>Warning</Trans>
									</Badge>
								)}
								{container.image_audit?.status === "update_available" && (
									<Badge variant="warning" className="text-[10px]">
										<Trans>Image update</Trans>
									</Badge>
								)}
								{container.image_audit?.status === "check_failed" && (
									<Badge variant="danger" className="text-[10px]">
										<Trans>Audit failed</Trans>
									</Badge>
								)}
							</div>
						</Link>
					))}
				</AttentionCard>
			</div>
		</div>
	)
})

function AttentionCard({
	title,
	href,
	empty,
	children,
}: {
	title: React.ReactNode
	href: string
	empty: React.ReactNode
	children: React.ReactNode[]
}) {
	return (
		<Card>
			<CardHeader className="flex flex-row items-center justify-between gap-3 pb-3">
				<CardTitle className="text-base">{title}</CardTitle>
				<Link href={href} className="text-xs font-medium text-muted-foreground hover:text-foreground hover:underline">
					<Trans>View all</Trans>
				</Link>
			</CardHeader>
			<CardContent className="space-y-2">
				{children.length > 0 ? (
					children
				) : (
					<div className="rounded-md border border-dashed border-border/60 p-6 text-center text-sm text-muted-foreground">
						{empty}
					</div>
				)}
			</CardContent>
		</Card>
	)
}
