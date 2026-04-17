import { t } from "@lingui/core/macro"
import { Trans } from "@lingui/react/macro"
import { getPagePath } from "@nanostores/router"
import { ChevronDownIcon } from "lucide-react"
import { memo, useEffect, useMemo, useState } from "react"
import {
	copyBinaryEnvCommand,
	copyDockerCompose,
	copyDockerRun,
	copyInstallScriptCommand,
	type DropdownItem,
	InstallDropdown,
} from "@/components/agent-install-dropdowns"
import { $router, Link } from "@/components/router"
import { Button } from "@/components/ui/button"
import {
	Dialog,
	DialogContent,
	DialogDescription,
	DialogFooter,
	DialogHeader,
	DialogTitle,
} from "@/components/ui/dialog"
import { DropdownMenu, DropdownMenuTrigger } from "@/components/ui/dropdown-menu"
import { InputCopy } from "@/components/ui/input-copy"
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs"
import { AppleIcon, DockerIcon, TuxIcon } from "@/components/ui/icons"
import { isReadOnlyUser, pb } from "@/lib/api"
import { getHubURL } from "@/lib/utils"
import type { AppInfo } from "@/types"

type EnrollmentTokenResponse = {
	token: string
	active: boolean
	permanent: boolean
}

export function AddAgentDialog({ open, setOpen }: { open: boolean; setOpen: (open: boolean) => void }) {
	if (isReadOnlyUser()) {
		return null
	}

	return (
		<Dialog open={open} onOpenChange={setOpen}>
			<AgentDialog setOpen={setOpen} open={open} />
		</Dialog>
	)
}

