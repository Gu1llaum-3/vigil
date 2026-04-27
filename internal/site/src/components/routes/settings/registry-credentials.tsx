import { Trans, useLingui } from "@lingui/react/macro"
import { redirectPage } from "@nanostores/router"
import { Loader2Icon, PencilIcon, PlusIcon, Trash2Icon } from "lucide-react"
import { type FormEvent, memo, useEffect, useState } from "react"
import { $router } from "@/components/router"
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
import { Separator } from "@/components/ui/separator"
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table"
import { toast } from "@/components/ui/use-toast"
import { isAdmin, pb } from "@/lib/api"

const REDACTED = "**REDACTED**"

interface RegistryCredential {
	id: string
	name: string
	registry: string
	username: string
	password: string
	created: string
	updated: string
}

function apiGet<T>(path: string): Promise<T> {
	return pb.send(path, { method: "GET" }) as Promise<T>
}

function apiPost<T>(path: string, body: unknown): Promise<T> {
	return pb.send(path, {
		method: "POST",
		body: JSON.stringify(body),
		headers: { "Content-Type": "application/json" },
	}) as Promise<T>
}

function apiPatch<T>(path: string, body: unknown): Promise<T> {
	return pb.send(path, {
		method: "PATCH",
		body: JSON.stringify(body),
		headers: { "Content-Type": "application/json" },
	}) as Promise<T>
}

async function apiDelete(path: string): Promise<void> {
	await pb.send(path, { method: "DELETE" })
}

const RegistryCredentialsPage = memo(() => {
	const { t } = useLingui()
	const admin = isAdmin()
	const [credentials, setCredentials] = useState<RegistryCredential[]>([])
	const [loading, setLoading] = useState(true)
	const [editing, setEditing] = useState<RegistryCredential | null>(null)
	const [creating, setCreating] = useState(false)

	useEffect(() => {
		if (!admin) {
			redirectPage($router, "settings", { name: "general" })
			return
		}
		apiGet<RegistryCredential[]>("/api/app/registry-credentials")
			.then(setCredentials)
			.catch((error: unknown) => {
				toast({
					title: t`Failed to load credentials`,
					description: (error as Error).message,
					variant: "destructive",
				})
			})
			.finally(() => setLoading(false))
	}, [admin, t])

	if (!admin) return null

	const handleSaved = (saved: RegistryCredential) => {
		setCredentials((current) => {
			const idx = current.findIndex((c) => c.id === saved.id)
			if (idx >= 0) {
				const next = [...current]
				next[idx] = saved
				return next
			}
			return [...current, saved]
		})
	}

	const handleDelete = async (cred: RegistryCredential) => {
		if (!window.confirm(t`Delete credential for ${cred.registry}?`)) return
		try {
			await apiDelete(`/api/app/registry-credentials/${cred.id}`)
			setCredentials((current) => current.filter((c) => c.id !== cred.id))
			toast({ title: t`Credential deleted` })
		} catch (error: unknown) {
			toast({
				title: t`Failed to delete credential`,
				description: (error as Error).message,
				variant: "destructive",
			})
		}
	}

	return (
		<>
			<div className="flex flex-col gap-3 sm:flex-row sm:items-start sm:justify-between">
				<div>
					<h3 className="text-xl font-medium mb-1">
						<Trans>Registry credentials</Trans>
					</h3>
					<p className="text-sm text-muted-foreground leading-relaxed max-w-prose">
						<Trans>
							Credentials used by the hub to authenticate against private container registries during image audits.
							Passwords are encrypted at rest with a key stored in the data directory. The hub falls back to the host's{" "}
							<code>~/.docker/config.json</code> for registries not listed here.
						</Trans>
					</p>
				</div>
				<Button onClick={() => setCreating(true)} className="shrink-0">
					<PlusIcon className="mr-2 size-4" />
					<Trans>Add credential</Trans>
				</Button>
			</div>
			<Separator className="my-4" />
			{loading ? (
				<div className="flex items-center gap-2 text-sm text-muted-foreground">
					<Loader2Icon className="size-4 animate-spin" />
					<Trans>Loading credentials…</Trans>
				</div>
			) : credentials.length === 0 ? (
				<p className="text-sm text-muted-foreground">
					<Trans>No credentials configured. Add one to authenticate against a private registry.</Trans>
				</p>
			) : (
				<Table>
					<TableHeader>
						<TableRow>
							<TableHead>
								<Trans>Name</Trans>
							</TableHead>
							<TableHead>
								<Trans>Registry</Trans>
							</TableHead>
							<TableHead>
								<Trans>Username</Trans>
							</TableHead>
							<TableHead className="text-right">
								<Trans>Actions</Trans>
							</TableHead>
						</TableRow>
					</TableHeader>
					<TableBody>
						{credentials.map((cred) => (
							<TableRow key={cred.id}>
								<TableCell className="font-medium">{cred.name}</TableCell>
								<TableCell className="font-mono text-xs">{cred.registry}</TableCell>
								<TableCell>{cred.username}</TableCell>
								<TableCell className="text-right">
									<Button variant="ghost" size="icon" onClick={() => setEditing(cred)}>
										<PencilIcon className="size-4" />
									</Button>
									<Button
										variant="ghost"
										size="icon"
										onClick={() => handleDelete(cred)}
										className="text-destructive hover:text-destructive"
									>
										<Trash2Icon className="size-4" />
									</Button>
								</TableCell>
							</TableRow>
						))}
					</TableBody>
				</Table>
			)}
			<CredentialDialog
				open={creating || !!editing}
				credential={editing}
				onClose={() => {
					setCreating(false)
					setEditing(null)
				}}
				onSaved={handleSaved}
			/>
		</>
	)
})

