import "./index.css"
import { i18n } from "@lingui/core"
import { I18nProvider } from "@lingui/react"
import { useStore } from "@nanostores/react"
import { DirectionProvider } from "@radix-ui/react-direction"
import { lazy, memo, Suspense, useEffect } from "react"
import ReactDOM from "react-dom/client"
import { AppShell } from "@/components/app-shell.tsx"
import NotificationLogToasts from "@/components/notification-log-toasts.tsx"
import { $router } from "@/components/router.tsx"
import Settings from "@/components/routes/settings/layout.tsx"
import { ThemeProvider } from "@/components/theme-provider.tsx"
import { Toaster } from "@/components/ui/toaster.tsx"
import { pb, updateUserSettings, verifyAuth } from "@/lib/api.ts"
import { dynamicActivate, getLocale } from "@/lib/i18n"
import { $authenticated, $copyContent, $direction, $userSettings, defaultLayoutWidth } from "@/lib/stores.ts"

const LoginPage = lazy(() => import("@/components/login/login.tsx"))
const Home = lazy(() => import("@/components/routes/home.tsx"))
const NotificationsPage = lazy(() => import("@/components/routes/notifications.tsx"))
const MonitorsPage = lazy(() => import("@/components/routes/monitors.tsx"))
const MonitorDetailPage = lazy(() => import("@/components/routes/monitor-detail.tsx"))
const HostsPage = lazy(() => import("@/components/routes/hosts.tsx"))
const HostDetailPage = lazy(() => import("@/components/routes/host-detail.tsx"))
const ContainersPage = lazy(() => import("@/components/routes/containers.tsx"))
const ContainerDetailPage = lazy(() => import("@/components/routes/container-detail.tsx"))
const ImagesPage = lazy(() => import("@/components/routes/images.tsx"))
const CopyToClipboardDialog = lazy(() => import("@/components/copy-to-clipboard.tsx"))

const App = memo(() => {
	const page = useStore($router)

	useEffect(() => {
		// change auth store on auth change
		const unsubscribeAuth = pb.authStore.onChange(() => {
			$authenticated.set(pb.authStore.isValid)
		})
		// verify token is still valid server-side (catches stale tokens after DB reset)
		verifyAuth()
		// get user settings
		updateUserSettings()
		return () => {
			unsubscribeAuth()
		}
	}, [])

	if (!page) {
		return <h1 className="text-3xl text-center my-14">404</h1>
	} else if (page.route === "home") {
		return <Home />
	} else if (page.route === "notifications") {
		return <NotificationsPage />
	} else if (page.route === "hosts") {
		return <HostsPage />
	} else if (page.route === "host") {
		return <HostDetailPage />
	} else if (page.route === "containers") {
		return <ContainersPage />
	} else if (page.route === "container") {
		return <ContainerDetailPage />
	} else if (page.route === "monitors") {
		return <MonitorsPage />
	} else if (page.route === "monitor") {
		return <MonitorDetailPage />
	} else if (page.route === "images") {
		return <ImagesPage />
	} else if (page.route === "settings") {
		return <Settings />
	}
})

const Layout = () => {
	const authenticated = useStore($authenticated)
	const copyContent = useStore($copyContent)
	const direction = useStore($direction)
	const { layoutWidth } = useStore($userSettings, { keys: ["layoutWidth"] })

	useEffect(() => {
		document.documentElement.dir = direction
	}, [direction])

	return (
		<DirectionProvider dir={direction}>
			{!authenticated ? (
				<Suspense>
					<LoginPage />
				</Suspense>
			) : (
				<div style={{ "--container": `${layoutWidth ?? defaultLayoutWidth}px` } as React.CSSProperties}>
					<NotificationLogToasts />
					<AppShell>
						<Suspense>
							<App />
						</Suspense>
						{copyContent && (
							<Suspense>
								<CopyToClipboardDialog content={copyContent} />
							</Suspense>
						)}
					</AppShell>
				</div>
			)}
		</DirectionProvider>
	)
}

const I18nApp = () => {
	useEffect(() => {
		dynamicActivate(getLocale())
	}, [])

	return (
		<I18nProvider i18n={i18n}>
			<ThemeProvider>
				<Layout />
				<Toaster />
			</ThemeProvider>
		</I18nProvider>
	)
}

ReactDOM.createRoot(document.getElementById("app") as HTMLElement).render(
	// strict mode in dev mounts / unmounts components twice
	// and breaks the clipboard dialog
	//<StrictMode>
	<I18nApp />
	//</StrictMode>
)
