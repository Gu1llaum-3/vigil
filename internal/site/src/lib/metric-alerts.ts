import { pb } from "./api"

export const METRIC_ALERT_METRICS = ["cpu", "memory", "disk", "loadavg"] as const
export type MetricAlertMetric = (typeof METRIC_ALERT_METRICS)[number]

export interface MetricAlert {
	id?: string
	/** Empty = global default; otherwise the agent id this override applies to. */
	agent: string
	metric: MetricAlertMetric
	enabled: boolean
	warning_value: number
	critical_value: number
	hysteresis: number
}

/**
 * Non-translatable UI metadata per metric: `unit` is appended to threshold values
 * (loadavg is unitless), `max`/`step` bound the sliders, and `warning`/`critical` are
 * the defaults seeded when an alert is first enabled (so toggling it on yields a usable,
 * firing alert rather than a 0/0 no-op the backend rejects). Labels/hints are translated
 * in the component, not here.
 */
export const metricAlertInfo: Record<
	MetricAlertMetric,
	{ unit: string; max: number; step: number; warning: number; critical: number; hysteresis: number }
> = {
	cpu: { unit: "%", max: 100, step: 1, warning: 80, critical: 90, hysteresis: 5 },
	memory: { unit: "%", max: 100, step: 1, warning: 80, critical: 90, hysteresis: 5 },
	disk: { unit: "%", max: 100, step: 1, warning: 80, critical: 90, hysteresis: 5 },
	// loadavg is normalized to load-per-core hub-side, so the threshold is "load per CPU
	// core" (1.0 = fully utilized) and is comparable across hosts of any size.
	loadavg: { unit: "/core", max: 4, step: 0.25, warning: 1, critical: 2, hysteresis: 0.5 },
}

export function emptyMetricAlert(agent: string, metric: MetricAlertMetric): MetricAlert {
	// Seed sensible defaults so enabling the alert produces a working threshold rather
	// than a 0/0 no-op (which the backend now rejects).
	const info = metricAlertInfo[metric]
	return {
		agent,
		metric,
		enabled: false,
		warning_value: info.warning,
		critical_value: info.critical,
		hysteresis: info.hysteresis,
	}
}

export function getMetricAlerts(): Promise<MetricAlert[]> {
	return pb.send("/api/app/metric-alerts", { method: "GET" }) as Promise<MetricAlert[]>
}

export function upsertMetricAlert(alert: MetricAlert): Promise<MetricAlert> {
	return pb.send("/api/app/metric-alerts", {
		method: "PUT",
		body: JSON.stringify(alert),
		headers: { "Content-Type": "application/json" },
	}) as Promise<MetricAlert>
}

export async function deleteMetricAlert(id: string): Promise<void> {
	await pb.send(`/api/app/metric-alerts/${id}`, { method: "DELETE" })
}
