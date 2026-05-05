import { useCallback, useEffect, useRef, useState } from "react"
import { pb } from "@/lib/api"
import type { DashboardResponse } from "@/lib/dashboard-types"

export function useDashboardData() {
	const [dashboard, setDashboard] = useState<DashboardResponse | null>(null)
	const [loading, setLoading] = useState(true)
	const debounceRef = useRef<ReturnType<typeof setTimeout> | null>(null)

	const fetchDashboard = useCallback(async () => {
		try {
			const data = await pb.send<DashboardResponse>("/api/app/dashboard", { method: "GET" })
			setDashboard(data)
		} catch (error) {
			console.error("dashboard fetch failed", error)
		} finally {
			setLoading(false)
		}
	}, [])

	useEffect(() => {
		const unsubscribes: Array<() => void> = []
		const debouncedFetch = () => {
			if (debounceRef.current) clearTimeout(debounceRef.current)
			debounceRef.current = setTimeout(fetchDashboard, 1000)
		}

		fetchDashboard()
		;(async () => {
			unsubscribes.push(await pb.collection("agents").subscribe("*", debouncedFetch))
			unsubscribes.push(await pb.collection("host_snapshots").subscribe("*", debouncedFetch))
			unsubscribes.push(await pb.collection("monitors").subscribe("*", debouncedFetch))
			unsubscribes.push(await pb.collection("container_image_audits").subscribe("*", debouncedFetch))
		})()

		return () => {
			for (const unsubscribe of unsubscribes) unsubscribe()
			if (debounceRef.current) clearTimeout(debounceRef.current)
		}
	}, [fetchDashboard])

	return { dashboard, loading, refetch: fetchDashboard }
}
