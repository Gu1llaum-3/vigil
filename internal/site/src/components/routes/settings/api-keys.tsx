import { Trans, useLingui } from "@lingui/react/macro"
import { CopyIcon, KeyRoundIcon, Loader2Icon, PlusIcon, Trash2Icon } from "lucide-react"
import { type FormEvent, memo, useEffect, useState } from "react"
import { Button } from "@/components/ui/button"
import {
	Dialog,
	DialogContent,
	DialogDescription,
	DialogFooter,
	DialogHeader,
	DialogTitle,
} from "@/components/ui/dialog"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table"
import { toast } from "@/components/ui/use-toast"
import { pb } from "@/lib/api"
import { formatDateTime } from "@/lib/format"
import { copyToClipboard } from "@/lib/utils"

interface ApiKey {
	id: string
	name: string
	prefix: string
	scope: string
	last_used_at: string
	expires_at: string
	created: string
}

export default memo(function ApiKeysSettings() {
	const { t } = useLingui()
	const [keys, setKeys] = useState<ApiKey[]>([])
	const [loading, setLoading] = useState(true)
	const [creating, setCreating] = useState(false)
	const [newName, setNewName] = useState("")
	const [dialogOpen, setDialogOpen] = useState(false)
	// The plaintext token, shown exactly once right after creation.
	const [freshToken, setFreshToken] = useState<string | null>(null)

	async function load() {
		try {
			const data = await pb.send<ApiKey[]>("/api/app/api-keys", { method: "GET" })
			setKeys(data)
		} catch (e) {
			toast({ title: t`Failed to load API keys`, description: String(e), variant: "destructive" })
		} finally {
			setLoading(false)
		}
	}

	useEffect(() => {
		load()
		// eslint-disable-next-line react-hooks/exhaustive-deps
	}, [])

	async function createKey(e: FormEvent) {
		e.preventDefault()
		if (!newName.trim()) return
		setCreating(true)
		try {
			// v1 issues read-only keys; read-write is a future option.
			const res = await pb.send<{ token: string }>("/api/app/api-keys", {
				method: "POST",
				body: JSON.stringify({ name: newName.trim(), scope: "read" }),
				headers: { "Content-Type": "application/json" },
			})
			setFreshToken(res.token)
			setNewName("")
			setDialogOpen(false)
			await load()
		} catch (err) {
			toast({ title: t`Failed to create API key`, description: String(err), variant: "destructive" })
		} finally {
			setCreating(false)
		}
	}

	async function revokeKey(id: string) {
		try {
			await pb.send(`/api/app/api-keys/${id}`, { method: "DELETE" })
			await load()
			toast({ title: t`API key revoked` })
		} catch (err) {
			toast({ title: t`Failed to revoke API key`, description: String(err), variant: "destructive" })
		}
	}

	return (
		<div>
			<div className="mb-4 flex items-start justify-between gap-4">
				<div>
					<h3 className="text-lg font-medium">
						<Trans>API keys</Trans>
					</h3>
					<p className="mt-1 text-sm text-muted-foreground leading-relaxed">
						<Trans>
							Long-lived read-only tokens for scripts and integrations (e.g. the MCP server). A key acts as you and
							can read everything you can; it cannot make changes. The token is shown only once at creation.
						</Trans>
					</p>
				</div>
				<Button onClick={() => setDialogOpen(true)} className="gap-2 shrink-0">
					<PlusIcon className="size-4" />
					<Trans>New key</Trans>
				</Button>
			</div>

			{loading ? (
				<div className="flex h-24 items-center justify-center text-muted-foreground">
					<Loader2Icon className="size-5 animate-spin" />
				</div>
			) : keys.length === 0 ? (
				<div className="flex h-24 items-center justify-center rounded-md border border-dashed border-border/60 text-sm text-muted-foreground">
					<Trans>No API keys yet.</Trans>
				</div>
			) : (
				<Table>
					<TableHeader>
						<TableRow>
							<TableHead>
								<Trans>Name</Trans>
							</TableHead>
							<TableHead>
								<Trans>Token</Trans>
							</TableHead>
							<TableHead>
								<Trans>Last used</Trans>
							</TableHead>
							<TableHead className="w-10" />
						</TableRow>
					</TableHeader>
					<TableBody>
						{keys.map((k) => (
							<TableRow key={k.id}>
								<TableCell className="font-medium">{k.name}</TableCell>
								<TableCell className="font-mono text-xs text-muted-foreground">{k.prefix}…</TableCell>
								<TableCell className="text-sm text-muted-foreground">
									{k.last_used_at ? formatDateTime(k.last_used_at) : <Trans>never</Trans>}
								</TableCell>
								<TableCell>
									<Button
										variant="ghost"
										size="icon"
										title={t`Revoke`}
										onClick={() => revokeKey(k.id)}
									>
										<Trash2Icon className="size-4 text-red-500" />
									</Button>
								</TableCell>
							</TableRow>
						))}
					</TableBody>
				</Table>
			)}

			{/* Create dialog */}
			<Dialog open={dialogOpen} onOpenChange={setDialogOpen}>
				<DialogContent>
					<form onSubmit={createKey}>
						<DialogHeader>
							<DialogTitle>
								<Trans>New API key</Trans>
							</DialogTitle>
							<DialogDescription>
								<Trans>Give the key a name so you can recognize it later.</Trans>
							</DialogDescription>
						</DialogHeader>
						<div className="my-4 grid gap-2">
							<Label htmlFor="api-key-name">
								<Trans>Name</Trans>
							</Label>
							<Input
								id="api-key-name"
								value={newName}
								onChange={(e) => setNewName(e.target.value)}
								placeholder={t`e.g. mcp-server, reporting-script`}
								autoFocus
							/>
						</div>
						<DialogFooter>
							<Button type="submit" disabled={creating || !newName.trim()} className="gap-2">
								{creating && <Loader2Icon className="size-4 animate-spin" />}
								<Trans>Create</Trans>
							</Button>
						</DialogFooter>
					</form>
				</DialogContent>
			</Dialog>

			{/* Show-once token dialog */}
			<Dialog open={Boolean(freshToken)} onOpenChange={(open) => !open && setFreshToken(null)}>
				<DialogContent>
					<DialogHeader>
						<DialogTitle className="flex items-center gap-2">
							<KeyRoundIcon className="size-4" />
							<Trans>Copy your API key now</Trans>
						</DialogTitle>
						<DialogDescription>
							<Trans>This is the only time the full token is shown. Store it somewhere safe — you cannot see it again.</Trans>
						</DialogDescription>
					</DialogHeader>
					<div className="my-4 flex items-center gap-2 rounded-md border border-border/60 bg-muted/40 p-3">
						<code className="min-w-0 flex-1 overflow-auto font-mono text-sm">{freshToken}</code>
						<Button
							variant="ghost"
							size="icon"
							onClick={() => freshToken && copyToClipboard(freshToken)}
							title={t`Copy`}
						>
							<CopyIcon className="size-4" />
						</Button>
					</div>
					<DialogFooter>
						<Button onClick={() => setFreshToken(null)}>
							<Trans>Done</Trans>
						</Button>
					</DialogFooter>
				</DialogContent>
			</Dialog>
		</div>
	)
})
