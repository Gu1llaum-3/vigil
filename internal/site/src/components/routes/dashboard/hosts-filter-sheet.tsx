import { Trans, useLingui } from "@lingui/react/macro"
import { SearchIcon, SlidersHorizontalIcon } from "lucide-react"
import { useId } from "react"
import { Button } from "@/components/ui/button"
import { Checkbox } from "@/components/ui/checkbox"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"
import {
	Sheet,
	SheetClose,
	SheetContent,
	SheetDescription,
	SheetFooter,
	SheetHeader,
	SheetTitle,
	SheetTrigger,
} from "@/components/ui/sheet"
import { cn } from "@/lib/utils"
import type { HostsOverviewRecord } from "@/lib/dashboard-types"

export type HostsConnection = "all" | "connected" | "offline"
export type HostsCompliance = "security" | "reboot" | "stale" | "unknown" | "clean"
export type HostsFeature = "docker"

export type HostsFilters = {
	connection: HostsConnection
	compliance: Set<HostsCompliance>
	features: Set<HostsFeature>
}

export const defaultHostsFilters: HostsFilters = {
	connection: "all",
	compliance: new Set(),
	features: new Set(),
}

export function countHostsFilters(f: HostsFilters): number {
	return (f.connection !== "all" ? 1 : 0) + f.compliance.size + f.features.size
}

function hostMatchesCompliance(h: HostsOverviewRecord, flag: HostsCompliance): boolean {
	const security = h.packages?.security_count ?? 0
	const outdated = h.packages?.outdated_count ?? 0
	const lastUpgradeAge = h.packages?.last_upgrade_age_days ?? 0
	const lastUpgradeKnown = h.packages?.last_upgrade_known ?? false
	const isStale = outdated > 0 && lastUpgradeAge > 30
	const isUnknown = outdated > 0 && !lastUpgradeKnown
	switch (flag) {
		case "security":
			return security > 0
		case "reboot":
			return Boolean(h.reboot?.required)
		case "stale":
			return isStale
		case "unknown":
			return isUnknown
		case "clean":
			return !h.reboot?.required && security === 0 && !isStale && !isUnknown
	}
}

export function applyHostsFilters(hosts: HostsOverviewRecord[], filters: HostsFilters): HostsOverviewRecord[] {
	return hosts.filter((h) => {
		if (filters.connection === "connected" && h.status !== "connected") return false
		if (filters.connection === "offline" && h.status === "connected") return false

		if (filters.compliance.size > 0) {
			let matchesAny = false
			for (const flag of filters.compliance) {
				if (hostMatchesCompliance(h, flag)) {
					matchesAny = true
					break
				}
			}
			if (!matchesAny) return false
		}

		if (filters.features.has("docker") && h.docker?.state !== "available") return false

		return true
	})
}

interface HostsFilterSheetProps {
	filters: HostsFilters
	onFiltersChange: (next: HostsFilters) => void
	search: string
	onSearchChange: (value: string) => void
}

