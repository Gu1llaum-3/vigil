import { Plural, Trans, useLingui } from "@lingui/react/macro"
import { GaugeIcon, RefreshCwIcon, ServerIcon } from "lucide-react"
import { memo, useEffect, useState } from "react"
import { MetricThresholds } from "@/components/metric-thresholds"
import { PageHeader } from "@/components/page-header"
import Spinner from "@/components/spinner"
import { Button } from "@/components/ui/button"
import { Sheet, SheetContent, SheetDescription, SheetHeader, SheetTitle, SheetTrigger } from "@/components/ui/sheet"
import { isAdmin, isReadOnlyUser, pb } from "@/lib/api"
import { type HostsFilters, defaultHostsFilters } from "./dashboard/hosts-filter-sheet"
import { HostsTable } from "./dashboard/hosts-table"
import { useHostsOverviewData } from "./dashboard/use-hosts-overview-data"

export default memo(function HostsPage() {
	const { t } = useLingui()
	const { hosts, loading, refetch } = useHostsOverviewData()
	const [filters, setFilters] = useState<HostsFilters>(defaultHostsFilters)
	const [refreshing, setRefreshing] = useState(false)

	useEffect(() => {
		document.title = `${t`Hosts`} / Vigil`
	}, [t])

	async function refreshSnapshots() {
		setRefreshing(true)
		try {
			await pb.send("/api/app/refresh-snapshots", { method: "POST" })
			await refetch()
		} finally {
			setRefreshing(false)
		}
	}

	if (loading) {
		return (
			<div className="flex min-h-72 items-center justify-center">
				<Spinner />
			</div>
		)
	}

	const connected = hosts.filter((host) => host.status === "connected").length

	return (
		<div className="space-y-5 pb-10">
			<PageHeader
				icon={ServerIcon}
				title={<Trans>Hosts</Trans>}
				meta={
					<>
						<Plural value={hosts.length} one="# host" other="# hosts" /> · {connected}/{hosts.length}{" "}
						<Trans>connected</Trans>
					</>
				}
					actions={
						<div className="flex flex-wrap gap-2">
							{isAdmin() && (
								<Sheet>
									<SheetTrigger asChild>
										<Button variant="outline" className="gap-2">
											<GaugeIcon className="size-4" />
											<Trans>Alert thresholds</Trans>
										</Button>
									</SheetTrigger>
									<SheetContent className="w-full overflow-y-auto sm:max-w-lg">
										<SheetHeader>
											<SheetTitle>
												<Trans>Global alert thresholds</Trans>
											</SheetTitle>
											<SheetDescription>
												<Trans>
													Default CPU / memory / disk / load thresholds for every host. Override per host from a host's
													page. Create a notification rule for the "host.metric_exceeded" event to be notified.
												</Trans>
											</SheetDescription>
										</SheetHeader>
										<div className="mt-4">
											<MetricThresholds agentId="" />
										</div>
									</SheetContent>
								</Sheet>
							)}
							<Button
								variant="outline"
								disabled={refreshing || isReadOnlyUser()}
								onClick={refreshSnapshots}
								className="gap-2"
							>
								<RefreshCwIcon className={`size-4 ${refreshing ? "animate-spin" : ""}`} />
								<Trans>Refresh inventory</Trans>
							</Button>
						</div>
					}
			/>

			<HostsTable hosts={hosts} filters={filters} onFiltersChange={setFilters} />
		</div>
	)
})
