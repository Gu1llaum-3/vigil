import type { RecordModel } from "pocketbase"
import type { HourFormat } from "@/lib/enums"

// global window properties
declare global {
	var APP: {
		BASE_PATH: string
		DISPLAY_NAME: string
		HUB_VERSION: string
		HUB_URL: string
		OAUTH_DISABLE_POPUP: boolean
	}
}

export interface AgentRecord extends RecordModel {
	id: string
	name: string
	token: string
	fingerprint: string
	status: "pending" | "connected" | "offline"
	version: string
	last_seen: string
	capabilities: Record<string, unknown>
	metadata: Record<string, unknown>
	created_by: string
}

export interface AppInfo {
	key: string // public key
	v: string // version
	cu: boolean // check updates
}

export interface UpdateInfo {
	v: string // new version
	url: string // url to new version
}

export interface SemVer {
	major: number
	minor: number
	patch: number
}

export interface UserSettings {
	hourFormat?: HourFormat
	layoutWidth?: number
}

export type NotificationKind = "email" | "webhook" | "slack" | "teams" | "gchat" | "ntfy" | "gotify" | "in-app"

export interface NotificationChannel {
	id: string
	name: string
	kind: NotificationKind
	enabled: boolean
	config: Record<string, unknown>
	created: string
	updated: string
}

export interface NotificationRule {
	id: string
	name: string
	enabled: boolean
	events: string[]
	filter: Record<string, unknown> | null
	channels: string[]
	min_severity: string
	throttle_seconds: number
	created: string
	updated: string
}

export interface NotificationLog {
	id: string
	rule: string
	channel: string
	channel_kind?: NotificationKind
	created_by?: string
	event_kind: string
	resource_id: string
	resource_name?: string
	resource_type: string
	status: "sent" | "failed" | "throttled"
	error?: string
	payload_preview?: string
	sent_at: string
}

export type SystemNotificationCategory = "monitors" | "agents" | "container_images"

export interface SystemNotification {
	id: string
	event_kind: string
	category: SystemNotificationCategory
	severity: "info" | "warning" | "critical"
	resource_type: string
	resource_id: string
	resource_name?: string
	title: string
	message?: string
	payload?: Record<string, unknown>
	occurred_at: string
	read: boolean
}

export interface SystemNotificationsPage {
	items: SystemNotification[]
	page: number
	limit: number
	has_more: boolean
}

export interface SystemNotificationUnreadResponse {
	count: number
	items: SystemNotification[]
}

export interface SystemNotificationPreferences {
	enabled_categories: Record<SystemNotificationCategory, boolean>
}

export interface NotificationLogsPage {
	items: NotificationLog[]
	page: number
	limit: number
	has_more: boolean
}

export interface PurgeSettings {
	monitor_events_retention_days: number
	notification_logs_retention_days: number
	monitor_events_manual_default_days: number
	notification_logs_manual_default_days: number
	offline_agents_manual_default_days: number
}

export interface PurgeRunResponse {
	scope: "monitor_events" | "notification_logs" | "offline_agents"
	mode: "older_than_days" | "all"
	deleted_count: number
}

export interface ScheduledJobRecord {
	key: string
	label: string
	description: string
	schedule: string
	last_run_at: string
	last_success_at: string
	last_status: string
	last_error: string
	last_result?: Record<string, unknown>
	last_duration_ms: number
}
