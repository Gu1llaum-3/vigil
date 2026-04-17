/** biome-ignore-all lint/correctness/useUniqueElementIds: component is only rendered once */
import { Trans, useLingui } from "@lingui/react/macro"
import { LanguagesIcon, LoaderCircleIcon, SaveIcon } from "lucide-react"
import { useState } from "react"
import { useStore } from "@nanostores/react"
import { Button } from "@/components/ui/button"
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"
import { Separator } from "@/components/ui/separator"
import Slider from "@/components/ui/slider"
import { Label } from "@/components/ui/label"
import { HourFormat } from "@/lib/enums"
import { dynamicActivate } from "@/lib/i18n"
import languages from "@/lib/languages"
import { $userSettings, defaultLayoutWidth } from "@/lib/stores"
import { currentHour12 } from "@/lib/utils"
import type { UserSettings } from "@/types"
import { saveSettings } from "./layout"

export default function SettingsProfilePage({ userSettings }: { userSettings: UserSettings }) {
	const [isLoading, setIsLoading] = useState(false)
	const { i18n } = useLingui()
	const currentUserSettings = useStore($userSettings)
	const layoutWidth = currentUserSettings.layoutWidth ?? defaultLayoutWidth

	async function handleSubmit(e: React.FormEvent<HTMLFormElement>) {
		e.preventDefault()
		setIsLoading(true)
		const formData = new FormData(e.target as HTMLFormElement)
		const data = Object.fromEntries(formData) as Partial<UserSettings>
		await saveSettings(data)
		setIsLoading(false)
	}

	return (
		<div>
			<div>
				<h3 className="text-xl font-medium mb-2">
					<Trans>General</Trans>
				</h3>
				<p className="text-sm text-muted-foreground leading-relaxed">
					<Trans>Change general application options.</Trans>
				</p>
			</div>
			<Separator className="my-4" />
			<form onSubmit={handleSubmit} className="space-y-5">
				<div className="grid gap-2">
					<div className="mb-2">
						<h3 className="mb-1 text-lg font-medium flex items-center gap-2">
							<LanguagesIcon className="h-4 w-4" />
							<Trans>Language</Trans>
						</h3>
						<p className="text-sm text-muted-foreground leading-relaxed">
							<Trans>Translations are managed from the project's locale catalogs.</Trans>
						</p>
					</div>
					<Label className="block" htmlFor="lang">
						<Trans>Preferred Language</Trans>
					</Label>
					<Select value={i18n.locale} onValueChange={(lang: string) => dynamicActivate(lang)}>
						<SelectTrigger id="lang">
							<SelectValue />
						</SelectTrigger>
						<SelectContent>
							{languages.map(([lang, label, e]) => (
								<SelectItem key={lang} value={lang}>
									<span className="me-2.5">
										{e || (
											<code
												aria-hidden="true"
												className="font-mono bg-muted text-[.65em] w-5 h-4 inline-grid place-items-center"
											>
												{lang}
											</code>
										)}
									</span>
									{label}
								</SelectItem>
							))}
						</SelectContent>
					</Select>
				</div>
				<Separator />
				<div className="grid gap-2">
					<div className="mb-2">
						<h3 className="mb-1 text-lg font-medium">
							<Trans>Layout width</Trans>
						</h3>
						<Label htmlFor="layoutWidth" className="text-sm text-muted-foreground leading-relaxed">
							<Trans>Adjust the width of the main layout</Trans> ({layoutWidth}px)
						</Label>
					</div>
					<Slider
						id="layoutWidth"
						name="layoutWidth"
						value={[layoutWidth]}
						onValueChange={(val) => $userSettings.setKey("layoutWidth", val[0])}
						min={1000}
						max={2000}
						step={10}
						className="w-full mb-1"
					/>
				</div>
				<Separator />
				<div className="grid gap-2">
					<div className="mb-2">
						<h3 className="mb-1 text-lg font-medium">
							<Trans>Time format</Trans>
						</h3>
					</div>
					<div className="grid sm:grid-cols-3 gap-4">
						<div className="grid gap-2">
							<Label className="block" htmlFor="hourFormat">
								<Trans>Time format</Trans>
							</Label>
							<Select
								name="hourFormat"
								key={userSettings.hourFormat}
								defaultValue={userSettings.hourFormat ?? (currentHour12() ? HourFormat["12h"] : HourFormat["24h"])}
							>
								<SelectTrigger id="hourFormat">
									<SelectValue />
								</SelectTrigger>
								<SelectContent>
									{Object.keys(HourFormat).map((value) => (
										<SelectItem key={value} value={value}>
											{value}
										</SelectItem>
									))}
								</SelectContent>
							</Select>
						</div>
					</div>
				</div>
				<Separator />
				<Button type="submit" className="flex items-center gap-1.5 disabled:opacity-100" disabled={isLoading}>
					{isLoading ? <LoaderCircleIcon className="h-4 w-4 animate-spin" /> : <SaveIcon className="h-4 w-4" />}
					<Trans>Save Settings</Trans>
				</Button>
			</form>
		</div>
	)
}
