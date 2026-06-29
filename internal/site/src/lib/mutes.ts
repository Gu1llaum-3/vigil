import { useCallback, useEffect, useState } from "react"
import { pb } from "@/lib/api"

/**
 * Per-resource notification mutes. A mute suppresses BOTH the in-app bell and external
 * channel delivery for a single monitor, host (agent), or container — see the
 * notification_mutes collection and the hub's emitNotification chokepoint.
 */

export type MuteResourceType = "monitor" | "agent" | "container_image"

const COLLECTION = "notification_mutes"

interface MuteRecord {
	id: string
	resource_type: MuteResourceType
	resource_id: string
	muted_until: string // "" = indefinite
}

/** muteKey is the lookup key used by the useMutes map. */
export function muteKey(type: MuteResourceType, id: string): string {
	return `${type}:${id}`
}

/** A mute is active when it has no expiry, or its expiry is still in the future. */
function isActive(mutedUntil: string, now: number): boolean {
	if (!mutedUntil) return true
	const ts = new Date(mutedUntil).getTime()
	return Number.isNaN(ts) || ts > now
}

export interface ActiveMute {
	id: string
	/** RFC3339 expiry, or "" for an indefinite mute. */
	mutedUntil: string
}

/** Duration presets offered in the mute menu. `null` = indefinite. */
export const MUTE_PRESETS: Array<{ hours: number | null }> = [
	{ hours: 1 },
	{ hours: 8 },
	{ hours: 24 },
	{ hours: null },
]

/** mutedUntilFromHours converts a preset to an expiry Date (or null for indefinite). */
export function mutedUntilFromHours(hours: number | null): Date | null {
	if (hours == null) return null
	return new Date(Date.now() + hours * 3600 * 1000)
}

function findMute(type: MuteResourceType, id: string): Promise<MuteRecord[]> {
	return pb.collection(COLLECTION).getFullList<MuteRecord>({
		filter: pb.filter("resource_type = {:t} && resource_id = {:id}", { t: type, id }),
	})
}

/**
 * muteResource upserts a mute for the resource. The collection has a unique index on
 * (resource_type, resource_id), so an existing mute is updated rather than duplicated.
 * The find-then-create is not atomic, so two concurrent mutes (double-click, two tabs)
 * can race on the unique index — the loser's create is rejected; we recover by updating
 * the row the winner just inserted rather than surfacing a false "Failed to mute".
 */
export async function muteResource(type: MuteResourceType, id: string, mutedUntil: Date | null): Promise<void> {
	const until = mutedUntil ? mutedUntil.toISOString() : ""
	const existing = await findMute(type, id)
	if (existing.length > 0) {
		await pb.collection(COLLECTION).update(existing[0].id, { muted_until: until })
		return
	}
	try {
		await pb.collection(COLLECTION).create({
			resource_type: type,
			resource_id: id,
			muted_until: until,
			created_by: pb.authStore.record?.id,
		})
	} catch (e) {
		const raced = await findMute(type, id)
		if (raced.length > 0) {
			await pb.collection(COLLECTION).update(raced[0].id, { muted_until: until })
			return
		}
		throw e
	}
}

/** unmuteResource removes any mute(s) for the resource, tolerating an already-deleted row. */
export async function unmuteResource(type: MuteResourceType, id: string): Promise<void> {
	const existing = await findMute(type, id)
	await Promise.all(
		existing.map((r) =>
			pb
				.collection(COLLECTION)
				.delete(r.id)
				.catch((e: { status?: number }) => {
					// Already removed by realtime/another client — the end state is correct.
					if (e?.status !== 404) throw e
				})
		)
	)
}

/**
 * useMutes returns the set of currently-active mutes keyed by `${type}:${id}`, kept in
 * sync via the realtime subscription. A timer also re-evaluates periodically so a timed
 * mute drops out of the map when it expires (expiry produces no DB event of its own).
 */
export function useMutes(): Map<string, ActiveMute> {
	const [mutes, setMutes] = useState<Map<string, ActiveMute>>(new Map())

	const load = useCallback(async () => {
		try {
			const records = await pb.collection(COLLECTION).getFullList<MuteRecord>()
			const now = Date.now()
			const next = new Map<string, ActiveMute>()
			for (const r of records) {
				if (isActive(r.muted_until, now)) {
					next.set(muteKey(r.resource_type, r.resource_id), { id: r.id, mutedUntil: r.muted_until })
				}
			}
			setMutes(next)
		} catch {
			// non-fatal: ignore transient fetch failures
		}
	}, [])

	useEffect(() => {
		let cancelled = false
		let unsubscribe: (() => void) | undefined
		let debounce: ReturnType<typeof setTimeout> | null = null
		// Debounce realtime-driven refetches (AGENTS.md convention) so a burst of mute
		// changes coalesces into one getFullList instead of one per event.
		const scheduleLoad = () => {
			if (debounce) clearTimeout(debounce)
			debounce = setTimeout(load, 1000)
		}
		load()
		;(async () => {
			try {
				const handle = await pb.collection(COLLECTION).subscribe("*", scheduleLoad)
				// If the component unmounted before subscribe() resolved, tear down now —
				// otherwise the late-assigned handle would leak past cleanup.
				if (cancelled) handle()
				else unsubscribe = handle
			} catch {
				// realtime subscription is best-effort
			}
		})()
		const interval = setInterval(load, 60_000)
		return () => {
			cancelled = true
			unsubscribe?.()
			if (debounce) clearTimeout(debounce)
			clearInterval(interval)
		}
	}, [load])

	return mutes
}
