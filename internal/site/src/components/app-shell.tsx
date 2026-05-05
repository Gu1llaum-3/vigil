import { useEffect, useState } from "react"
import Navbar from "@/components/navbar"
import { AppSidebar } from "@/components/app-sidebar"
import { cn } from "@/lib/utils"

const sidebarStateKey = "vigil.sidebar.collapsed"

export function AppShell({ children }: { children: React.ReactNode }) {
	const [mobileNavOpen, setMobileNavOpen] = useState(false)
	const [collapsed, setCollapsed] = useState(() => {
		try {
			return window.localStorage.getItem(sidebarStateKey) === "true"
		} catch {
			return false
		}
	})

	useEffect(() => {
		try {
			window.localStorage.setItem(sidebarStateKey, String(collapsed))
		} catch {
			// ignore storage failures
		}
	}, [collapsed])

	return (
		<div className="flex min-h-svh bg-background">
			<AppSidebar collapsed={collapsed} mobileOpen={mobileNavOpen} onMobileOpenChange={setMobileNavOpen} />
			<div className="min-w-0 flex-1">
				<div className="sticky top-0 z-30 border-b border-border/60 bg-background/85 backdrop-blur supports-[backdrop-filter]:bg-background/70">
					<div className="container">
						<Navbar
							onMenuClick={() => setMobileNavOpen(true)}
							sidebarCollapsed={collapsed}
							onSidebarCollapsedChange={setCollapsed}
						/>
					</div>
				</div>
				<main className={cn("container relative py-6 sm:py-8")}>{children}</main>
			</div>
		</div>
	)
}
