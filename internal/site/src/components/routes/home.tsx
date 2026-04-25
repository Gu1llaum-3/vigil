import { Trans, useLingui } from "@lingui/react/macro"
import { useStore } from "@nanostores/react"
import { RefreshCwIcon } from "lucide-react"
import { memo, useCallback, useEffect, useRef, useState } from "react"
import Spinner from "@/components/spinner"
import { Button } from "@/components/ui/button"
import { pb, isReadOnlyUser } from "@/lib/api"
import { isErrorContainer, isWarningContainer } from "@/lib/container-status"
import { HourFormat } from "@/lib/enums"
import { $userSettings } from "@/lib/stores"
import { currentHour12 } from "@/lib/utils"
import type { DashboardResponse } from "@/lib/dashboard-types"
import { Charts } from "./dashboard/charts"
import { type ContainersFilters, defaultContainersFilters } from "./dashboard/containers-filter-sheet"
import { ContainersTable } from "./dashboard/containers-table"
import { EmptyState } from "./dashboard/empty-state"
import { type HostsCompliance, type HostsFilters, defaultHostsFilters } from "./dashboard/hosts-filter-sheet"
import { HostsTable } from "./dashboard/hosts-table"
import { KpiCards } from "./dashboard/kpi-cards"

// KPI cards toggle a single Hosts facet preset. We keep their string-based contract
// (activeFilter / onFilterChange) and adapt to/from the multi-facet HostsFilters
// here so the cards stay decoupled from the filter shape.
//
// Note: the "outdated" KPI key has no equivalent in the existing filter model
// (the legacy chip switch didn't handle it either), so we preserve that no-op.
function kpiKeyToHostsFilters(key: string | null): HostsFilters {
	if (!key || key === "all") return defaultHostsFilters
	if (key === "security" || key === "reboot") {
		return { ...defaultHostsFilters, compliance: new Set<HostsCompliance>([key]) }
	}
	if (key === "docker") {
		return { ...defaultHostsFilters, features: new Set(["docker"]) }
	}
	return defaultHostsFilters
}

function deriveKpiKey(f: HostsFilters): string | null {
	if (f.connection !== "all") return null
	const complianceSize = f.compliance.size
	const featuresSize = f.features.size
	if (complianceSize === 0 && featuresSize === 0) return null
	if (complianceSize === 1 && featuresSize === 0) {
		if (f.compliance.has("security")) return "security"
		if (f.compliance.has("reboot")) return "reboot"
	}
	if (complianceSize === 0 && featuresSize === 1 && f.features.has("docker")) return "docker"
	return null
}

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
	const [hostsFilters, setHostsFilters] = useState<HostsFilters>(defaultHostsFilters)
	const [containersFilters, setContainersFilters] = useState<ContainersFilters>(defaultContainersFilters)
	const snapshotDebounceRef = useRef<ReturnType<typeof setTimeout> | null>(null)
	const monitorDebounceRef = useRef<ReturnType<typeof setTimeout> | null>(null)
	const containersSectionRef = useRef<HTMLElement | null>(null)

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

	const hasContainers = (dashboard.containers ?? []).length > 0
	const hasContainerErrors = (dashboard.containers ?? []).some(isErrorContainer)
	const hasContainerWarnings = (dashboard.containers ?? []).some(isWarningContainer)
	const hour12 = userSettings.hourFormat ? userSettings.hourFormat === HourFormat["12h"] : currentHour12()
	const lastRefreshAt = dashboard.hosts.reduce<string | null>((latest, host) => {
		if (!host.collected_at) {
			return latest
		}

		if (!latest || Date.parse(host.collected_at) > Date.parse(latest)) {
			return host.collected_at
		}

		return latest
	}, null)

	function handleContainersClick() {
		if (!hasContainers) return
		setContainersFilters(
			hasContainerErrors
				? { ...defaultContainersFilters, severity: new Set(["error"]) }
				: hasContainerWarnings
					? { ...defaultContainersFilters, severity: new Set(["warning"]) }
					: { ...defaultContainersFilters, status: "running" }
		)
		containersSectionRef.current?.scrollIntoView({ behavior: "smooth", block: "start" })
	}

	return (
		<div className="flex flex-1 flex-col gap-6 py-6 sm:py-8">
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
				activeFilter={deriveKpiKey(hostsFilters)}
				onFilterChange={(key) => setHostsFilters(kpiKeyToHostsFilters(key))}
				hasContainersSection={hasContainers}
				hasContainerWarnings={hasContainerWarnings}
				hasContainerErrors={hasContainerErrors}
				onContainersClick={handleContainersClick}
			/>

			<Charts summary={dashboard.summary} />

			<section className="space-y-3">
				<h2 className="text-sm font-medium text-muted-foreground uppercase tracking-wide">
					<Trans>Hosts</Trans>
				</h2>
				<HostsTable hosts={dashboard.hosts} filters={hostsFilters} onFiltersChange={setHostsFilters} />
			</section>

			{hasContainers && (
				<section ref={containersSectionRef} className="space-y-3">
					<h2 className="text-sm font-medium text-muted-foreground uppercase tracking-wide">
						<Trans>Container fleet</Trans>
					</h2>
					<ContainersTable
						containers={dashboard.containers}
						filters={containersFilters}
						onFiltersChange={setContainersFilters}
					/>
				</section>
			)}
		</div>
	)
})
