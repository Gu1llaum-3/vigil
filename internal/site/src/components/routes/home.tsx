import { Trans, useLingui } from "@lingui/react/macro"
import { RefreshCwIcon } from "lucide-react"
import { memo, useCallback, useEffect, useRef, useState } from "react"
import Spinner from "@/components/spinner"
import { Button } from "@/components/ui/button"
import { pb, isReadOnlyUser } from "@/lib/api"
import type { DashboardResponse } from "@/lib/dashboard-types"
import { Charts } from "./dashboard/charts"
import { ContainersTable } from "./dashboard/containers-table"
import { EmptyState } from "./dashboard/empty-state"
import { HostsTable } from "./dashboard/hosts-table"
import { KpiCards } from "./dashboard/kpi-cards"

export default memo(function Home() {
	const { t } = useLingui()
	const [dashboard, setDashboard] = useState<DashboardResponse | null>(null)
	const [loading, setLoading] = useState(true)
	const [refreshing, setRefreshing] = useState(false)
	const [activeFilter, setActiveFilter] = useState<string | null>(null)
	const snapshotDebounceRef = useRef<ReturnType<typeof setTimeout> | null>(null)
	const monitorDebounceRef = useRef<ReturnType<typeof setTimeout> | null>(null)

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

	const isEmpty = !dashboard || !dashboard.hosts || dashboard.hosts.length === 0
	if (isEmpty) {
		return (
			<div className="flex flex-1 py-6 sm:py-8">
				<div className="flex w-full flex-col">
					<EmptyState />
				</div>
			</div>
		)
	}

	const hasContainers = (dashboard.containers ?? []).length > 0

	return (
		<div className="flex flex-1 flex-col gap-6 py-6 sm:py-8">
			<div className="flex items-center justify-between">
				<h1 className="text-2xl font-semibold tracking-tight">
					<Trans>Patch Audit Dashboard</Trans>
				</h1>
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

			<KpiCards summary={dashboard.summary} activeFilter={activeFilter} onFilterChange={setActiveFilter} />

			<Charts summary={dashboard.summary} />

			<section className="space-y-3">
				<h2 className="text-sm font-medium text-muted-foreground uppercase tracking-wide">
					<Trans>Hosts</Trans>
				</h2>
				<HostsTable hosts={dashboard.hosts} activeFilter={activeFilter} onFilterChange={setActiveFilter} />
			</section>

			{hasContainers && (
				<section className="space-y-3">
					<h2 className="text-sm font-medium text-muted-foreground uppercase tracking-wide">
						<Trans>Container fleet</Trans>
					</h2>
					<ContainersTable containers={dashboard.containers} />
				</section>
			)}
		</div>
	)
})
