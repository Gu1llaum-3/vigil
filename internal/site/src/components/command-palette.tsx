import { t } from "@lingui/core/macro"
import { Trans } from "@lingui/react/macro"
import { getPagePath } from "@nanostores/router"
import { DialogDescription } from "@radix-ui/react-dialog"
import {
	ActivityIcon,
	BellIcon,
	BotIcon,
	BoxesIcon,
	DatabaseBackupIcon,
	HomeIcon,
	ImageIcon,
	LogsIcon,
	MailIcon,
	ServerIcon,
	SettingsIcon,
	UsersIcon,
} from "lucide-react"
import { memo, useEffect, useMemo } from "react"
import {
	CommandDialog,
	CommandEmpty,
	CommandGroup,
	CommandInput,
	CommandItem,
	CommandList,
	CommandSeparator,
	CommandShortcut,
} from "@/components/ui/command"
import { isAdmin } from "@/lib/api"
import { listen } from "@/lib/utils"
import { $router, basePath, navigate, prependBasePath } from "./router"

export default memo(function CommandPalette({ open, setOpen }: { open: boolean; setOpen: (open: boolean) => void }) {
	useEffect(() => {
		const down = (e: KeyboardEvent) => {
			if (e.key === "k" && (e.metaKey || e.ctrlKey)) {
				e.preventDefault()
				setOpen(!open)
			}
		}
		return listen(document, "keydown", down)
	}, [open, setOpen])

	return useMemo(() => {
		const PageShortcut = (
			<CommandShortcut>
				<Trans>Page</Trans>
			</CommandShortcut>
		)
		const SettingsShortcut = (
			<CommandShortcut>
				<Trans>Settings</Trans>
			</CommandShortcut>
		)
		const AdminShortcut = (
			<CommandShortcut>
				<Trans>Admin</Trans>
			</CommandShortcut>
		)
		return (
			<CommandDialog open={open} onOpenChange={setOpen}>
				<DialogDescription className="sr-only">Command palette</DialogDescription>
				<CommandInput placeholder={t`Search pages and settings...`} />
				<CommandList>
					<CommandGroup heading={t`Pages / Settings`}>
						<CommandItem
							keywords={["home"]}
							onSelect={() => {
								navigate(basePath)
								setOpen(false)
							}}
						>
							<HomeIcon className="me-2 size-4" />
							<span>
								<Trans>Home</Trans>
							</span>
							{PageShortcut}
						</CommandItem>
						<CommandItem
							keywords={["servers", "agents", "fleet"]}
							onSelect={() => {
								navigate(getPagePath($router, "hosts"))
								setOpen(false)
							}}
						>
							<ServerIcon className="me-2 size-4" />
							<span>
								<Trans>Hosts</Trans>
							</span>
							{PageShortcut}
						</CommandItem>
						<CommandItem
							keywords={["docker", "containers", "runtime"]}
							onSelect={() => {
								navigate(getPagePath($router, "containers"))
								setOpen(false)
							}}
						>
							<BoxesIcon className="me-2 size-4" />
							<span>
								<Trans>Containers</Trans>
							</span>
							{PageShortcut}
						</CommandItem>
						<CommandItem
							keywords={["images", "audit", "updates"]}
							onSelect={() => {
								navigate(getPagePath($router, "images"))
								setOpen(false)
							}}
						>
							<ImageIcon className="me-2 size-4" />
							<span>
								<Trans>Image updates</Trans>
							</span>
							{PageShortcut}
						</CommandItem>
						<CommandItem
							keywords={["monitor", "monitors", "uptime"]}
							onSelect={() => {
								navigate(getPagePath($router, "monitors"))
								setOpen(false)
							}}
						>
							<ActivityIcon className="me-2 size-4" />
							<span>
								<Trans>Monitors</Trans>
							</span>
							{PageShortcut}
						</CommandItem>
						<CommandItem
							onSelect={() => {
								navigate(getPagePath($router, "settings", { name: "general" }))
								setOpen(false)
							}}
						>
							<SettingsIcon className="me-2 size-4" />
							<span>
								<Trans>Settings</Trans>
							</span>
							{SettingsShortcut}
						</CommandItem>
						<CommandItem
							keywords={[t`Agents`, t`Enrollment token`]}
							onSelect={() => {
								navigate(getPagePath($router, "settings", { name: "agents" }))
								setOpen(false)
							}}
						>
							<BotIcon className="me-2 size-4" />
							<span>
								<Trans>Agents</Trans>
							</span>
							{SettingsShortcut}
						</CommandItem>
						{isAdmin() && (
							<CommandItem
								keywords={["notification", "notifications", "alerts", "rules", "channels"]}
								onSelect={() => {
									navigate(getPagePath($router, "settings", { name: "notifications" }))
									setOpen(false)
								}}
							>
								<BellIcon className="me-2 size-4" />
								<span>
									<Trans>Notifications</Trans>
								</span>
								{SettingsShortcut}
							</CommandItem>
						)}
					</CommandGroup>
					{isAdmin() && (
						<>
							<CommandSeparator className="mb-1.5" />
							<CommandGroup heading={t`Admin`}>
								<CommandItem
									keywords={["pocketbase"]}
									onSelect={() => {
										setOpen(false)
										window.open(prependBasePath("/_/"), "_blank")
									}}
								>
									<UsersIcon className="me-2 size-4" />
									<span>
										<Trans>Users</Trans>
									</span>
									{AdminShortcut}
								</CommandItem>
								<CommandItem
									onSelect={() => {
										setOpen(false)
										window.open(prependBasePath("/_/#/logs"), "_blank")
									}}
								>
									<LogsIcon className="me-2 size-4" />
									<span>
										<Trans>Logs</Trans>
									</span>
									{AdminShortcut}
								</CommandItem>
								<CommandItem
									onSelect={() => {
										setOpen(false)
										window.open(prependBasePath("/_/#/settings/backups"), "_blank")
									}}
								>
									<DatabaseBackupIcon className="me-2 size-4" />
									<span>
										<Trans>Backups</Trans>
									</span>
									{AdminShortcut}
								</CommandItem>
								<CommandItem
									keywords={["email"]}
									onSelect={() => {
										setOpen(false)
										window.open(prependBasePath("/_/#/settings/mail"), "_blank")
									}}
								>
									<MailIcon className="me-2 size-4" />
									<span>
										<Trans>SMTP settings</Trans>
									</span>
									{AdminShortcut}
								</CommandItem>
							</CommandGroup>
						</>
					)}
					<CommandEmpty>
						<Trans>No results found.</Trans>
					</CommandEmpty>
				</CommandList>
			</CommandDialog>
		)
	}, [open])
})
