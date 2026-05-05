import { Plural, Trans, useLingui } from "@lingui/react/macro"
import { BoxesIcon } from "lucide-react"
import { memo, useEffect, useState } from "react"
import { PageHeader } from "@/components/page-header"
import Spinner from "@/components/spinner"
import { type ContainersFilters, defaultContainersFilters } from "./dashboard/containers-filter-sheet"
import { ContainersTable } from "./dashboard/containers-table"
import { useDashboardData } from "./dashboard/use-dashboard-data"

export default memo(function ContainersPage() {
	const { t } = useLingui()
	const { dashboard, loading } = useDashboardData()
	const [filters, setFilters] = useState<ContainersFilters>(defaultContainersFilters)

	useEffect(() => {
		document.title = `${t`Containers`} / Vigil`
	}, [t])

	if (loading) {
		return (
			<div className="flex min-h-72 items-center justify-center">
				<Spinner />
			</div>
		)
	}

	const containers = dashboard?.containers ?? []
	const running = containers.filter((container) => container.status === "running").length

	return (
		<div className="space-y-5 pb-10">
			<PageHeader
				icon={BoxesIcon}
				title={<Trans>Containers</Trans>}
				meta={
					<>
						<Plural value={containers.length} one="# container" other="# containers" /> · {running}/{containers.length}{" "}
						<Trans>running</Trans>
					</>
				}
			/>

			<ContainersTable containers={containers} filters={filters} onFiltersChange={setFilters} />
		</div>
	)
})