const AgentDialog = ({ open, setOpen }: { open: boolean; setOpen: (open: boolean) => void }) => {
	const [tab, setTab] = useState("docker")
	const [loading, setLoading] = useState(false)
	const [enabling, setEnabling] = useState(false)
	const [publicKey, setPublicKey] = useState("")
	const [token, setToken] = useState("")
	const [tokenActive, setTokenActive] = useState(false)
	const [error, setError] = useState("")

	useEffect(() => {
		if (!open) {
			return
		}

		let cancelled = false
		setLoading(true)
		setError("")

		Promise.all([
			pb.send<AppInfo>("/api/app/info"),
			pb.send<EnrollmentTokenResponse>("/api/app/agent-enrollment-token"),
		])
			.then(([info, enrollment]) => {
				if (cancelled) {
					return
				}
				setPublicKey(info.key)
				setToken(enrollment.token)
				setTokenActive(enrollment.active)
			})
			.catch((err: Error) => {
				if (!cancelled) {
					setError(err.message)
				}
			})
			.finally(() => {
				if (!cancelled) {
					setLoading(false)
				}
			})

		return () => {
			cancelled = true
		}
	}, [open])

	async function enableEnrollmentToken() {
		setEnabling(true)
		setError("")
		try {
			const enrollment = await pb.send<EnrollmentTokenResponse>("/api/app/agent-enrollment-token", {
				query: {
					token,
					enable: 1,
				},
			})
			setToken(enrollment.token)
			setTokenActive(enrollment.active)
		} catch (err) {
			setError((err as Error).message)
		} finally {
			setEnabling(false)
		}
	}

	const dockerItems = useMemo<DropdownItem[]>(
		() => [
			{
				text: t({ message: "Copy docker run", context: "Button to copy docker run command" }),
				onClick: () => copyDockerRun(publicKey, token),
				icons: [DockerIcon],
			},
		],
		[publicKey, token]
	)

	const binaryItems = useMemo<DropdownItem[]>(
		() => [
			{
				text: t({ message: "Copy raw command", context: "Button to copy raw binary command" }),
				onClick: () => copyBinaryEnvCommand(publicKey, token),
				icons: [TuxIcon, AppleIcon],
			},
		],
		[publicKey, token]
	)

	const commandsReady = tokenActive && publicKey !== "" && token !== ""

	return (
		<DialogContent className="w-[90%] sm:w-auto sm:max-w-[42rem] rounded-lg">
			<Tabs value={tab} onValueChange={setTab}>
				<DialogHeader>
					<DialogTitle className="mb-1 pb-1 max-w-100 truncate pr-8">
						<Trans>Add agent</Trans>
					</DialogTitle>
					<TabsList className="grid w-full grid-cols-2">
						<TabsTrigger value="docker">Docker</TabsTrigger>
						<TabsTrigger value="binary">
							<Trans>Binary</Trans>
						</TabsTrigger>
					</TabsList>
				</DialogHeader>
				<TabsContent value="docker" tabIndex={-1}>
					<DialogDescription className="mb-3 leading-relaxed w-0 min-w-full">
						<Trans>
							Copy a Docker-based install snippet for a new agent. The dialog uses the hub public key and the current
							enrollment token.
						</Trans>
					</DialogDescription>
				</TabsContent>
				<TabsContent value="binary" tabIndex={-1}>
					<DialogDescription className="mb-3 leading-relaxed w-0 min-w-full">
						<Trans>
							Copy the install script command or a raw binary launch command for a new agent. The dialog uses the hub
							public key and the current enrollment token.
						</Trans>
					</DialogDescription>
				</TabsContent>
				<div className="grid xs:grid-cols-[auto_1fr] gap-y-3 gap-x-4 items-center mt-1 mb-4">
					<div className="xs:col-span-2 rounded-md border bg-muted/30 px-4 py-3 text-sm text-muted-foreground">
						{loading ? (
							<Trans>Loading installation details...</Trans>
						) : tokenActive ? (
							<Trans>
								Enrollment token is active. You can also manage it from{" "}
								<Link
									href={getPagePath($router, "settings", { name: "agents" })}
									className="link"
									onClick={() => setOpen(false)}
								>
									<Trans>Settings &gt; Agents</Trans>
								</Link>
								.
							</Trans>
						) : (
							<div className="flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
								<p>
									<Trans>Enable the enrollment token to generate ready-to-use install commands.</Trans>
								</p>
								<Button type="button" variant="outline" onClick={enableEnrollmentToken} disabled={enabling || loading}>
									<Trans>Enable enrollment token</Trans>
								</Button>
							</div>
						)}
					</div>
					{error && <p className="xs:col-span-2 text-sm text-destructive">{error}</p>}
					<label htmlFor="agent-hub-url" className="xs:text-end whitespace-pre">
						<Trans>Hub URL</Trans>
					</label>
					<InputCopy value={getHubURL()} id="agent-hub-url" name="agent-hub-url" />
					<label htmlFor="agent-public-key" className="xs:text-end whitespace-pre">
						<Trans comment="Use 'Key' if your language requires many more characters">Public Key</Trans>
					</label>
					<InputCopy value={publicKey} id="agent-public-key" name="agent-public-key" />
					<label htmlFor="agent-token" className="xs:text-end whitespace-pre">
						<Trans>Enrollment token</Trans>
					</label>
					<InputCopy value={token} id="agent-token" name="agent-token" />
				</div>
				<DialogFooter className="flex justify-end gap-x-2 gap-y-3 flex-col mt-5">
					<TabsContent value="docker" className="contents">
						<CopyButton
							text={t({ message: "Copy docker compose", context: "Button to copy docker compose file content" })}
							onClick={() => copyDockerCompose(publicKey, token)}
							dropdownItems={dockerItems}
							icon={<DockerIcon className="size-4 -me-0.5" />}
							disabled={!commandsReady}
						/>
					</TabsContent>
					<TabsContent value="binary" className="contents">
						<CopyButton
							text={t({ message: "Copy install script", context: "Button to copy install script command" })}
							onClick={() => copyInstallScriptCommand(publicKey, token)}
							dropdownItems={binaryItems}
							icon={<TuxIcon className="size-4" />}
							disabled={!commandsReady}
						/>
					</TabsContent>
				</DialogFooter>
			</Tabs>
		</DialogContent>
	)
}

interface CopyButtonProps {
	text: string
	onClick: () => void
	dropdownItems: DropdownItem[]
	icon?: React.ReactElement
	disabled?: boolean
}

const CopyButton = memo((props: CopyButtonProps) => {
	return (
		<div className="flex gap-0 rounded-lg">
			<Button
				type="button"
				variant="outline"
				onClick={props.onClick}
				disabled={props.disabled}
				className="rounded-e-none dark:border-e-0 grow flex items-center gap-2"
			>
				{props.text} {props.icon}
			</Button>
			<div className="w-px h-full bg-muted"></div>
			<DropdownMenu>
				<DropdownMenuTrigger asChild>
					<Button variant="outline" className="px-2 rounded-s-none border-s-0" disabled={props.disabled}>
						<ChevronDownIcon />
					</Button>
				</DropdownMenuTrigger>
				<InstallDropdown items={props.dropdownItems} />
			</DropdownMenu>
		</div>
	)
})
