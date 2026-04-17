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
