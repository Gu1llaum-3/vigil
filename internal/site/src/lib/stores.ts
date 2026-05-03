import { atom, map } from "nanostores"
import type { UserSettings } from "@/types"
import { pb } from "./api"

/** Default layout width. Used as fallback when user setting is unset. */
export const defaultLayoutWidth = 1580

/** Store if user is authenticated */
export const $authenticated = atom(pb.authStore.isValid)

/** User settings */
export const $userSettings = map<UserSettings>({})

/** Fallback copy to clipboard dialog content */
export const $copyContent = atom("")

/** Direction for localization */
export const $direction = atom<"ltr" | "rtl">("ltr")

/**
 * Bumped whenever a surface marks system notifications as read. Both the
 * navbar bell and the /notifications page watch this stamp and re-fetch on
 * change so the read state stays in sync across surfaces (the read cursor is
 * stored on user_settings, which has no per-record realtime event).
 */
export const $systemNotificationsReadStamp = atom(0)
export const bumpSystemNotificationsReadStamp = () => $systemNotificationsReadStamp.set(Date.now())
