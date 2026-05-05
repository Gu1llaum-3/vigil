import { Trans } from "@lingui/react/macro"
import { useStore } from "@nanostores/react"
import { getPagePath } from "@nanostores/router"
import {
	BellIcon,
	BoxesIcon,
	PlusIcon,
	ActivityIcon,
	DatabaseBackupIcon,
	CheckCheckIcon,
	LogOutIcon,
	LogsIcon,
	MenuIcon,
	Clock3Icon,
	SearchIcon,
	SettingsIcon,
	UserIcon,
	UsersIcon,
} from "lucide-react"
import { lazy, Suspense, useCallback, useEffect, useRef, useState } from "react"
import { Button, buttonVariants } from "@/components/ui/button"
import {
	DropdownMenu,
	DropdownMenuContent,
	DropdownMenuGroup,
	DropdownMenuItem,
	DropdownMenuLabel,
	DropdownMenuSeparator,
	DropdownMenuSub,
	DropdownMenuSubContent,
	DropdownMenuSubTrigger,
	DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu"
import { isAdmin, isReadOnlyUser, logOut, pb } from "@/lib/api"
import type { MonitorGroupResponse } from "@/lib/monitor-types"
import { $systemNotificationsReadStamp, bumpSystemNotificationsReadStamp } from "@/lib/stores"
import { cn, runOnce } from "@/lib/utils"
import { AddAgentDialog } from "./add-agent"
import { LangToggle } from "./lang-toggle"
import { Logo } from "./logo"
import { ModeToggle } from "./mode-toggle"
import { $router, basePath, Link, navigate, prependBasePath } from "./router"
import { Badge } from "./ui/badge"
import type { SystemNotification, SystemNotificationUnreadResponse } from "@/types"
import { Tooltip, TooltipContent, TooltipTrigger } from "./ui/tooltip"

const CommandPalette = lazy(() => import("./command-palette"))

const isMac = navigator.platform.toUpperCase().indexOf("MAC") >= 0

function useDownMonitorCount() {
	const [downCount, setDownCount] = useState(0)
	const debounceRef = useRef<ReturnType<typeof setTimeout> | null>(null)

	const fetchDownCount = useCallback(async () => {
		try {
			const groups = await pb.send<MonitorGroupResponse[]>("/api/app/monitors", { method: "GET" })
			const allMonitors = (groups ?? []).flatMap((group) => group.monitors)
			setDownCount(allMonitors.filter((monitor) => monitor.last_checked_at && monitor.status === 0).length)
		} catch {
			// ignore transient navbar fetch failures
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

function formatNotificationTime(sentAt: string) {
	if (!sentAt) return ""
	const parsed = new Date(sentAt)
	if (Number.isNaN(parsed.getTime())) return sentAt
	return parsed.toLocaleString()
}

function useNotificationCenter() {
	const currentUserId = pb.authStore.record?.id
	const [items, setItems] = useState<SystemNotification[]>([])
	const [count, setCount] = useState(0)
	const debounceRef = useRef<ReturnType<typeof setTimeout> | null>(null)
	const readStamp = useStore($systemNotificationsReadStamp)

	const fetchUnread = useCallback(async () => {
		try {
			if (!currentUserId) {
				setItems([])
				setCount(0)
				return
			}
			const res = await pb.send<SystemNotificationUnreadResponse>("/api/app/system-notifications/unread", {
				method: "GET",
				query: { limit: 8 },
			})
			setItems(res.items ?? [])
			setCount(res.count ?? 0)
		} catch {
			// ignore transient navbar fetch failures
		}
	}, [currentUserId])

	useEffect(() => {
		if (!currentUserId) {
			setItems([])
			setCount(0)
			return
		}

		let unsubscribe: (() => void) | undefined
		fetchUnread()
		;(async () => {
			unsubscribe = await pb.collection("system_notifications").subscribe(
				"*",
				() => {
					if (debounceRef.current) clearTimeout(debounceRef.current)
					debounceRef.current = setTimeout(fetchUnread, 500)
				},
				{
					fields:
						"id,event_kind,category,severity,resource_id,resource_name,resource_type,title,message,payload,occurred_at",
				}
			)
		})()

		return () => {
			unsubscribe?.()
			if (debounceRef.current) clearTimeout(debounceRef.current)
		}
	}, [currentUserId, fetchUnread])

	useEffect(() => {
		if (!currentUserId) return
		if (readStamp === 0) return
		fetchUnread()
	}, [currentUserId, readStamp, fetchUnread])

	const markAllAsRead = useCallback(async () => {
		if (!currentUserId) return
		try {
			await pb.send("/api/app/system-notifications/read-all", { method: "POST" })
			// fetchUnread is re-run by the readStamp-bumped useEffect; awaiting it here
			// would race the same URL and the PocketBase SDK auto-cancels the older one.
			bumpSystemNotificationsReadStamp()
		} catch {
			// ignore transient navbar action failures
		}
	}, [currentUserId])

	return { items, count, markAllAsRead }
}

function NotificationBellIcon({ count }: { count: number }) {
	return (
		<span className="relative inline-flex">
			<BellIcon className="h-[1.2rem] w-[1.2rem]" />
			{count > 0 ? (
				<span className="absolute -right-2 -top-2 inline-flex min-w-4 items-center justify-center rounded-full bg-red-500 px-1 text-[10px] font-semibold leading-4 text-white">
					{count > 9 ? "9+" : count}
				</span>
			) : null}
		</span>
	)
}

function notificationSeverityVariant(severity: string) {
	switch (severity) {
		case "critical":
			return "danger" as const
		case "warning":
			return "warning" as const
		default:
			return "secondary" as const
	}
}

function notificationSummary(log: SystemNotification) {
	if (log.message && log.message.trim() !== log.title.trim()) return log.message
	if (log.resource_name || log.resource_id) return log.resource_name || log.resource_id
	return log.event_kind
}

function NotificationCenterMenu({
	count,
	items,
	onClear,
}: {
	count: number
	items: SystemNotification[]
	onClear: () => void
}) {
	return (
		<DropdownMenu>
			<DropdownMenuTrigger asChild>
				<button aria-label="Notifications" className={cn(buttonVariants({ variant: "ghost", size: "icon" }))}>
					<NotificationBellIcon count={count} />
				</button>
			</DropdownMenuTrigger>
			<DropdownMenuContent align="end" className="w-96 max-w-[calc(100vw-2rem)] p-0">
				<div className="flex items-center justify-between gap-3 px-3 py-2">
					<DropdownMenuLabel className="p-0 text-sm font-semibold">
						<Trans>Notifications</Trans>
					</DropdownMenuLabel>
					{count > 0 ? (
						<Badge variant="secondary" className="shrink-0">
							{count}
						</Badge>
					) : null}
				</div>
				<DropdownMenuSeparator />
				<div className="max-h-80 overflow-y-auto p-1">
					{items.length === 0 ? (
						<div className="px-3 py-6 text-sm text-muted-foreground">
							<Trans>No unread notifications</Trans>
						</div>
					) : (
						items.map((log) => (
							<div key={log.id} className="rounded-md px-3 py-2 hover:bg-accent/50">
								<div className="flex items-start gap-2">
									<div className="min-w-0 flex-1">
										<p className="truncate text-sm font-medium text-foreground">{log.title}</p>
										<p className="truncate text-xs text-muted-foreground">{notificationSummary(log)}</p>
									</div>
									<Badge variant={notificationSeverityVariant(log.severity)} className="shrink-0 uppercase">
										{log.severity}
									</Badge>
								</div>
								<div className="mt-1 flex items-center gap-1 text-xs text-muted-foreground">
									<Clock3Icon className="size-3.5 shrink-0" />
									<span>{formatNotificationTime(log.occurred_at)}</span>
								</div>
							</div>
						))
					)}
				</div>
				<DropdownMenuSeparator />
				<DropdownMenuItem onSelect={() => navigate(getPagePath($router, "notifications"))}>
					<Trans>View all notifications</Trans>
				</DropdownMenuItem>
				<DropdownMenuItem onSelect={onClear} disabled={count === 0} className="flex items-center gap-2">
					<CheckCheckIcon className="size-4" />
					<Trans>Mark all as read</Trans>
				</DropdownMenuItem>
			</DropdownMenuContent>
		</DropdownMenu>
	)
}

function MonitorNavIcon({ downCount }: { downCount: number }) {
	return (
		<span className="relative inline-flex">
			<ActivityIcon className="h-[1.2rem] w-[1.2rem]" />
			{downCount > 0 ? (
				<span className="absolute -right-2 -top-2 inline-flex min-w-4 items-center justify-center rounded-full bg-red-500 px-1 text-[10px] font-semibold leading-4 text-white">
					{downCount > 9 ? "9+" : downCount}
				</span>
			) : null}
		</span>
	)
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

function ImagesNavIcon({ count }: { count: number }) {
	return (
		<span className="relative inline-flex">
			<BoxesIcon className="h-[1.2rem] w-[1.2rem]" />
			{count > 0 ? (
				<span className="absolute -right-2 -top-2 inline-flex min-w-4 items-center justify-center rounded-full bg-amber-500 px-1 text-[10px] font-semibold leading-4 text-white">
					{count > 9 ? "9+" : count}
				</span>
			) : null}
		</span>
	)
}

export default function Navbar() {
	const [addAgentDialogOpen, setAddAgentDialogOpen] = useState(false)
	const [commandPaletteOpen, setCommandPaletteOpen] = useState(false)
	const downMonitorCount = useDownMonitorCount()
	const imageUpdatesCount = useImageUpdatesCount()
	const notificationCenter = useNotificationCenter()

	const AdminLinks = AdminDropdownGroup()

	return (
		<div className="flex items-center h-14 md:h-16 bg-card px-4 pe-3 sm:px-6 border border-border/60 bt-0 rounded-md my-4">
			<Suspense>
				<CommandPalette open={commandPaletteOpen} setOpen={setCommandPaletteOpen} />
			</Suspense>
			<AddAgentDialog open={addAgentDialogOpen} setOpen={setAddAgentDialogOpen} />

			<Link
				href={basePath}
				aria-label="Home"
				className="group me-4 flex items-center gap-2 p-2 ps-0"
				onMouseEnter={runOnce(() => import("@/components/routes/home"))}
			>
				<span className="logo-halo">
					<span className="logo-halo-aura" aria-hidden="true" />
					<Logo interactive className="logo-halo-eye h-8 w-8 md:h-9 md:w-9 text-foreground shrink-0" />
				</span>
				<span
					className="auth-title shrink-0 whitespace-nowrap"
					style={{ color: "hsl(var(--foreground))", fontSize: "1.25rem" }}
				>
					Vigil
				</span>
			</Link>
			<Button
				variant="outline"
				className="hidden md:block text-sm text-muted-foreground px-4"
				onClick={() => setCommandPaletteOpen(true)}
			>
				<span className="flex items-center">
					<SearchIcon className="me-1.5 h-4 w-4" />
					<Trans>Search</Trans>
					<span className="flex items-center ms-3.5">
						<Kbd>{isMac ? "⌘" : "Ctrl"}</Kbd>
						<Kbd>K</Kbd>
					</span>
				</span>
			</Button>

			{/* mobile menu */}
			<div className="ms-auto flex items-center text-xl md:hidden">
				<ModeToggle />
				<Button variant="ghost" size="icon" onClick={() => setCommandPaletteOpen(true)}>
					<SearchIcon className="h-[1.2rem] w-[1.2rem]" />
				</Button>
				<NotificationCenterMenu
					count={notificationCenter.count}
					items={notificationCenter.items}
					onClear={notificationCenter.markAllAsRead}
				/>
				<DropdownMenu>
					<DropdownMenuTrigger
						onMouseEnter={() => import("@/components/routes/settings/general")}
						className="ms-3"
						aria-label="Open Menu"
					>
						<MenuIcon />
					</DropdownMenuTrigger>
					<DropdownMenuContent align="end">
						<DropdownMenuLabel className="max-w-40 truncate">{pb.authStore.record?.email}</DropdownMenuLabel>
						<DropdownMenuSeparator />
						<DropdownMenuGroup>
							<DropdownMenuItem
								onClick={() => navigate(getPagePath($router, "monitors"))}
								className="flex items-center"
							>
								<span className="me-2.5">
									<MonitorNavIcon downCount={downMonitorCount} />
								</span>
								<Trans>Monitors</Trans>
							</DropdownMenuItem>
							<DropdownMenuItem
								onClick={() => navigate(getPagePath($router, "images"))}
								className="flex items-center"
							>
								<span className="me-2.5">
									<ImagesNavIcon count={imageUpdatesCount} />
								</span>
								<Trans>Container images</Trans>
							</DropdownMenuItem>
							<DropdownMenuItem
								onClick={() => navigate(getPagePath($router, "settings", { name: "general" }))}
								className="flex items-center"
							>
								<SettingsIcon className="h-4 w-4 me-2.5" />
								<Trans>Settings</Trans>
							</DropdownMenuItem>
							{isAdmin() && (
								<DropdownMenuSub>
									<DropdownMenuSubTrigger>
										<UserIcon className="h-4 w-4 me-2.5" />
										<Trans>Admin</Trans>
									</DropdownMenuSubTrigger>
									<DropdownMenuSubContent>{AdminLinks}</DropdownMenuSubContent>
								</DropdownMenuSub>
							)}
							{!isReadOnlyUser() && (
								<DropdownMenuItem
									className="flex items-center"
									onSelect={() => {
										setAddAgentDialogOpen(true)
									}}
								>
									<PlusIcon className="h-4 w-4 me-2.5" />
									<Trans>Add agent</Trans>
								</DropdownMenuItem>
							)}
						</DropdownMenuGroup>
						<DropdownMenuSeparator />
						<DropdownMenuGroup>
							<DropdownMenuItem onSelect={logOut} className="flex items-center">
								<LogOutIcon className="h-4 w-4 me-2.5" />
								<Trans>Log Out</Trans>
							</DropdownMenuItem>
						</DropdownMenuGroup>
					</DropdownMenuContent>
				</DropdownMenu>
			</div>

			{/* desktop nav */}
			{/** biome-ignore lint/a11y/noStaticElementInteractions: ignore */}
			<div
				className="hidden md:flex items-center ms-auto"
				onMouseEnter={() => import("@/components/routes/settings/general")}
			>
				<LangToggle />
				<ModeToggle />
				<div className="me-1">
					<NotificationCenterMenu
						count={notificationCenter.count}
						items={notificationCenter.items}
						onClear={notificationCenter.markAllAsRead}
					/>
				</div>
				<Tooltip>
					<TooltipTrigger asChild>
						<Link
							href={getPagePath($router, "monitors")}
							aria-label="Monitors"
							className={cn(buttonVariants({ variant: "ghost", size: "icon" }))}
							onMouseEnter={runOnce(() => import("@/components/routes/monitors"))}
						>
							<MonitorNavIcon downCount={downMonitorCount} />
						</Link>
					</TooltipTrigger>
					<TooltipContent>
						<Trans>Monitors</Trans>
					</TooltipContent>
				</Tooltip>
				<Tooltip>
					<TooltipTrigger asChild>
						<Link
							href={getPagePath($router, "images")}
							aria-label="Container images"
							className={cn(buttonVariants({ variant: "ghost", size: "icon" }))}
							onMouseEnter={runOnce(() => import("@/components/routes/images"))}
						>
							<ImagesNavIcon count={imageUpdatesCount} />
						</Link>
					</TooltipTrigger>
					<TooltipContent>
						<Trans>Container images</Trans>
					</TooltipContent>
				</Tooltip>
				<Tooltip>
					<TooltipTrigger asChild>
						<Link
							href={getPagePath($router, "settings", { name: "general" })}
							aria-label="Settings"
							className={cn(buttonVariants({ variant: "ghost", size: "icon" }))}
						>
							<SettingsIcon className="h-[1.2rem] w-[1.2rem]" />
						</Link>
					</TooltipTrigger>
					<TooltipContent>
						<Trans>Settings</Trans>
					</TooltipContent>
				</Tooltip>
				<DropdownMenu>
					<DropdownMenuTrigger asChild>
						<button aria-label="User Actions" className={cn(buttonVariants({ variant: "ghost", size: "icon" }))}>
							<UserIcon className="h-[1.2rem] w-[1.2rem]" />
						</button>
					</DropdownMenuTrigger>
					<DropdownMenuContent align="end" className="min-w-44">
						<DropdownMenuLabel>{pb.authStore.record?.email}</DropdownMenuLabel>
						<DropdownMenuSeparator />
						{isAdmin() && (
							<>
								{AdminLinks}
								<DropdownMenuSeparator />
							</>
						)}
						<DropdownMenuItem onSelect={logOut}>
							<LogOutIcon className="me-2.5 h-4 w-4" />
							<span>
								<Trans>Log Out</Trans>
							</span>
						</DropdownMenuItem>
					</DropdownMenuContent>
				</DropdownMenu>
				{!isReadOnlyUser() && (
					<Button variant="outline" className="flex gap-1 ms-2" onClick={() => setAddAgentDialogOpen(true)}>
						<PlusIcon className="h-4 w-4 -ms-1" />
						<Trans>Add agent</Trans>
					</Button>
				)}
			</div>
		</div>
	)
}

const Kbd = ({ children }: { children: React.ReactNode }) => (
	<kbd className="pointer-events-none inline-flex h-5 select-none items-center gap-1 rounded border bg-muted px-1.5 font-mono text-[10px] font-medium text-muted-foreground opacity-100">
		{children}
	</kbd>
)

function AdminDropdownGroup() {
	return (
		<DropdownMenuGroup>
			<DropdownMenuItem asChild>
				<a href={prependBasePath("/_/")} target="_blank" rel="noopener">
					<UsersIcon className="me-2.5 h-4 w-4" />
					<span>
						<Trans>Users</Trans>
					</span>
				</a>
			</DropdownMenuItem>
			<DropdownMenuItem asChild>
				<a href={prependBasePath("/_/#/logs")} target="_blank" rel="noopener">
					<LogsIcon className="me-2.5 h-4 w-4" />
					<span>
						<Trans>Logs</Trans>
					</span>
				</a>
			</DropdownMenuItem>
			<DropdownMenuItem asChild>
				<a href={prependBasePath("/_/#/settings/backups")} target="_blank" rel="noopener">
					<DatabaseBackupIcon className="me-2.5 h-4 w-4" />
					<span>
						<Trans>Backups</Trans>
					</span>
				</a>
			</DropdownMenuItem>
		</DropdownMenuGroup>
	)
}
