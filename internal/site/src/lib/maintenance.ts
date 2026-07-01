import type { ChartBand } from "@/components/metric-charts"
import { pb } from "./api"

// Blue tint for maintenance chart bands (drawn under the red down / amber pending bands).
export const MAINTENANCE_BAND_COLOR = "rgba(59, 130, 246, 0.12)"

// One concrete maintenance interval covering a resource, as returned by
// /api/app/{monitors,hosts}/{id}/maintenance?range=.
export interface MaintenanceOccurrence {
	start: string
	end: string
	title: string
	severity: string
}

// fetchMaintenance loads the maintenance intervals covering a resource over `range`. Bands are
// a non-fatal chart overlay, so a failure resolves to [] — but it is logged (not silently
// swallowed) so a broken endpoint is distinguishable from "no windows" when diagnosing.
function fetchMaintenance(kind: "monitors" | "hosts", id: string, range: string): Promise<MaintenanceOccurrence[]> {
	return pb
		.send<MaintenanceOccurrence[]>(`/api/app/${kind}/${id}/maintenance?range=${range}`, { method: "GET" })
		.catch((err) => {
			console.warn(`failed to load ${kind} maintenance windows`, err)
			return [] as MaintenanceOccurrence[]
		})
}

export function fetchMonitorMaintenance(id: string, range: string): Promise<MaintenanceOccurrence[]> {
	return fetchMaintenance("monitors", id, range)
}

export function fetchHostMaintenance(id: string, range: string): Promise<MaintenanceOccurrence[]> {
	return fetchMaintenance("hosts", id, range)
}

// occurrenceBands converts maintenance occurrences into chart x-ranges (ms), dropping any
// with unparseable timestamps or non-positive width (so a malformed/degenerate interval never
// paints a spurious 1px band).
export function occurrenceBands(occurrences: MaintenanceOccurrence[]): ChartBand[] {
	return occurrences
		.map((o) => ({ start: Date.parse(o.start), end: Date.parse(o.end) }))
		.filter((b) => Number.isFinite(b.start) && Number.isFinite(b.end) && b.end > b.start)
}
