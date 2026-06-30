import { Trans, useLingui } from "@lingui/react/macro"
import { TagIcon } from "lucide-react"
import { useState } from "react"
import { TagsDialog } from "@/components/tags-dialog"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip"
import { isReadOnlyUser } from "@/lib/api"
import { cn } from "@/lib/utils"

// Shared host-tags rendering, used by the hosts table, host detail, and agents settings.
// `cloud` is the compact fixed-height form (first few tags + a focusable "+N" whose tooltip
// lists them all) used in dense table rows; `wrap` shows every tag wrapped. When `edit` is
// given (and the user is not read-only) it adds an edit affordance opening the shared
// TagsDialog: a hover pencil (`pencil`, expects an ancestor with the `group/row` class) or a
// labeled button (`button`). Tags are deduped here.
const CLOUD_VISIBLE = 2

export interface HostTagsEdit {
	agentId: string
	title?: string
	mode: "pencil" | "button"
}

export function HostTags({
	tags: rawTags,
	variant = "wrap",
	edit,
	emptyDash = false,
	className,
}: {
	tags?: string[]
	variant?: "cloud" | "wrap"
	edit?: HostTagsEdit
	emptyDash?: boolean
	className?: string
}) {
	const { t } = useLingui()
	const [open, setOpen] = useState(false)
	const tags = Array.from(new Set(rawTags ?? []))
	const canEdit = !!edit && !isReadOnlyUser()
	const editLabel = tags.length ? t`Edit tags` : t`Add tags`

	if (tags.length === 0 && !canEdit) {
		return emptyDash ? (
			<span className={cn("text-xs text-muted-foreground/40", className)}>—</span>
		) : null
	}

	const cloud = variant === "cloud"
	const visible = cloud ? tags.slice(0, CLOUD_VISIBLE) : tags
	const overflow = cloud ? tags.length - CLOUD_VISIBLE : 0
	const badgeClass = cloud
		? "max-w-[7rem] shrink-0 truncate px-1.5 py-0 text-[10px] font-normal"
		: "px-1.5 py-0 text-xs font-normal"

	return (
		<div className={cn("flex items-center gap-1", !cloud && "flex-wrap gap-1.5", className)}>
			<div className={cn("flex items-center gap-1", cloud ? "overflow-hidden" : "flex-wrap gap-1.5")}>
				{visible.map((tag) => (
					<Badge key={tag} variant="secondary" title={cloud ? tag : undefined} className={badgeClass}>
						{tag}
					</Badge>
				))}
				{overflow > 0 && (
					<Tooltip>
						<TooltipTrigger asChild>
							<button
								type="button"
								data-nolink
								onClick={(e) => e.stopPropagation()}
								aria-label={t`Show all ${tags.length} tags`}
								className="inline-flex shrink-0 items-center rounded-full border border-border/50 px-1.5 text-[10px] font-normal text-muted-foreground transition-colors hover:text-foreground focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-ring"
							>
								+{overflow}
							</button>
						</TooltipTrigger>
						<TooltipContent className="max-w-[18rem]">
							<div className="flex flex-wrap gap-1">
								{tags.map((tag) => (
									<Badge key={tag} variant="secondary" className="px-1.5 py-0 text-[10px] font-normal">
										{tag}
									</Badge>
								))}
							</div>
						</TooltipContent>
					</Tooltip>
				)}
			</div>
			{canEdit && edit.mode === "pencil" && (
				<Button
					variant="ghost"
					size="icon"
					data-nolink
					className="size-6 shrink-0 text-muted-foreground opacity-0 transition-opacity group-hover/row:opacity-100 focus-visible:opacity-100"
					onClick={(e) => {
						e.stopPropagation()
						setOpen(true)
					}}
					title={editLabel}
				>
					<TagIcon className="size-3.5" />
					<span className="sr-only">{editLabel}</span>
				</Button>
			)}
			{canEdit && edit.mode === "button" && (
				<Button
					variant="ghost"
					size="sm"
					data-nolink
					className="h-6 gap-1 px-2 text-xs text-muted-foreground"
					onClick={(e) => {
						e.stopPropagation()
						setOpen(true)
					}}
				>
					<TagIcon className="size-3.5" />
					{tags.length ? <Trans>Edit tags</Trans> : <Trans>Add tags</Trans>}
				</Button>
			)}
			{canEdit && (
				<TagsDialog
					agentId={edit.agentId}
					currentTags={tags}
					title={edit.title}
					open={open}
					onClose={() => setOpen(false)}
				/>
			)}
		</div>
	)
}
