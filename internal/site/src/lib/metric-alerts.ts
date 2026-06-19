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

/** UI metadata per metric. `unit` is appended to threshold values; loadavg is unitless. */
export const metricAlertInfo: Record<
	MetricAlertMetric,
	{ label: string; unit: string; hint: string; max: number; step: number }
> = {
	cpu: { label: "CPU usage", unit: "%", hint: "Average CPU utilization", max: 100, step: 1 },
	memory: { label: "Memory usage", unit: "%", hint: "Average memory utilization", max: 100, step: 1 },
	disk: { label: "Disk usage", unit: "%", hint: "Highest filesystem usage", max: 100, step: 1 },
	loadavg: { label: "Load average", unit: "", hint: "1-minute load (≈ number of cores)", max: 16, step: 0.5 },
}

export function emptyMetricAlert(agent: string, metric: MetricAlertMetric): MetricAlert {
	// loadavg is a small unitless number (≈ cores); a 5-point margin would exceed
	// typical thresholds and make alerts unrecoverable, so default it to 0.5.
	const hysteresis = metric === "loadavg" ? 0.5 : 5
	return { agent, metric, enabled: false, warning_value: 0, critical_value: 0, hysteresis }
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