export function HostsFilterSheet({ filters, onFiltersChange, search, onSearchChange }: HostsFilterSheetProps) {
	const { t } = useLingui()
	const groupId = useId()
	const activeCount = countHostsFilters(filters)
	const totalCount = activeCount + (search ? 1 : 0)

	function setConnection(value: HostsConnection) {
		onFiltersChange({ ...filters, connection: value })
	}

	function toggleCompliance(flag: HostsCompliance, checked: boolean) {
		const next = new Set(filters.compliance)
		if (checked) next.add(flag)
		else next.delete(flag)
		onFiltersChange({ ...filters, compliance: next })
	}

	function toggleFeature(flag: HostsFeature, checked: boolean) {
		const next = new Set(filters.features)
		if (checked) next.add(flag)
		else next.delete(flag)
		onFiltersChange({ ...filters, features: next })
	}

	function reset() {
		onFiltersChange(defaultHostsFilters)
		onSearchChange("")
	}

	const complianceOptions: Array<{ value: HostsCompliance; label: string }> = [
		{ value: "security", label: t`Security` },
		{ value: "reboot", label: t`Reboot req.` },
		{ value: "stale", label: t`Out of SLA` },
		{ value: "unknown", label: t`Unknown` },
		{ value: "clean", label: t`Compliant` },
	]

	return (
		<Sheet>
			<SheetTrigger asChild>
				<Button variant="outline" size="sm" className="gap-2">
					<SlidersHorizontalIcon className="size-4" />
					<Trans>Filters</Trans>
					{totalCount > 0 && (
						<span className="ml-1 inline-flex h-5 min-w-5 items-center justify-center rounded-full bg-primary px-1.5 text-[11px] font-semibold text-primary-foreground">
							{totalCount}
						</span>
					)}
				</Button>
			</SheetTrigger>
			<SheetContent side="right" className="flex flex-col gap-0 p-0">
				<SheetHeader className="border-b border-border/60 p-4">
					<SheetTitle>
						<Trans>Filter hosts</Trans>
					</SheetTitle>
					<SheetDescription>
						<Trans>Combine multiple criteria to narrow down the table.</Trans>
					</SheetDescription>
				</SheetHeader>

				<div className="flex-1 space-y-6 overflow-y-auto p-4">
					<div className="space-y-2">
						<Label htmlFor={`${groupId}-search`}>
							<Trans>Search</Trans>
						</Label>
						<div className="relative">
							<SearchIcon className="pointer-events-none absolute left-3 top-1/2 size-4 -translate-y-1/2 text-muted-foreground" />
							<Input
								id={`${groupId}-search`}
								value={search}
								onChange={(e) => onSearchChange(e.target.value)}
								placeholder={t`Host, IP, OS, kernel…`}
								className="pl-9"
							/>
						</div>
					</div>

					<div className="space-y-2">
						<Label htmlFor={`${groupId}-connection`}>
							<Trans>Connection</Trans>
						</Label>
						<Select value={filters.connection} onValueChange={(v) => setConnection(v as HostsConnection)}>
							<SelectTrigger id={`${groupId}-connection`}>
								<SelectValue />
							</SelectTrigger>
							<SelectContent>
								<SelectItem value="all">{t`All`}</SelectItem>
								<SelectItem value="connected">{t`Online`}</SelectItem>
								<SelectItem value="offline">{t`Offline`}</SelectItem>
							</SelectContent>
						</Select>
					</div>

					<div className="space-y-3">
						<p className="text-sm font-medium leading-none">
							<Trans>Compliance</Trans>
						</p>
						<div className="space-y-2">
							{complianceOptions.map((opt) => {
								const id = `${groupId}-compliance-${opt.value}`
								const checked = filters.compliance.has(opt.value)
								return (
									<label
										key={opt.value}
										htmlFor={id}
										className={cn(
											"flex cursor-pointer items-center gap-3 rounded-md px-2 py-1.5 transition-colors hover:bg-accent/50",
											checked && "bg-accent/40"
										)}
									>
										<Checkbox
											id={id}
											checked={checked}
											onCheckedChange={(value) => toggleCompliance(opt.value, value === true)}
										/>
										<span className="text-sm">{opt.label}</span>
									</label>
								)
							})}
						</div>
					</div>

					<div className="space-y-3">
						<p className="text-sm font-medium leading-none">
							<Trans>Features</Trans>
						</p>
						<div className="space-y-2">
							<label
								htmlFor={`${groupId}-feature-docker`}
								className={cn(
									"flex cursor-pointer items-center gap-3 rounded-md px-2 py-1.5 transition-colors hover:bg-accent/50",
									filters.features.has("docker") && "bg-accent/40"
								)}
							>
								<Checkbox
									id={`${groupId}-feature-docker`}
									checked={filters.features.has("docker")}
									onCheckedChange={(value) => toggleFeature("docker", value === true)}
								/>
								<span className="text-sm">
									<Trans>Docker</Trans>
								</span>
							</label>
						</div>
					</div>
				</div>

				<SheetFooter className="flex-row gap-2 border-t border-border/60 p-4">
					<Button variant="outline" className="flex-1" onClick={reset} disabled={totalCount === 0}>
						<Trans>Reset</Trans>
					</Button>
					<SheetClose asChild>
						<Button className="flex-1">
							<Trans>Apply</Trans>
						</Button>
					</SheetClose>
				</SheetFooter>
			</SheetContent>
		</Sheet>
	)
}
