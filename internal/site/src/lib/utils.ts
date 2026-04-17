import { t } from "@lingui/core/macro"
import { type ClassValue, clsx } from "clsx"
import { useEffect, useState } from "react"
import { twMerge } from "tailwind-merge"
import { toast } from "@/components/ui/use-toast"
import { $copyContent } from "./stores"

export function cn(...inputs: ClassValue[]) {
	return twMerge(clsx(inputs))
}

/** Adds event listener to node and returns function that removes the listener */
export function listen<T extends Event = Event>(node: Node, event: string, handler: (event: T) => void) {
	node.addEventListener(event, handler as EventListener)
	return () => node.removeEventListener(event, handler as EventListener)
}

export async function copyToClipboard(content: string) {
	const duration = 1500
	try {
		await navigator.clipboard.writeText(content)
		toast({
			duration,
			description: t`Copied to clipboard`,
		})
	} catch (_e) {
		$copyContent.set(content)
	}
}

export const currentHour12 = () => new Intl.DateTimeFormat().resolvedOptions().hour12

/** Generate a random token for the agent */
export const generateToken = () => {
	try {
		return crypto?.randomUUID()
	} catch (_e) {
		return Array.from({ length: 2 }, () => (performance.now() * Math.random()).toString(16).replace(".", "-")).join("-")
	}
}

/** Get the hub URL from the global APP object */
export const getHubURL = () => globalThis.APP?.HUB_URL || window.location.origin

// Cache for runOnce
// biome-ignore lint/complexity/noBannedTypes: Function is used to allow any function to be passed in
const runOnceCache = new WeakMap<Function, { done: boolean; result: unknown }>()
/** Run a function only once */
// biome-ignore lint/suspicious/noExplicitAny: any is used to allow any function to be passed in
export function runOnce<T extends (...args: any[]) => any>(fn: T): T {
	return ((...args: Parameters<T>) => {
		let state = runOnceCache.get(fn)
		if (!state) {
			state = { done: false, result: undefined }
			runOnceCache.set(fn, state)
		}
		if (!state.done) {
			state.result = fn(...args)
			state.done = true
		}
		return state.result
	}) as T
}
