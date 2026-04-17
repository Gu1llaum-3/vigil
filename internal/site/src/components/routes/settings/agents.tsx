import { t } from "@lingui/core/macro"
import { Trans, useLingui } from "@lingui/react/macro"
import { redirectPage } from "@nanostores/router"
import {
	BotIcon,
	CheckIcon,
	CopyIcon,
	FingerprintIcon,
	KeyIcon,
	MoreHorizontalIcon,
	RotateCwIcon,
	Trash2Icon,
	XIcon,
} from "lucide-react"
import { memo, useEffect, useMemo, useState } from "react"
import { $router } from "@/components/router"
import { Button } from "@/components/ui/button"
import {
	DropdownMenu,
	DropdownMenuContent,
	DropdownMenuItem,
	DropdownMenuSeparator,
	DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu"
import { Separator } from "@/components/ui/separator"
import { Switch } from "@/components/ui/switch"
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs"
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table"
import { toast } from "@/components/ui/use-toast"
import { isReadOnlyUser, pb } from "@/lib/api"
import { copyToClipboard, generateToken, getHubURL } from "@/lib/utils"
import type { AgentRecord } from "@/types"

const pbAgentOptions = {
	fields: "id,name,token,fingerprint,status,version,last_seen",
}

function sortAgents(agents: AgentRecord[]) {
	return agents.sort((a, b) => (a.name || a.id).localeCompare(b.name || b.id))
}

function getAgentBaseName(agent: AgentRecord) {
	return agent.name || agent.id
}

function getAgentFingerprintSuffix(agent: AgentRecord) {
	if (agent.fingerprint) {
		return agent.fingerprint.slice(0, 8)
	}
	return agent.id.slice(0, 8)
}

function buildAgentDisplayNames(agents: AgentRecord[]) {
	const counts = new Map<string, number>()
	for (const agent of agents) {
		const baseName = getAgentBaseName(agent)
		counts.set(baseName, (counts.get(baseName) ?? 0) + 1)
	}

	return new Map(
		agents.map((agent) => {
			const baseName = getAgentBaseName(agent)
			const hasDuplicateName = !!agent.name && (counts.get(baseName) ?? 0) > 1
			const displayName = hasDuplicateName ? `${baseName} · ${getAgentFingerprintSuffix(agent)}` : baseName
			return [agent.id, displayName]
		})
	)
}

const SettingsAgentsPage = memo(() => {
	if (isReadOnlyUser()) {
		redirectPage($router, "settings", { name: "general" })
	}
	const [agents, setAgents] = useState<AgentRecord[]>([])

	// Get agent records on mount
	useEffect(() => {
		pb.collection("agents")
			.getFullList<AgentRecord>(pbAgentOptions)
			.then((list) => {
				setAgents(sortAgents(list))
			})
	}, [])

	// Subscribe to agent updates
	useEffect(() => {
		let unsubscribe: (() => void) | undefined
		;(async () => {
			unsubscribe = await pb.collection("agents").subscribe(
				"*",
				(res) => {
					setAgents((current) => {
						if (res.action === "create") {
							return sortAgents([...current, res.record as AgentRecord])
						}
						if (res.action === "update") {
							return current.map((agent) => {
								if (agent.id === res.record.id) {
									return { ...agent, ...res.record } as AgentRecord
								}
								return agent
							})
						}
						if (res.action === "delete") {
							return current.filter((agent) => agent.id !== res.record.id)
						}
						return current
					})
				},
				pbAgentOptions
			)
		})()
		return () => unsubscribe?.()
	}, [])

	return (
		<>
			<SectionIntro />
			<Separator className="my-4" />
			<SectionEnrollmentToken />
			<Separator className="my-4" />
			<SectionTable agents={agents} />
		</>
	)
})

const SectionIntro = memo(() => {
	return (
		<div>
			<h3 className="text-xl font-medium mb-2">
				<Trans>Agents</Trans>
			</h3>
			<p className="text-sm text-muted-foreground leading-relaxed">
				<Trans>Agents connect to the hub via WebSocket.</Trans>
			</p>
			<p className="text-sm text-muted-foreground leading-relaxed mt-1.5">
				<Trans>
					Each agent authenticates with a token and establishes a stable fingerprint on first connection. Use an
					enrollment token to allow new agents to self-register.
				</Trans>
			</p>
		</div>
	)
})

const SectionEnrollmentToken = memo(() => {
	const [token, setToken] = useState("")
	const [isLoading, setIsLoading] = useState(true)
	const [checked, setChecked] = useState(false)
	const [isPermanent, setIsPermanent] = useState(false)

	async function updateToken(enable: number = -1, permanent: number = -1) {
		const data = await pb.send(`/api/app/agent-enrollment-token`, {
			query: {
				token,
				enable,
				permanent,
			},
		})
		setToken(data.token)
		setChecked(data.active)
		setIsPermanent(!!data.permanent)
		setIsLoading(false)
	}

	useEffect(() => {
		updateToken()
	}, [])

	return (
		<div>
			<h3 className="text-lg font-medium mb-2">
				<Trans>Enrollment token</Trans>
			</h3>
			<p className="text-sm text-muted-foreground leading-relaxed">
				<Trans>When enabled, this token allows agents to self-register without prior creation.</Trans>
			</p>
			<div className="mt-3 border rounded-md px-4 py-3 max-w-full">
				{!isLoading && (
					<div className="flex flex-col gap-3">
						<div className="flex items-center gap-4 min-w-0">
							<Switch
								checked={checked}
								onCheckedChange={(checked) => {
									updateToken(checked ? 1 : 0, isPermanent ? 1 : 0)
								}}
							/>
							<div className="min-w-0 flex-1 overflow-auto">
								<span
									className={`text-sm text-primary opacity-60 transition-opacity${checked ? " opacity-100" : " select-none"}`}
								>
									{token}
								</span>
							</div>
							{checked && (
								<Button variant="ghost" size="icon" onClick={() => copyToClipboard(token)}>
									<CopyIcon className="w-4 h-4" />
								</Button>
							)}
						</div>

						{checked && (
							<div className="border-t pt-3">
								<div className="text-sm font-medium">
									<Trans>Persistence</Trans>
								</div>
								<Tabs
									value={isPermanent ? "permanent" : "ephemeral"}
									onValueChange={(value) => updateToken(1, value === "permanent" ? 1 : 0)}
									className="mt-2"
								>
									<TabsList>
										<TabsTrigger className="xs:min-w-40" value="ephemeral">
											<Trans>Ephemeral</Trans>
										</TabsTrigger>
										<TabsTrigger className="xs:min-w-40" value="permanent">
											<Trans>Permanent</Trans>
										</TabsTrigger>
									</TabsList>
									<TabsContent value="ephemeral" className="mt-3">
										<p className="text-sm text-muted-foreground leading-relaxed">
											<Trans>Expires after one hour or on hub restart.</Trans>
										</p>
									</TabsContent>
									<TabsContent value="permanent" className="mt-3">
										<p className="text-sm text-muted-foreground leading-relaxed">
											<Trans>Saved in the database and does not expire until you disable it.</Trans>
										</p>
									</TabsContent>
								</Tabs>
							</div>
						)}
					</div>
				)}
			</div>
		</div>
	)
})

function getStatusIcon(status: string) {
	switch (status) {
		case "connected":
			return <CheckIcon className="size-4 text-emerald-600" />
		case "offline":
			return <XIcon className="size-4 text-red-600" />
		default:
			return <span className="size-4 rounded-full bg-muted-foreground/60" />
	}
}

const SectionTable = memo(({ agents = [] }: { agents: AgentRecord[] }) => {
	const { t } = useLingui()
	const isReadOnly = isReadOnlyUser()
	const displayNames = useMemo(() => buildAgentDisplayNames(agents), [agents])

	const headerCols = useMemo(
		() => [
			{
				label: t`Agent`,
				Icon: BotIcon,
				w: "10em",
			},
			{
				label: t`Status`,
				Icon: BotIcon,
				w: "7em",
			},
			{
				label: t`Token`,
				Icon: KeyIcon,
				w: "18em",
			},
			{
				label: t`Fingerprint`,
				Icon: FingerprintIcon,
				w: "18em",
			},
		],
		[t]
	)

	return (
		<div className="rounded-md border overflow-hidden w-full mt-4">
			<Table>
				<TableHeader>
					<tr className="border-border/50">
						{headerCols.map((col, i) => (
							<TableHead key={col.label} style={{ minWidth: col.w }}>
								{i === 0 || i === 2 || i === 3 ? (
									<span className="flex items-center gap-2">
										<col.Icon className="size-4" />
										{col.label}
									</span>
								) : (
									col.label
								)}
							</TableHead>
						))}
						{!isReadOnly && (
							<TableHead className="w-0">
								<span className="sr-only">
									<Trans>Actions</Trans>
								</span>
							</TableHead>
						)}
					</tr>
				</TableHeader>
				<TableBody className="whitespace-pre">
					{agents.map((agent) => (
						<TableRow key={agent.id}>
							<TableCell className="font-medium ps-5 py-2 max-w-60 truncate">{displayNames.get(agent.id)}</TableCell>
							<TableCell className="py-2">
								<span className="inline-flex items-center gap-2" title={agent.status || "unknown"}>
									{getStatusIcon(agent.status)}
									<span className="sr-only">{agent.status || "unknown"}</span>
								</span>
							</TableCell>
							<TableCell className="font-mono text-[0.95em] py-2">{agent.token}</TableCell>
							<TableCell className="font-mono text-[0.95em] py-2">{agent.fingerprint}</TableCell>
							{!isReadOnly && (
								<TableCell className="py-2 px-4 xl:px-2">
									<ActionsButtonTable agent={agent} />
								</TableCell>
							)}
						</TableRow>
					))}
				</TableBody>
			</Table>
		</div>
	)
})

async function updateAgent(agent: AgentRecord, rotateToken = false, resetFingerprint = false) {
	try {
		const patch: Partial<AgentRecord> = {}
		if (rotateToken) {
			patch.token = generateToken()
		}
		if (resetFingerprint) {
			patch.fingerprint = ""
		}
		await pb.collection("agents").update(agent.id, patch)
	} catch (error: unknown) {
		toast({
			title: t`Error`,
			description: (error as Error).message,
		})
	}
}

async function deleteAgent(agent: AgentRecord) {
	try {
		await pb.collection("agents").delete(agent.id)
	} catch (error: unknown) {
		toast({
			title: t`Error`,
			description: (error as Error).message,
		})
	}
}

const ActionsButtonTable = memo(({ agent }: { agent: AgentRecord }) => {
	const envVar = `HUB_URL=${getHubURL()}\nTOKEN=${agent.token}`
	const copyEnv = () => copyToClipboard(envVar)
	const copyYaml = () => copyToClipboard(envVar.replaceAll("=", ": "))

	return (
		<DropdownMenu>
			<DropdownMenuTrigger asChild>
				<Button variant="ghost" size={"icon"} data-nolink>
					<span className="sr-only">
						<Trans>Open menu</Trans>
					</span>
					<MoreHorizontalIcon className="w-5" />
				</Button>
			</DropdownMenuTrigger>
			<DropdownMenuContent align="end">
				<DropdownMenuItem onClick={copyYaml}>
					<CopyIcon className="me-2.5 size-4" />
					<Trans>Copy YAML</Trans>
				</DropdownMenuItem>
				<DropdownMenuItem onClick={copyEnv}>
					<CopyIcon className="me-2.5 size-4" />
					<Trans context="Environment variables">Copy env</Trans>
				</DropdownMenuItem>
				<DropdownMenuSeparator />
				<DropdownMenuItem onSelect={() => updateAgent(agent, true)}>
					<RotateCwIcon className="me-2.5 size-4" />
					<Trans>Rotate token</Trans>
				</DropdownMenuItem>
				{agent.fingerprint && (
					<DropdownMenuItem onSelect={() => updateAgent(agent, false, true)}>
						<Trash2Icon className="me-2.5 size-4" />
						<Trans>Reset fingerprint</Trans>
					</DropdownMenuItem>
				)}
				<DropdownMenuSeparator />
				<DropdownMenuItem onSelect={() => deleteAgent(agent)} className="text-destructive">
					<Trash2Icon className="me-2.5 size-4" />
					<Trans>Delete agent</Trans>
				</DropdownMenuItem>
			</DropdownMenuContent>
		</DropdownMenu>
	)
})

export default SettingsAgentsPage
