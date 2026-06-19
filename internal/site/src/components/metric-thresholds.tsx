import { Trans } from "@lingui/react/macro"
import { CpuIcon, GaugeIcon, HardDriveIcon, Loader2Icon, MemoryStickIcon } from "lucide-react"
import type { ComponentType } from "react"
import { useEffect, useState } from "react"
import Slider from "@/components/ui/slider"
import { Switch } from "@/components/ui/switch"
import { toast } from "@/components/ui/use-toast"
import { cn } from "@/lib/utils"
import {
	deleteMetricAlert,
	emptyMetricAlert,
	getMetricAlerts,
	type MetricAlert,
	type MetricAlertMetric,
	METRIC_ALERT_METRICS,
	metricAlertInfo,
	upsertMetricAlert,
} from "@/lib/metric-alerts"

type Forms = Record<MetricAlertMetric, MetricAlert>

const metricIcons: Record<MetricAlertMetric, ComponentType<{ className?: string }>> = {
	cpu: CpuIcon,
	memory: MemoryStickIcon,
	disk: HardDriveIcon,
	loadavg: GaugeIcon,
}

function buildForms(alerts: MetricAlert[], agentId: string): Forms {
	const forms = {} as Forms
	for (const metric of METRIC_ALERT_METRICS) {
		const override = alerts.find((a) => a.agent === agentId && a.metric === metric)
		if (override) {
			forms[metric] = { ...override }
			continue
		}
		// Per-host scope with no override yet: seed the card from the global default so
		// it reflects what this host currently inherits. id stays undefined, so saving
		// (toggle off → mute, or editing → override) creates the per-host row.
		if (agentId !== "") {
			const global = alerts.find((a) => a.agent === "" && a.metric === metric)
			forms[metric] = global ? { ...global, id: undefined, agent: agentId } : emptyMetricAlert(agentId, metric)
			continue
		}
		forms[metric] = emptyMetricAlert(agentId, metric)
	}
	return forms
}

/**
 * MetricThresholds edits host metric-alert thresholds for a scope: agentId="" is
 * the global default; a real agent id edits that host's override. Beszel-style
 * cards with a toggle + inline-value sliders; changes auto-save on commit.
 */
export function MetricThresholds({ agentId = "" }: { agentId?: string }) {
	const [forms, setForms] = useState<Forms | null>(null)

	const reload = () => {
		getMetricAlerts()
			.then((alerts) => setForms(buildForms(alerts ?? [], agentId)))
			.catch(() => setForms(buildForms([], agentId)))
	}

	useEffect(reload, [agentId])

	if (!forms) {
		return (
			<div className="flex h-24 items-center justify-center text-muted-foreground">
				<Loader2Icon className="h-5 w-5 animate-spin" />
			</div>
		)
	}

	const patch = (metric: MetricAlertMetric, p: Partial<MetricAlert>) =>
		setForms((prev) => (prev ? { ...prev, [metric]: { ...prev[metric], ...p } } : prev))

	const save = async (metric: MetricAlertMetric, override?: Partial<MetricAlert>) => {
		const form = { ...forms[metric], ...override, agent: agentId }
		try {
			const saved = await upsertMetricAlert(form)
			patch(metric, saved)
		} catch (e) {
			toast({ title: "Failed to save threshold", description: String(e), variant: "destructive" })
		}
	}

	const resetToGlobal = async (metric: MetricAlertMetric) => {
		const id = forms[metric].id
		if (!id) return
		try {
			await deleteMetricAlert(id)
			patch(metric, emptyMetricAlert(agentId, metric))
			toast({ title: "Reverted to global default" })
		} catch (e) {
			toast({ title: "Failed to reset", description: String(e), variant: "destructive" })
		}
	}

	return (
		<div className="grid gap-3">
			{METRIC_ALERT_METRICS.map((metric) => {
				const form = forms[metric]
				const info = metricAlertInfo[metric]
				const Icon = metricIcons[metric]
				const perHost = agentId !== ""
				const hasOverride = perHost && Boolean(form.id)
				return (
					<div
						key={metric}
						className="rounded-lg border border-muted-foreground/15 transition-colors hover:border-muted-foreground/25"
					>
						<div className={cn("flex items-center justify-between gap-4 p-4", { "pb-0": form.enabled })}>
							<div className="grid select-none gap-1">
								<p className="flex items-center gap-3 font-semibold">
									<Icon className="h-4 w-4 opacity-85" /> {info.label}
								</p>
								{!form.enabled && <span className="text-sm text-muted-foreground">{info.hint}</span>}
							</div>
							<Switch
								checked={form.enabled}
								onCheckedChange={(enabled) => {
									patch(metric, { enabled })
									save(metric, { enabled })
								}}
							/>
						</div>

						{form.enabled && (
							<div className="grid gap-5 px-4 pb-5 pt-2 tabular-nums text-muted-foreground sm:grid-cols-3">
								<ThresholdSlider
									label={<Trans>Warning above</Trans>}
									value={form.warning_value}
									unit={info.unit}
									max={info.max}
									step={info.step}
									onChange={(v) => patch(metric, { warning_value: v })}
									onCommit={(v) => save(metric, { warning_value: v })}
								/>
								<ThresholdSlider
									label={<Trans>Critical above</Trans>}
									value={form.critical_value}
									unit={info.unit}
									max={info.max}
									step={info.step}
									onChange={(v) => patch(metric, { critical_value: v })}
									onCommit={(v) => save(metric, { critical_value: v })}
								/>
								<ThresholdSlider
									label={<Trans>Resolve margin</Trans>}
									value={form.hysteresis}
									unit={info.unit}
									// Must stay below the threshold or the alert can never recover, so cap
									// the slider one step under the active (warning, else critical) threshold.
									max={Math.max(
										info.step,
										(form.warning_value || form.critical_value || info.max) - info.step,
									)}
									step={info.step}
									onChange={(v) => patch(metric, { hysteresis: v })}
									onCommit={(v) => save(metric, { hysteresis: v })}
								/>
							</div>
						)}

						{perHost && !hasOverride && (
							<div className="border-t border-muted-foreground/10 px-4 py-2 text-xs text-muted-foreground">
								<Trans>Inherits the global default</Trans>
							</div>
						)}
						{hasOverride && (
							<div className="flex items-center justify-between gap-2 border-t border-muted-foreground/10 px-4 py-2 text-xs text-muted-foreground">
								<span>
									{form.enabled ? (
										<Trans>Overrides the global default</Trans>
									) : (
										<Trans>Alerts muted for this host</Trans>
									)}
								</span>
								<button type="button" className="underline hover:text-foreground" onClick={() => resetToGlobal(metric)}>
									<Trans>Reset to global</Trans>
								</button>
							</div>
						)}
					</div>
				)
			})}
		</div>
	)
}

function ThresholdSlider({
	label,
	value,
	unit,
	max,
	step,
	onChange,
	onCommit,
}: {
	label: React.ReactNode
	value: number
	unit: string
	max: number
	step: number
	onChange: (v: number) => void
	onCommit: (v: number) => void
}) {
	return (
		<div>
			<p className="mb-2 flex min-h-9 items-start text-sm leading-tight">
				<span>
					{label}{" "}
					<strong className="text-foreground">
						{value}
						{unit}
					</strong>
				</span>
			</p>
			<Slider
				value={[value]}
				min={0}
				max={max}
				step={step}
				onValueChange={(v) => onChange(v[0])}
				onValueCommit={(v) => onCommit(v[0])}
			/>
		</div>
	)
}
