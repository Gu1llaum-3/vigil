import { Trans, useLingui } from "@lingui/react/macro"
import { SlidersHorizontalIcon } from "lucide-react"
import { useId } from "react"
import { Button } from "@/components/ui/button"
import { Checkbox } from "@/components/ui/checkbox"
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
import { containerSeverity, isStoppedContainerStatus } from "@/lib/container-status"
import { cn } from "@/lib/utils"
import type { ContainerFleetEntry } from "@/lib/dashboard-types"

export type ContainersStatus = "all" | "running" | "stopped" | "restarting" | "paused" | "created"
export type ContainersSeverity = "error" | "warning"
export type ContainersImageAudit = "updates"

export type ContainersFilters = {
	status: ContainersStatus
	severity: Set<ContainersSeverity>
	imageAudit: Set<ContainersImageAudit>
}

export const defaultContainersFilters: ContainersFilters = {
	status: "all",
	severity: new Set(),
	imageAudit: new Set(),
}

export function countContainersFilters(f: ContainersFilters): number {
	return (f.status !== "all" ? 1 : 0) + f.severity.size + f.imageAudit.size
}

function containerHasUpdate(c: ContainerFleetEntry): boolean {
	const lineStatus = c.image_audit?.line_status || c.image_audit?.status
	return lineStatus === "patch_available" || lineStatus === "minor_available" || lineStatus === "update_available"
}

export function applyContainersFilters(
	containers: ContainerFleetEntry[],
	filters: ContainersFilters
): ContainerFleetEntry[] {
	return containers.filter((c) => {
		switch (filters.status) {
			case "running":
				if (c.status !== "running") return false
				break
			case "stopped":
				if (!isStoppedContainerStatus(c.status)) return false
				break
			case "restarting":
				if (c.status !== "restarting") return false
				break
			case "paused":
				if (c.status !== "paused") return false
				break
			case "created":
				if (c.status !== "created") return false
				break
		}

		if (filters.severity.size > 0) {
			const sev = containerSeverity(c)
			if (sev !== "error" && sev !== "warning") return false
			if (!filters.severity.has(sev)) return false
		}

		if (filters.imageAudit.has("updates") && !containerHasUpdate(c)) return false

		return true
	})
}

interface ContainersFilterSheetProps {
	filters: ContainersFilters
	onFiltersChange: (next: ContainersFilters) => void
}

export function ContainersFilterSheet({ filters, onFiltersChange }: ContainersFilterSheetProps) {
	const { t } = useLingui()
	const groupId = useId()
	const activeCount = countContainersFilters(filters)

	function setStatus(value: ContainersStatus) {
		onFiltersChange({ ...filters, status: value })
	}

	function toggleSeverity(flag: ContainersSeverity, checked: boolean) {
		const next = new Set(filters.severity)
		if (checked) next.add(flag)
		else next.delete(flag)
		onFiltersChange({ ...filters, severity: next })
	}

	function toggleImageAudit(flag: ContainersImageAudit, checked: boolean) {
		const next = new Set(filters.imageAudit)
		if (checked) next.add(flag)
		else next.delete(flag)
		onFiltersChange({ ...filters, imageAudit: next })
	}

	function reset() {
		onFiltersChange(defaultContainersFilters)
	}

	const severityOptions: Array<{ value: ContainersSeverity; label: string }> = [
		{ value: "error", label: t`Errors` },
		{ value: "warning", label: t`Warnings` },
	]

	return (
		<Sheet>
			<SheetTrigger asChild>
				<Button variant="outline" size="sm" className="gap-2">
					<SlidersHorizontalIcon className="size-4" />
					<Trans>Filters</Trans>
					{activeCount > 0 && (
						<span className="ml-1 inline-flex h-5 min-w-5 items-center justify-center rounded-full bg-primary px-1.5 text-[11px] font-semibold text-primary-foreground">
							{activeCount}
						</span>
					)}
				</Button>
			</SheetTrigger>
			<SheetContent side="right" className="flex flex-col gap-0 p-0">
				<SheetHeader className="border-b border-border/60 p-4">
					<SheetTitle>
						<Trans>Filter containers</Trans>
					</SheetTitle>
					<SheetDescription>
						<Trans>Combine multiple criteria to narrow down the table.</Trans>
					</SheetDescription>
				</SheetHeader>

				<div className="flex-1 space-y-6 overflow-y-auto p-4">
					<div className="space-y-2">
						<Label htmlFor={`${groupId}-status`}>
							<Trans>Status</Trans>
						</Label>
						<Select value={filters.status} onValueChange={(v) => setStatus(v as ContainersStatus)}>
							<SelectTrigger id={`${groupId}-status`}>
								<SelectValue />
							</SelectTrigger>
							<SelectContent>
								<SelectItem value="all">{t`All`}</SelectItem>
								<SelectItem value="running">{t`Running`}</SelectItem>
								<SelectItem value="stopped">{t`Stopped`}</SelectItem>
								<SelectItem value="restarting">{t`Restarting`}</SelectItem>
								<SelectItem value="paused">{t`Paused`}</SelectItem>
								<SelectItem value="created">{t`Created`}</SelectItem>
							</SelectContent>
						</Select>
					</div>

					<div className="space-y-3">
						<p className="text-sm font-medium leading-none">
							<Trans>Severity</Trans>
						</p>
						<div className="space-y-2">
							{severityOptions.map((opt) => {
								const id = `${groupId}-severity-${opt.value}`
								const checked = filters.severity.has(opt.value)
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
											onCheckedChange={(value) => toggleSeverity(opt.value, value === true)}
										/>
										<span className="text-sm">{opt.label}</span>
									</label>
								)
							})}
						</div>
					</div>

					<div className="space-y-3">
						<p className="text-sm font-medium leading-none">
							<Trans>Image audit</Trans>
						</p>
						<div className="space-y-2">
							<label
								htmlFor={`${groupId}-image-updates`}
								className={cn(
									"flex cursor-pointer items-center gap-3 rounded-md px-2 py-1.5 transition-colors hover:bg-accent/50",
									filters.imageAudit.has("updates") && "bg-accent/40"
								)}
							>
								<Checkbox
									id={`${groupId}-image-updates`}
									checked={filters.imageAudit.has("updates")}
									onCheckedChange={(value) => toggleImageAudit("updates", value === true)}
								/>
								<span className="text-sm">
									<Trans>Updates available</Trans>
								</span>
							</label>
						</div>
					</div>
				</div>

				<SheetFooter className="flex-row gap-2 border-t border-border/60 p-4">
					<Button variant="outline" className="flex-1" onClick={reset} disabled={activeCount === 0}>
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
