import { Trans } from "@lingui/react/macro"
import { getPagePath } from "@nanostores/router"
import {
	PlusIcon,
	ActivityIcon,
	DatabaseBackupIcon,
	LogOutIcon,
	LogsIcon,
	MenuIcon,
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
import { cn, runOnce } from "@/lib/utils"
import { AddAgentDialog } from "./add-agent"
import { LangToggle } from "./lang-toggle"
import { Logo } from "./logo"
import { ModeToggle } from "./mode-toggle"
import { $router, basePath, Link, navigate, prependBasePath } from "./router"
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

export default function Navbar() {
	const [addAgentDialogOpen, setAddAgentDialogOpen] = useState(false)
	const [commandPaletteOpen, setCommandPaletteOpen] = useState(false)
	const downMonitorCount = useDownMonitorCount()

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
				className="p-2 ps-0 me-3 group"
				onMouseEnter={runOnce(() => import("@/components/routes/home"))}
			>
				<span className="logo-halo">
					<span className="logo-halo-aura" aria-hidden="true" />
					<Logo interactive className="logo-halo-eye h-8 w-8 md:h-9 md:w-9 text-foreground shrink-0" />
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
				<a href={prependBasePath("/_/")} target="_blank">
					<UsersIcon className="me-2.5 h-4 w-4" />
					<span>
						<Trans>Users</Trans>
					</span>
				</a>
			</DropdownMenuItem>
			<DropdownMenuItem asChild>
				<a href={prependBasePath("/_/#/logs")} target="_blank">
					<LogsIcon className="me-2.5 h-4 w-4" />
					<span>
						<Trans>Logs</Trans>
					</span>
				</a>
			</DropdownMenuItem>
			<DropdownMenuItem asChild>
				<a href={prependBasePath("/_/#/settings/backups")} target="_blank">
					<DatabaseBackupIcon className="me-2.5 h-4 w-4" />
					<span>
						<Trans>Backups</Trans>
					</span>
				</a>
			</DropdownMenuItem>
		</DropdownMenuGroup>
	)
}