function CredentialDialog({
	open,
	credential,
	onClose,
	onSaved,
}: {
	open: boolean
	credential: RegistryCredential | null
	onClose: () => void
	onSaved: (saved: RegistryCredential) => void
}) {
	const { t } = useLingui()
	const [name, setName] = useState("")
	const [registry, setRegistry] = useState("")
	const [username, setUsername] = useState("")
	const [password, setPassword] = useState("")
	const [saving, setSaving] = useState(false)

	useEffect(() => {
		if (open) {
			setName(credential?.name ?? "")
			setRegistry(credential?.registry ?? "")
			setUsername(credential?.username ?? "")
			setPassword("")
		}
	}, [open, credential])

	const handleSubmit = async (e: FormEvent) => {
		e.preventDefault()
		const trimmedName = name.trim()
		const trimmedRegistry = registry.trim()
		const trimmedUsername = username.trim()
		if (!trimmedName || !trimmedRegistry || !trimmedUsername) return
		setSaving(true)
		try {
			const body: Record<string, string> = {
				name: trimmedName,
				registry: trimmedRegistry,
				username: trimmedUsername,
			}
			if (password) body.password = password
			else if (credential) body.password = REDACTED

			const saved = credential
				? await apiPatch<RegistryCredential>(`/api/app/registry-credentials/${credential.id}`, body)
				: await apiPost<RegistryCredential>("/api/app/registry-credentials", body)
			onSaved(saved)
			toast({ title: credential ? t`Credential updated` : t`Credential added` })
			onClose()
		} catch (error: unknown) {
			toast({
				title: t`Failed to save credential`,
				description: (error as Error).message,
				variant: "destructive",
			})
		} finally {
			setSaving(false)
		}
	}

	return (
		<Dialog open={open} onOpenChange={(next) => !next && onClose()}>
			<DialogContent className="w-[90%] sm:max-w-[32rem] rounded-lg">
				<DialogHeader>
					<DialogTitle>{credential ? t`Edit credential` : t`Add credential`}</DialogTitle>
					<DialogDescription>
						<Trans>
							Use the registry hostname (e.g. <code>ghcr.io</code>, <code>harbor.example.com</code>,{" "}
							<code>docker.io</code>). One credential per registry.
						</Trans>
					</DialogDescription>
				</DialogHeader>
				<form onSubmit={handleSubmit} className="space-y-4">
					<div className="space-y-2">
						<Label htmlFor="cred-name">
							<Trans>Name</Trans>
						</Label>
						<Input id="cred-name" value={name} onChange={(e) => setName(e.target.value)} autoFocus />
					</div>
					<div className="space-y-2">
						<Label htmlFor="cred-registry">
							<Trans>Registry</Trans>
						</Label>
						<Input
							id="cred-registry"
							value={registry}
							onChange={(e) => setRegistry(e.target.value)}
							placeholder="ghcr.io"
						/>
					</div>
					<div className="space-y-2">
						<Label htmlFor="cred-username">
							<Trans>Username</Trans>
						</Label>
						<Input id="cred-username" value={username} onChange={(e) => setUsername(e.target.value)} />
					</div>
					<div className="space-y-2">
						<Label htmlFor="cred-password">
							<Trans>Password or token</Trans>
						</Label>
						<Input
							id="cred-password"
							type="password"
							value={password}
							onChange={(e) => setPassword(e.target.value)}
							placeholder={credential ? t`Leave blank to keep current` : ""}
							autoComplete="new-password"
						/>
					</div>
					<DialogFooter>
						<Button type="button" variant="outline" onClick={onClose} disabled={saving}>
							<Trans>Cancel</Trans>
						</Button>
						<Button
							type="submit"
							disabled={saving || !name.trim() || !registry.trim() || !username.trim() || (!credential && !password)}
						>
							{saving ? <Loader2Icon className="mr-2 size-4 animate-spin" /> : null}
							<Trans>Save</Trans>
						</Button>
					</DialogFooter>
				</form>
			</DialogContent>
		</Dialog>
	)
}

export default RegistryCredentialsPage
