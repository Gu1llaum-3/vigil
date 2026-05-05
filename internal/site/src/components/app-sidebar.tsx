import { Trans } from "@lingui/react/macro"
import { useStore } from "@nanostores/react"
import { getPagePath } from "@nanostores/router"
import { ActivityIcon, BellIcon, BoxesIcon, HomeIcon, ImageIcon, ServerIcon, SettingsIcon } from "lucide-react"
import { useCallback, useEffect, useRef, useState } from "react"
import { Logo } from "@/components/logo"
import { Sheet, SheetContent, SheetHeader, SheetTitle } from "@/components/ui/sheet"
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip"
import { pb } from "@/lib/api"
import type { MonitorGroupResponse } from "@/lib/monitor-types"
import { cn } from "@/lib/utils"
import { $router, Link } from "./router"

function useDownMonitorCount() {
	const [downCount, setDownCount] = useState(0)
	const debounceRef = useRef<ReturnType<typeof setTimeout> | null>(null)

	const fetchDownCount = useCallback(async () => {
		try {
			const groups = await pb.send<MonitorGroupResponse[]>("/api/app/monitors", { method: "GET" })
			const allMonitors = (groups ?? []).flatMap((group) => group.monitors)
			setDownCount(allMonitors.filter((monitor) => monitor.last_checked_at && monitor.status === 0).length)
		} catch {
			// ignore transient sidebar fetch failures
		}
	}, [])

	useEffect(() => {
		let unsubscribe: (() => void) | undefined
		fetchDownCount()
		;(async () => {
			unsubscribe = await pb.collection("monitors").subscribe("*", () => {
				if (debounceRef.current) clearTimeout(debounceRef.current)
				debounceRef.current = setTimeout(fetchDownCount, 1000)
			})
		})()

		return () => {
			unsubscribe?.()
			if (debounceRef.current) clearTimeout(debounceRef.current)
		}
	}, [fetchDownCount])

	return downCount
}

function useImageUpdatesCount() {
	const [count, setCount] = useState(0)
	const debounceRef = useRef<ReturnType<typeof setTimeout> | null>(null)

	const fetchCount = useCallback(async () => {
		try {
			const records = await pb.collection("container_image_audits").getFullList<{ status: string }>({
				filter: 'status = "update_available"',
				fields: "id,status",
			})
			setCount(records.length)
		} catch {
			// non-fatal
		}
	}, [])

	useEffect(() => {
		fetchCount()
		let unsubscribe: (() => void) | undefined
		;(async () => {
			unsubscribe = await pb.collection("container_image_audits").subscribe("*", () => {
				if (debounceRef.current) clearTimeout(debounceRef.current)
				debounceRef.current = setTimeout(fetchCount, 1000)
			})
		})()
		return () => {
			unsubscribe?.()
			if (debounceRef.current) clearTimeout(debounceRef.current)
		}
	}, [fetchCount])

	return count
}

function CountBadge({ count, tone = "danger" }: { count: number; tone?: "danger" | "warning" }) {
	if (count <= 0) return null
	return (
		<span
			className={cn(
				"ms-auto inline-flex min-w-5 items-center justify-center rounded-full px-1.5 text-[10px] font-semibold leading-5 text-white",
				tone === "danger" ? "bg-red-500" : "bg-amber-500"
			)}
		>
			{count > 99 ? "99+" : count}
		</span>
	)
}

