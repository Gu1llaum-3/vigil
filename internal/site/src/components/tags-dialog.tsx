import { Trans, useLingui } from "@lingui/react/macro"
import { XIcon } from "lucide-react"
import { useEffect, useRef, useState } from "react"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Dialog, DialogContent, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { toast } from "@/components/ui/use-toast"
import { pb } from "@/lib/api"

/**
 * Shared free-text tag editor for a single agent/host. Used from the agents
 * settings table, the hosts overview row menu, and the host detail page. `tags`
 * is a plain (non-hidden) field and the agents updateRule already allows
 * non-readonly users, so a direct collection update is sufficient; the consumers'
 * realtime subscriptions to the agents collection refresh the displayed tags.
 */
export function TagsDialog({
	agentId,
	currentTags,
	title,
	open,
	onClose,
}: {
	agentId: string
	currentTags: string[]
	/** Optional heading (e.g. the host name) shown after "Edit tags". */
	title?: string
	open: boolean
	onClose: () => void
}) {
	const { t } = useLingui()
	const [tags, setTags] = useState<string[]>([])
	const [input, setInput] = useState("")
	const [saving, setSaving] = useState(false)

	// Seed from the stored tags only when the dialog opens — not on every
	// currentTags identity change, otherwise an unrelated realtime update mid-edit
	// would wipe the user's unsaved tags.
	const seededRef = useRef(false)
	useEffect(() => {
		if (!open) {
			seededRef.current = false
			return
		}
		if (!seededRef.current) {
			setTags(currentTags)
			setInput("")
			seededRef.current = true
		}
	}, [open, currentTags])

	function addTag() {
		const value = input.trim()
		if (!value) return
		if (!tags.includes(value)) setTags([...tags, value])
		setInput("")
	}

	async function save() {
		setSaving(true)
		try {
			await pb.collection("agents").update(agentId, { tags })
			onClose()
		} catch (error: unknown) {
			toast({ title: t`Error`, description: (error as Error).message })
		} finally {
			setSaving(false)
		}
	}

	return (
		<Dialog open={open} onOpenChange={(o) => !o && onClose()}>
			<DialogContent>
				<DialogHeader>
					<DialogTitle>{title ? `${t`Edit tags`} — ${title}` : <Trans>Edit tags</Trans>}</DialogTitle>
				</DialogHeader>
				<div className="grid gap-3">
					<Label>
						<Trans>Tags</Trans>
					</Label>
					<div className="flex min-h-6 flex-wrap gap-1.5">
						{tags.length === 0 ? (
							<span className="text-sm text-muted-foreground">
								<Trans>No tags yet</Trans>
							</span>
						) : (
							tags.map((tag) => (
								<Badge key={tag} variant="secondary" className="gap-1 font-normal">
									{tag}
									<button
										type="button"
										onClick={() => setTags(tags.filter((x) => x !== tag))}
										className="text-muted-foreground transition-colors hover:text-foreground"
										aria-label={t`Remove tag`}
									>
										<XIcon className="size-3" />
									</button>
								</Badge>
							))
						)}
					</div>
					<div className="flex gap-2">
						<Input
							value={input}
							onChange={(e) => setInput(e.target.value)}
							onKeyDown={(e) => {
								if (e.key === "Enter") {
									e.preventDefault()
									addTag()
								}
							}}
							placeholder={t`Add a tag…`}
						/>
						<Button type="button" variant="outline" onClick={addTag}>
							<Trans>Add</Trans>
						</Button>
					</div>
				</div>
				<DialogFooter>
					<Button variant="outline" onClick={onClose}>
						<Trans>Cancel</Trans>
					</Button>
					<Button onClick={save} disabled={saving}>
						<Trans>Save</Trans>
					</Button>
				</DialogFooter>
			</DialogContent>
		</Dialog>
	)
}
