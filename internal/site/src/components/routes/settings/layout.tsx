import { t } from "@lingui/core/macro"
import { Trans, useLingui } from "@lingui/react/macro"
import { useStore } from "@nanostores/react"
import { getPagePath, redirectPage } from "@nanostores/router"
import { BellIcon, BotIcon, Clock3Icon, SettingsIcon, Trash2Icon } from "lucide-react"
import { lazy, useEffect } from "react"
import { $router } from "@/components/router.tsx"
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card.tsx"
import { toast } from "@/components/ui/use-toast.ts"
import { pb } from "@/lib/api"
import { $userSettings } from "@/lib/stores.ts"
import type { UserSettings } from "@/types"
import { Separator } from "../../ui/separator"
import { SidebarNav } from "./sidebar-nav.tsx"

const generalSettingsImport = () => import("./general.tsx")
const agentsSettingsImport = () => import("./agents.tsx")
const notificationsSettingsImport = () => import("./notifications.tsx")
const jobsSettingsImport = () => import("./jobs.tsx")
const purgeSettingsImport = () => import("./purge.tsx")

const GeneralSettings = lazy(generalSettingsImport)
const AgentsSettings = lazy(agentsSettingsImport)
const NotificationsSettings = lazy(notificationsSettingsImport)
const JobsSettings = lazy(jobsSettingsImport)
const PurgeSettings = lazy(purgeSettingsImport)

export async function saveSettings(newSettings: Partial<UserSettings>) {
	try {
		// get fresh copy of settings
		const req = await pb.collection("user_settings").getFirstListItem("", {
			fields: "id,settings",
		})
		// update user settings
		const updatedSettings = await pb.collection("user_settings").update(req.id, {
			settings: {
				...req.settings,
				...newSettings,
			},
		})
		$userSettings.set(updatedSettings.settings)
		toast({
			title: t`Settings saved`,
			description: t`Your user settings have been updated.`,
		})
	} catch (e) {
		// console.error('update settings', e)
		toast({
			title: t`Failed to save settings`,
			description: t`Check logs for more details.`,
			variant: "destructive",
		})
	}
}

export default function SettingsLayout() {
	const { t } = useLingui()

	const sidebarNavItems = [
		{
			title: t({ message: `General`, comment: "Context: General settings" }),
			href: getPagePath($router, "settings", { name: "general" }),
			icon: SettingsIcon,
		},
		{
			title: t`Agents`,
			href: getPagePath($router, "settings", { name: "agents" }),
			icon: BotIcon,
			noReadOnly: true,
			preload: agentsSettingsImport,
		},
		{
			title: t`Notifications`,
			href: getPagePath($router, "settings", { name: "notifications" }),
			icon: BellIcon,
			admin: true,
			preload: notificationsSettingsImport,
		},
		{
			title: t`Jobs`,
			href: getPagePath($router, "settings", { name: "jobs" }),
			icon: Clock3Icon,
			admin: true,
			preload: jobsSettingsImport,
		},
		{
			title: t`Purge`,
			href: getPagePath($router, "settings", { name: "purge" }),
			icon: Trash2Icon,
			admin: true,
			preload: purgeSettingsImport,
		},
	]

	const page = useStore($router)

	// biome-ignore lint/correctness/useExhaustiveDependencies: no dependencies
	useEffect(() => {
		document.title = `${t`Settings`} / App`
		// @ts-expect-error redirect to account page if no page is specified
		if (!page?.params?.name) {
			redirectPage($router, "settings", { name: "general" })
		}
	}, [])

	return (
		<Card className="pt-5 px-4 pb-8 min-h-96 mb-14 sm:pt-6 sm:px-7">
			<CardHeader className="p-0">
				<CardTitle className="mb-1">
					<Trans>Settings</Trans>
				</CardTitle>
				<CardDescription>
					<Trans>Manage display preferences.</Trans>
				</CardDescription>
			</CardHeader>
			<CardContent className="p-0">
				<Separator className="hidden md:block my-5" />
				<div className="flex flex-col gap-3.5 md:flex-row md:gap-5 lg:gap-12">
					<aside className="md:max-w-52 min-w-40">
						<SidebarNav items={sidebarNavItems} />
					</aside>
					<div className="flex-1 min-w-0">
						{/* @ts-ignore */}
						<SettingsContent name={page?.params?.name ?? "general"} />
					</div>
				</div>
			</CardContent>
		</Card>
	)
}

function SettingsContent({ name }: { name: string }) {
	const userSettings = useStore($userSettings)

	switch (name) {
		case "general":
			return <GeneralSettings userSettings={userSettings} />
		case "agents":
			return <AgentsSettings />
		case "notifications":
			return <NotificationsSettings />
		case "jobs":
			return <JobsSettings />
		case "purge":
			return <PurgeSettings />
	}
}