function SidebarContent({ collapsed, onNavigate }: { collapsed: boolean; onNavigate?: () => void }) {
	const page = useStore($router)
	const downMonitorCount = useDownMonitorCount()
	const imageUpdatesCount = useImageUpdatesCount()
	const items = [
		{ label: <Trans>Dashboard</Trans>, href: getPagePath($router, "home"), icon: HomeIcon, activeRoutes: ["home"] },
		{
			label: <Trans>Hosts</Trans>,
			href: getPagePath($router, "hosts"),
			icon: ServerIcon,
			activeRoutes: ["hosts", "host"],
		},
		{
			label: <Trans>Containers</Trans>,
			href: getPagePath($router, "containers"),
			icon: BoxesIcon,
			activeRoutes: ["containers"],
		},
		{
			label: <Trans>Image updates</Trans>,
			href: getPagePath($router, "images"),
			icon: ImageIcon,
			activeRoutes: ["images"],
			count: imageUpdatesCount,
			tone: "warning" as const,
		},
		{
			label: <Trans>Monitors</Trans>,
			href: getPagePath($router, "monitors"),
			icon: ActivityIcon,
			activeRoutes: ["monitors", "monitor"],
			count: downMonitorCount,
		},
		{
			label: <Trans>Notifications</Trans>,
			href: getPagePath($router, "notifications"),
			icon: BellIcon,
			activeRoutes: ["notifications"],
		},
		{
			label: <Trans>Settings</Trans>,
			href: getPagePath($router, "settings", { name: "general" }),
			icon: SettingsIcon,
			activeRoutes: ["settings"],
		},
	]

	return (
		<div className="flex h-full flex-col gap-4 p-3">
			<div className="flex h-12 items-center gap-2 px-1">
				<Link href={getPagePath($router, "home")} className="flex min-w-0 items-center gap-2" onClick={onNavigate}>
					<span className="logo-halo shrink-0">
						<span className="logo-halo-aura" aria-hidden="true" />
						<Logo interactive className="logo-halo-eye size-9 text-foreground" />
					</span>
					{!collapsed && <span className="auth-title truncate text-xl text-foreground">Vigil</span>}
				</Link>
			</div>

			<nav className="grid gap-1">
				{items.map((item) => {
					const active = item.activeRoutes.includes(page?.route ?? "")
					const Icon = item.icon
					const link = (
						<Link
							href={item.href}
							onClick={onNavigate}
							className={cn(
								"flex h-10 items-center gap-3 rounded-md px-3 text-sm font-medium text-muted-foreground transition-colors hover:bg-accent/60 hover:text-foreground",
								collapsed && "justify-center px-0",
								active && "bg-muted text-foreground shadow-xs"
							)}
						>
							<Icon className="size-4 shrink-0" />
							{!collapsed && <span className="truncate">{item.label}</span>}
							{!collapsed && "count" in item && <CountBadge count={item.count ?? 0} tone={item.tone} />}
						</Link>
					)

					if (!collapsed) return <div key={item.href}>{link}</div>

					return (
						<Tooltip key={item.href}>
							<TooltipTrigger asChild>{link}</TooltipTrigger>
							<TooltipContent side="right" className="flex items-center gap-2">
								{item.label}
								{"count" in item && <CountBadge count={item.count ?? 0} tone={item.tone} />}
							</TooltipContent>
						</Tooltip>
					)
				})}
			</nav>
		</div>
	)
}

export function AppSidebar({
	collapsed,
	mobileOpen,
	onMobileOpenChange,
}: {
	collapsed: boolean
	mobileOpen: boolean
	onMobileOpenChange: (open: boolean) => void
}) {
	return (
		<>
			<aside
				className={cn(
					"sticky top-0 hidden h-svh shrink-0 border-r border-border/60 bg-card/80 backdrop-blur lg:block",
					collapsed ? "w-[4.5rem]" : "w-64"
				)}
			>
				<SidebarContent collapsed={collapsed} />
			</aside>
			<Sheet open={mobileOpen} onOpenChange={onMobileOpenChange}>
				<SheetContent side="left" className="w-72 px-0 py-0">
					<SheetHeader className="sr-only">
						<SheetTitle>
							<Trans>Navigation</Trans>
						</SheetTitle>
					</SheetHeader>
					<SidebarContent collapsed={false} onNavigate={() => onMobileOpenChange(false)} />
				</SheetContent>
			</Sheet>
		</>
	)
}
