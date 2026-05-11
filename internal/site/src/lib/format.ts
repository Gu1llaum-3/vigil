export function formatStorageValue(value: number) {
	return value.toLocaleString(undefined, { minimumFractionDigits: 2, maximumFractionDigits: 2 })
}

export function formatBytes(bytes: number) {
	if (!bytes || bytes <= 0) return "-"
	const units = ["B", "KB", "MB", "GB", "TB"]
	let value = bytes
	let unit = 0
	while (value >= 1024 && unit < units.length - 1) {
		value /= 1024
		unit++
	}
	return `${formatStorageValue(value)} ${units[unit]}`
}

export function formatBytesCompact(bytes: number) {
	if (!bytes || bytes <= 0) return "0 B"
	const units = ["B", "KB", "MB", "GB", "TB"]
	let value = bytes
	let unit = 0
	while (value >= 1024 && unit < units.length - 1) {
		value /= 1024
		unit++
	}
	const digits = unit === 0 ? 0 : unit === 1 ? 1 : 2
	return `${value.toFixed(digits)} ${units[unit]}`
}

export function formatBytesPerSecond(bytesPerSecond: number) {
	if (!bytesPerSecond || bytesPerSecond <= 0) return "0 B/s"
	const units = ["B/s", "KB/s", "MB/s", "GB/s"]
	let value = bytesPerSecond
	let unit = 0
	while (value >= 1024 && unit < units.length - 1) {
		value /= 1024
		unit++
	}
	const digits = unit === 0 ? 0 : unit === 1 ? 1 : 2
	return `${value.toFixed(digits)} ${units[unit]}`
}

export function formatPercent(value?: number | null): string {
	if (value == null) return "—"
	return `${Math.round(value * 10) / 10}%`
}

export function formatRam(mb: number) {
	if (!mb || mb <= 0) return "-"
	return mb >= 1024 ? `${Math.round(mb / 1024)} GB` : `${Math.round(mb)} MB`
}

export function formatUptime(seconds: number) {
	if (!seconds || seconds <= 0) return "-"
	const days = Math.floor(seconds / 86400)
	const hours = Math.floor((seconds % 86400) / 3600)
	const minutes = Math.floor((seconds % 3600) / 60)
	if (days > 0) return `${days}d ${hours}h`
	if (hours > 0) return `${hours}h ${minutes}m`
	return `${minutes}m`
}

export function formatDateTime(value: string) {
	if (!value) return "-"
	const parsed = new Date(value)
	if (Number.isNaN(parsed.getTime())) return value
	return parsed.toLocaleString()
}

export function formatChartTime(value: number) {
	return new Intl.DateTimeFormat(undefined, {
		month: "short",
		day: "numeric",
		hour: "2-digit",
		minute: "2-digit",
	}).format(new Date(value))
}
