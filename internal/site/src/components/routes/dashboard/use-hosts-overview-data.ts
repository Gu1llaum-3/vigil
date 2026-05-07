import { useCallback, useEffect, useRef, useState } from "react"
import { pb } from "@/lib/api"
import type { HostsOverviewRecord } from "@/lib/dashboard-types"

export function useHostsOverviewData() {
	const [hosts, setHosts] = useState<HostsOverviewRecord[]>([])
	const [loading, setLoading] = useState(true)
	const debounceRef = useRef<ReturnType<typeof setTimeout> | null>(null)

	const fetchHosts = useCallback(async () => {
		try {
			const data = await pb.send<HostsOverviewRecord[]>("/api/app/hosts-overview", { method: "GET" })
			setHosts(data)
		} catch (error) {
			console.error("hosts overview fetch failed", error)
		} finally {
			setLoading(false)
		}
	}, [])

	useEffect(() => {
		const unsubscribes: Array<() => void> = []
		const debouncedFetch = () => {
			if (debounceRef.current) clearTimeout(debounceRef.current)
			debounceRef.current = setTimeout(fetchHosts, 1000)
		}

		fetchHosts()
		;(async () => {
			unsubscribes.push(await pb.collection("agents").subscribe("*", debouncedFetch))
			unsubscribes.push(await pb.collection("host_snapshots").subscribe("*", debouncedFetch))
			unsubscribes.push(await pb.collection("host_metric_current").subscribe("*", debouncedFetch))
		})()

		return () => {
			for (const unsubscribe of unsubscribes) unsubscribe()
			if (debounceRef.current) clearTimeout(debounceRef.current)
		}
	}, [fetchHosts])

	return { hosts, loading, refetch: fetchHosts }
}
