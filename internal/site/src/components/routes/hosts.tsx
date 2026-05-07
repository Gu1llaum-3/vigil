import { Plural, Trans, useLingui } from "@lingui/react/macro"
import { RefreshCwIcon, ServerIcon } from "lucide-react"
import { memo, useEffect, useState } from "react"
import { PageHeader } from "@/components/page-header"
import Spinner from "@/components/spinner"
import { Button } from "@/components/ui/button"
import { isReadOnlyUser, pb } from "@/lib/api"
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
						<Button
							variant="outline"
							disabled={refreshing || isReadOnlyUser()}
							onClick={refreshSnapshots}
							className="gap-2"
						>
							<RefreshCwIcon className={`size-4 ${refreshing ? "animate-spin" : ""}`} />
							<Trans>Refresh inventory</Trans>
						</Button>
					}
			/>

			<HostsTable hosts={hosts} filters={filters} onFiltersChange={setFilters} />
		</div>
	)
})
