export type MonitorType = "http" | "ping" | "tcp" | "dns" | "push"
export type MonitorStatus = -1 | 0 | 1 // -1=unknown, 0=down, 1=up

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
	// TCP
	hostname?: string
	port?: number
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
	hostname: string
	port: number | ""
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
	hostname: "",
	port: "",
	dns_host: "",
	dns_type: "A",
	dns_server: "",
}
