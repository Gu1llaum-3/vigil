import { t } from "@lingui/core/macro"
import { useEffect, useRef } from "react"
import { toast } from "@/components/ui/use-toast"
import { isAdmin, pb } from "@/lib/api"
import type { NotificationLog } from "@/types"

const ALERT_EVENT_KINDS = new Set(["monitor.down", "monitor.up", "agent.offline", "agent.online"])
const DEDUPE_WINDOW_MS = 10_000
const ALERT_DURATION_MS = 15_000

function getToastDescription(log: NotificationLog) {
	if (log.status === "failed") {
		return (
			log.error ||
			log.payload_preview ||
			`${log.event_kind} (${log.resource_type}:${log.resource_name || log.resource_id})`
		)
	}
	return log.payload_preview || `${log.event_kind} (${log.resource_type}:${log.resource_name || log.resource_id})`
}

function getAlertToast(log: NotificationLog) {
	const name = log.resource_name || log.resource_id
	if (log.event_kind === "monitor.down") {
		return {
			title: t`Monitor down`,
			description: t`${name} just went down.`,
			variant: "destructive" as const,
			duration: ALERT_DURATION_MS,
		}
	}
	if (log.event_kind === "monitor.up") {
		return {
			title: t`Monitor recovered`,
			description: t`${name} is back up.`,
			variant: "success" as const,
			duration: ALERT_DURATION_MS,
		}
	}
	if (log.event_kind === "agent.online") {
		return {
			title: t`Host back online`,
			description: t`${name} is back online.`,
			variant: "success" as const,
			duration: ALERT_DURATION_MS,
		}
	}
	return {
		title: t`Host offline`,
		description: t`${name} just went offline.`,
		variant: "destructive" as const,
		duration: ALERT_DURATION_MS,
	}
}

export default function NotificationLogToasts() {
	const currentUserId = pb.authStore.record?.id
	const recentAlertKeys = useRef<Map<string, number>>(new Map())

	useEffect(() => {
		if (!currentUserId || !isAdmin()) {
			return
		}

		let unsubscribe: (() => void) | undefined
		;(async () => {
			unsubscribe = await pb.collection("notification_logs").subscribe(
				"*",
				(event) => {
					if (event.action !== "create") {
						return
					}

					const log = event.record as NotificationLog
					if (log.status === "sent" && ALERT_EVENT_KINDS.has(log.event_kind)) {
						const now = Date.now()
						const key = `${log.event_kind}|${log.resource_type}|${log.resource_id}`
						const lastSeenAt = recentAlertKeys.current.get(key)
						if (lastSeenAt && now - lastSeenAt < DEDUPE_WINDOW_MS) {
							return
						}
						recentAlertKeys.current.set(key, now)
						for (const [entryKey, seenAt] of recentAlertKeys.current.entries()) {
							if (now - seenAt >= DEDUPE_WINDOW_MS) {
								recentAlertKeys.current.delete(entryKey)
							}
						}

						toast(getAlertToast(log))
						return
					}

					if (log.status === "failed") {
						toast({
							title: t`Notification delivery failed`,
							description: getToastDescription(log),
							variant: "destructive",
						})
						return
					}

					if (log.channel_kind === "in-app" && log.status === "sent") {
						toast({
							title: t`Notification`,
							description: getToastDescription(log),
						})
					}
				},
				{
					filter: `created_by = "${currentUserId}" && (status = "failed" || channel_kind = "in-app" || (status = "sent" && (event_kind = "monitor.down" || event_kind = "monitor.up" || event_kind = "agent.offline" || event_kind = "agent.online")))`,
					fields:
						"id,created_by,channel_kind,event_kind,resource_id,resource_name,resource_type,status,error,payload_preview,sent_at",
				}
			)
		})()

		return () => unsubscribe?.()
	}, [currentUserId])

	return null
}
