import { t } from "@lingui/core/macro"
import { Trans, useLingui } from "@lingui/react/macro"
import { useStore } from "@nanostores/react"
import { getPagePath, redirectPage } from "@nanostores/router"
import { BellIcon, BotIcon, Clock3Icon, KeyRoundIcon, SettingsIcon, Trash2Icon } from "lucide-react"
import { lazy, useEffect } from "react"
import { PageHeader } from "@/components/page-header"
import { $router } from "@/components/router.tsx"
import { toast } from "@/components/ui/use-toast.ts"
import { pb } from "@/lib/api"
import { $userSettings } from "@/lib/stores.ts"
import type { UserSettings } from "@/types"
import { SidebarNav } from "./sidebar-nav.tsx"

const generalSettingsImport = () => import("./general.tsx")
const agentsSettingsImport = () => import("./agents.tsx")
const notificationsSettingsImport = () => import("./notifications.tsx")
const jobsSettingsImport = () => import("./jobs.tsx")
const purgeSettingsImport = () => import("./purge.tsx")
const registryCredentialsSettingsImport = () => import("./registry-credentials.tsx")

const GeneralSettings = lazy(generalSettingsImport)
const AgentsSettings = lazy(agentsSettingsImport)
const NotificationsSettings = lazy(notificationsSettingsImport)
const JobsSettings = lazy(jobsSettingsImport)
const PurgeSettings = lazy(purgeSettingsImport)
const RegistryCredentialsSettings = lazy(registryCredentialsSettingsImport)

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
			title: t`Registry credentials`,
			href: getPagePath($router, "settings", { name: "registry-credentials" }),
			icon: KeyRoundIcon,
			admin: true,
			preload: registryCredentialsSettingsImport,
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

	useEffect(() => {
		document.title = `${t`Settings`} / App`
		// @ts-expect-error redirect to account page if no page is specified
		if (!page?.params?.name) {
			redirectPage($router, "settings", { name: "general" })
		}
	}, [])

	return (
		<div className="space-y-5 pb-10">
			<PageHeader
				icon={SettingsIcon}
				title={<Trans>Settings</Trans>}
				description={<Trans>Manage display preferences.</Trans>}
			/>
			<div className="rounded-lg border border-border/60 bg-card p-4 sm:p-6">
				<div className="flex flex-col gap-3.5 md:flex-row md:gap-5 lg:gap-12">
					<aside className="md:max-w-52 min-w-40">
						<SidebarNav items={sidebarNavItems} />
					</aside>
					<div className="flex-1 min-w-0">
						{/* @ts-ignore */}
						<SettingsContent name={page?.params?.name ?? "general"} />
					</div>
				</div>
			</div>
		</div>
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
		case "registry-credentials":
			return <RegistryCredentialsSettings />
		case "purge":
			return <PurgeSettings />
	}
}
