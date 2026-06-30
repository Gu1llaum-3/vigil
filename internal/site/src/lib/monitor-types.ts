export type MonitorType = "http" | "ping" | "tcp" | "dns" | "push"
// -1=unknown, 0=down, 1=up, 2=pending. A monitor's own aggregate `status` is only ever
// -1/0/1; 2=pending appears only on individual check events (monitor_events / recent_checks /
// transitions), marking a failed check still under the failure threshold.
export type MonitorStatus = -1 | 0 | 1 | 2

export interface MonitorRecord {
	id: string
	name: string
	type: MonitorType
	group: string
	active: boolean
	interval: number
	timeout: number
	// HTTP
	url?: string
	http_method?: string
	http_accepted_codes?: number[]
	keyword?: string
	keyword_invert?: boolean
	// inverted: treat a reachable target as the alert condition (flip up<->down)
	inverted?: boolean
	// TCP
	hostname?: string
	port?: number
	// Ping
	ping_count?: number
	ping_per_request_timeout?: number
	ping_ip_family?: "" | "ipv4" | "ipv6"
	// DNS
	dns_host?: string
	dns_type?: string
	dns_server?: string
	// Push
	push_token?: string
	push_url?: string
	failure_threshold: number
	// Status
	status: MonitorStatus
	last_checked_at: string
	last_latency_ms: number
	last_msg: string
	avg_latency_24h_ms?: number
	uptime_24h?: number
	uptime_30d?: number
	recent_checks?: { status: MonitorStatus; checked_at: string }[]
}

// Up/down only count a *confirmed* status (1=up, 0=down). A monitor that is
// unchecked, unknown, or still retrying below its failure threshold sits at
// status -1 and is rendered "Pending" — it counts as neither up nor down (same
// separation as Uptime Kuma's PENDING state). Shared so the monitors page, group
// sections, and the sidebar bell count all agree with the status badge.
export function isMonitorUp(m: MonitorRecord): boolean {
	return Boolean(m.last_checked_at) && m.status === 1
}
export function isMonitorDown(m: MonitorRecord): boolean {
	return Boolean(m.last_checked_at) && m.status === 0
}

export interface MonitorGroupResponse {
	id: string
	name: string
	weight: number
	monitors: MonitorRecord[]
}

export interface MonitorGroupRecord {
	id: string
	name: string
	weight: number
}

// Request body for POST /api/app/monitors/:id/move
export interface MonitorMovePayload {
	group: string
}

export interface MonitorEventRecord {
	id: string
	status: MonitorStatus
	latency_ms: number
	msg: string
	checked_at: string
}

export interface MonitorFormData {
	name: string
	type: MonitorType
	group: string
	active: boolean
	interval: number
	timeout: number
	failure_threshold: number
	url: string
	http_method: string
	keyword: string
	keyword_invert: boolean
	inverted: boolean
	hostname: string
	port: number | ""
	ping_count: number | ""
	ping_per_request_timeout: number | ""
	ping_ip_family: "" | "ipv4" | "ipv6"
	dns_host: string
	dns_type: string
	dns_server: string
}

export const defaultMonitorForm: MonitorFormData = {
	name: "",
	type: "http",
	group: "",
	active: true,
	interval: 60,
	timeout: 10,
	failure_threshold: 3,
	url: "",
	http_method: "GET",
	keyword: "",
	keyword_invert: false,
	inverted: false,
	hostname: "",
	port: "",
	ping_count: 1,
	ping_per_request_timeout: 2,
	ping_ip_family: "",
	dns_host: "",
	dns_type: "A",
	dns_server: "",
}
