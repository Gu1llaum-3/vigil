import { t } from "@lingui/core/macro"
import { Trans, useLingui } from "@lingui/react/macro"
import { redirectPage } from "@nanostores/router"
import {
	BotIcon,
	CheckIcon,
	CopyIcon,
	FingerprintIcon,
	KeyIcon,
	MergeIcon,
	MoreHorizontalIcon,
	RotateCwIcon,
	ShieldAlertIcon,
	ShieldCheckIcon,
	ShieldXIcon,
	TagIcon,
	Trash2Icon,
	XIcon,
} from "lucide-react"
import { memo, useCallback, useEffect, useMemo, useState } from "react"
import { $router } from "@/components/router"
import { HostTags } from "@/components/host-tags"
import { TagsDialog } from "@/components/tags-dialog"
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
import { isAdmin, isReadOnlyUser, pb } from "@/lib/api"
import { copyToClipboard, getHubURL } from "@/lib/utils"
import type { AgentRecord } from "@/types"

const pbAgentOptions = {
	fields: "id,name,token,fingerprint,status,version,last_seen,tags,token_issued,duplicate_of",
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
	// Per-agent tokens are fetched from a dedicated endpoint: the token field is hidden on
	// the agents collection so it is not exposed to every authenticated user.
	const [tokens, setTokens] = useState<Record<string, string>>({})

	const fetchTokens = useCallback(() => {
		pb.send("/api/app/agent-tokens", { method: "GET" })
			.then((data) => setTokens((data ?? {}) as Record<string, string>))
			.catch(() => {})
	}, [])

	// Get agent records on mount
	useEffect(() => {
		pb.collection("agents")
			.getFullList<AgentRecord>(pbAgentOptions)
			.then((list) => {
				setAgents(sortAgents(list))
			})
		fetchTokens()
	}, [fetchTokens])

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
					// Tokens are not part of the (hidden-field) realtime payload, so refresh the
					// token map after any change. This covers new agents AND token rotation done
					// in another session (a rotation is an `update`, and since the token field is
					// hidden we can't tell it apart from other updates — so refetch on all events
					// rather than risk showing a stale, no-longer-valid token cross-session).
					fetchTokens()
				},
				pbAgentOptions
			)
		})()
		return () => unsubscribe?.()
	}, [fetchTokens])

	return (
		<>
			<SectionIntro />
			<Separator className="my-4" />
			<SectionEnrollmentToken />
			<Separator className="my-4" />
			<SectionTable agents={agents} tokens={tokens} />
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

	function applyState(data: { token: string; active: boolean; permanent?: boolean }) {
		setToken(data.token)
		setChecked(data.active)
		setIsPermanent(!!data.permanent)
		setIsLoading(false)
	}

	function showError(error: unknown) {
		setIsLoading(false)
		toast({ title: t`Error`, description: (error as Error).message, variant: "destructive" })
	}

	// The hub mints the token; regenerate replaces (and so revokes) the current value.
	async function updateToken(enable: boolean, permanent: boolean, regenerate = false) {
		try {
			applyState(
				await pb.send(`/api/app/agent-enrollment-token`, {
					method: "POST",
					body: { enable, permanent, regenerate },
				})
			)
		} catch (error) {
			showError(error)
		}
	}

	useEffect(() => {
		pb.send(`/api/app/agent-enrollment-token`, { method: "GET" }).then(applyState).catch(showError)
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
									updateToken(checked, isPermanent)
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
								<>
									<Button
										variant="ghost"
										size="icon"
										title={t`Regenerate (revokes the current token)`}
										onClick={() => updateToken(true, isPermanent, true)}
									>
										<RotateCwIcon className="w-4 h-4" />
									</Button>
									<Button variant="ghost" size="icon" onClick={() => copyToClipboard(token)}>
										<CopyIcon className="w-4 h-4" />
									</Button>
								</>
							)}
						</div>

						{checked && (
							<div className="border-t pt-3">
								<div className="text-sm font-medium">
									<Trans>Persistence</Trans>
								</div>
								<Tabs
									value={isPermanent ? "permanent" : "ephemeral"}
									onValueChange={(value) => updateToken(true, value === "permanent")}
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
		case "awaiting_approval":
			return <ShieldAlertIcon className="size-4 text-amber-600" />
		default:
			return <span className="size-4 rounded-full bg-muted-foreground/60" />
	}
}

const SectionTable = memo(({ agents = [], tokens = {} }: { agents: AgentRecord[]; tokens: Record<string, string> }) => {
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
			{
				label: t`Tags`,
				Icon: TagIcon,
				w: "14em",
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
								{i === 0 || i === 2 || i === 3 || i === 4 ? (
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
							<TableCell className="font-medium ps-5 py-2 max-w-60 truncate">
								{displayNames.get(agent.id)}
								{agent.status === "awaiting_approval" && (
									<div className="text-xs font-normal text-amber-700 dark:text-amber-500 whitespace-normal">
										<Trans>
											Awaiting approval: claims to be {displayNames.get(agent.duplicate_of ?? "") ?? agent.duplicate_of}
										</Trans>
									</div>
								)}
							</TableCell>
							<TableCell className="py-2">
								<span className="inline-flex items-center gap-2" title={agent.status || "unknown"}>
									{getStatusIcon(agent.status)}
									<span className="sr-only">{agent.status || "unknown"}</span>
								</span>
							</TableCell>
							<TableCell className="font-mono text-[0.95em] py-2">
								{tokens[agent.id] ?? ""}
								{!agent.token_issued && agent.status !== "awaiting_approval" && (
									<span
										className="ms-2 rounded border px-1 font-sans text-xs text-amber-700 dark:text-amber-500"
										title={t`This token was not issued by the hub for this host (it may be shared, e.g. an enrollment token): anyone holding it can present this host's fingerprint. Upgrade the agent, or rotate the token.`}
									>
										<Trans>shared</Trans>
									</span>
								)}
							</TableCell>
							<TableCell className="font-mono text-[0.95em] py-2">{agent.fingerprint}</TableCell>
							<TableCell className="py-2">
								{agent.tags && agent.tags.length > 0 ? (
									<HostTags tags={agent.tags} variant="wrap" />
								) : (
									<span className="text-muted-foreground">—</span>
								)}
							</TableCell>
							{!isReadOnly && (
								<TableCell className="py-2 px-4 xl:px-2">
									<ActionsButtonTable agent={agent} token={tokens[agent.id] ?? ""} />
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
		if (rotateToken) {
			// Token rotation is server-side (cryptographically strong; the token field is
			// hidden on the collection so it cannot be set from the client).
			const rotated = await pb.send<{ token: string; pushed: boolean }>(`/api/app/agents/${agent.id}/rotate-token`, {
				method: "POST",
			})
			toast({
				title: t`Token rotated`,
				description: rotated.pushed
					? t`The agent received its new token.`
					: t`The agent is offline or too old to receive it: reconfigure it with the new token.`,
			})
		}
		if (resetFingerprint) {
			await pb.collection("agents").update(agent.id, { fingerprint: "" })
		}
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
// Decisions on a host awaiting approval (admin only): it connected with the enrollment token
// using the fingerprint of a host that has its own token.
async function decideAgent(agent: AgentRecord, decision: "merge" | "approve" | "reject") {
	const question = {
		merge: t`Merge this host into the host it claims to be? Only do this if you reinstalled that host: it will take over its record, history and token.`,
		approve: t`Approve this host as a new, separate host? Only do this if it is a different machine (for example one sharing the hostname).`,
		reject: t`Reject this host? Its record is deleted. If it was not yours, also regenerate the enrollment token.`,
	}[decision]
	if (!window.confirm(question)) {
		return
	}
	try {
		await pb.send(`/api/app/agents/${agent.id}/${decision}`, { method: "POST" })
	} catch (caught: unknown) {
		let error = caught
		const status = (error as { status?: number }).status
		// The host it claims to be is connected right now: the strongest sign of impersonation.
		if (
			decision === "merge" &&
			status === 409 &&
			window.confirm(
				t`The host it claims to be is connected right now, so this one is probably not a reinstall of it. Merge anyway and disconnect the current one?`
			)
		) {
			try {
				await pb.send(`/api/app/agents/${agent.id}/merge`, { method: "POST", query: { force: 1 } })
				return
			} catch (forced: unknown) {
				error = forced
			}
		}
		toast({ title: t`Error`, description: (error as Error).message, variant: "destructive" })
	}
}

const ActionsButtonTable = memo(({ agent, token }: { agent: AgentRecord; token: string }) => {
	const envVar = `HUB_URL=${getHubURL()}\nTOKEN=${token}`
	const copyEnv = () => copyToClipboard(envVar)
	const copyYaml = () => copyToClipboard(envVar.replaceAll("=", ": "))
	const [tagsOpen, setTagsOpen] = useState(false)

	return (
		<>
			<TagsDialog
				agentId={agent.id}
				currentTags={agent.tags ?? []}
				open={tagsOpen}
				onClose={() => setTagsOpen(false)}
			/>
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
					{agent.status === "awaiting_approval" && isAdmin() && (
						<>
							<DropdownMenuItem onSelect={() => decideAgent(agent, "merge")}>
								<MergeIcon className="me-2.5 size-4" />
								<Trans>Merge into the host it claims to be (reinstall)</Trans>
							</DropdownMenuItem>
							<DropdownMenuItem onSelect={() => decideAgent(agent, "approve")}>
								<ShieldCheckIcon className="me-2.5 size-4" />
								<Trans>Approve as a new host</Trans>
							</DropdownMenuItem>
							<DropdownMenuItem onSelect={() => decideAgent(agent, "reject")} className="text-destructive">
								<ShieldXIcon className="me-2.5 size-4" />
								<Trans>Reject</Trans>
							</DropdownMenuItem>
							<DropdownMenuSeparator />
						</>
					)}
					<DropdownMenuItem onClick={copyYaml}>
						<CopyIcon className="me-2.5 size-4" />
						<Trans>Copy YAML</Trans>
					</DropdownMenuItem>
					<DropdownMenuItem onClick={copyEnv}>
						<CopyIcon className="me-2.5 size-4" />
						<Trans context="Environment variables">Copy env</Trans>
					</DropdownMenuItem>
					<DropdownMenuSeparator />
					<DropdownMenuItem onSelect={() => setTagsOpen(true)}>
						<TagIcon className="me-2.5 size-4" />
						<Trans>Edit tags</Trans>
					</DropdownMenuItem>
					<DropdownMenuItem onSelect={() => updateAgent(agent, true)}>
						<RotateCwIcon className="me-2.5 size-4" />
						<Trans>Rotate token</Trans>
					</DropdownMenuItem>
					{agent.fingerprint && (isAdmin() || !agent.token_issued) && (
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
		</>
	)
})

export default SettingsAgentsPage
