import { Trans, useLingui } from "@lingui/react/macro"
import { BellIcon, BellOffIcon } from "lucide-react"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import {
	DropdownMenu,
	DropdownMenuContent,
	DropdownMenuItem,
	DropdownMenuSub,
	DropdownMenuSubContent,
	DropdownMenuSubTrigger,
	DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu"
import { toast } from "@/components/ui/use-toast"
import {
	type ActiveMute,
	type MuteResourceType,
	MUTE_PRESETS,
	mutedUntilFromHours,
	muteResource,
	unmuteResource,
} from "@/lib/mutes"
import { cn } from "@/lib/utils"

/**
 * formatRemaining returns a coarse "time left" string for a timed mute (e.g. "45m",
 * "7h", "2d"), or "" for an indefinite/expired mute. The useMutes 60s re-eval keeps
 * this roughly current without a dedicated ticker.
 */
function formatRemaining(mutedUntil: string): string {
	const ms = new Date(mutedUntil).getTime() - Date.now()
	if (!(ms > 0)) return ""
	const min = Math.round(ms / 60000)
	if (min < 60) return `${min}m`
	const hr = Math.round(min / 60)
	if (hr < 48) return `${hr}h`
	return `${Math.round(hr / 24)}d`
}

function PresetLabel({ hours }: { hours: number | null }) {
	switch (hours) {
		case 1:
			return <Trans>For 1 hour</Trans>
		case 8:
			return <Trans>For 8 hours</Trans>
		case 24:
			return <Trans>For 24 hours</Trans>
		default:
			return <Trans>Until I resume</Trans>
	}
}

/**
 * MuteMenuItems renders the mute/resume entries for a resource. Drop it inside an existing
 * DropdownMenuContent. When the resource is muted it shows a single "Resume" item; otherwise
 * a submenu of duration presets. Suppresses both the in-app bell and external channels.
 */
export function MuteMenuItems({
	type,
	id,
	activeMute,
	onChanged,
}: {
	type: MuteResourceType
	id: string
	activeMute?: ActiveMute
	onChanged?: () => void
}) {
	const { t } = useLingui()

	async function applyMute(hours: number | null) {
		try {
			await muteResource(type, id, mutedUntilFromHours(hours))
			toast({ title: t`Notifications muted` })
			onChanged?.()
		} catch {
			toast({ title: t`Failed to mute notifications`, variant: "destructive" })
		}
	}

	async function resume() {
		try {
			await unmuteResource(type, id)
			toast({ title: t`Notifications resumed` })
			onChanged?.()
		} catch {
			toast({ title: t`Failed to resume notifications`, variant: "destructive" })
		}
	}

	if (activeMute) {
		return (
			<DropdownMenuItem onSelect={resume}>
				<BellIcon className="me-2.5 size-4" />
				<Trans>Resume notifications</Trans>
			</DropdownMenuItem>
		)
	}

	return (
		<DropdownMenuSub>
			<DropdownMenuSubTrigger>
				<BellOffIcon className="me-2.5 size-4" />
				<Trans>Mute notifications</Trans>
			</DropdownMenuSubTrigger>
			<DropdownMenuSubContent>
				{MUTE_PRESETS.map(({ hours }) => (
					<DropdownMenuItem key={hours ?? "indefinite"} onSelect={() => applyMute(hours)}>
						<PresetLabel hours={hours} />
					</DropdownMenuItem>
				))}
			</DropdownMenuSubContent>
		</DropdownMenuSub>
	)
}

/**
 * MuteBellButton is a self-contained bell control: a ghost icon button whose dropdown
 * holds the same mute/resume options as MuteMenuItems. When the resource is muted the
 * bell turns amber and shows the remaining time (for a timed mute); otherwise it stays
 * faint. Use it as a standalone row affordance (e.g. far-right of a table row).
 */
export function MuteBellButton({
	type,
	id,
	activeMute,
	onChanged,
}: {
	type: MuteResourceType
	id: string
	activeMute?: ActiveMute
	onChanged?: () => void
}) {
	const { t } = useLingui()
	const muted = !!activeMute
	const remaining = activeMute?.mutedUntil ? formatRemaining(activeMute.mutedUntil) : ""
	const title = !muted
		? t`Mute notifications`
		: activeMute?.mutedUntil
			? t`Muted until ${new Date(activeMute.mutedUntil).toLocaleString()}`
			: t`Muted indefinitely`

	return (
		<DropdownMenu>
			<DropdownMenuTrigger asChild>
				<Button
					variant="ghost"
					size="sm"
					data-nolink
					onClick={(e) => e.stopPropagation()}
					title={title}
					className={cn(
						"h-6 gap-1 px-1.5 tabular-nums",
						muted ? "text-amber-500 hover:text-amber-500" : "text-muted-foreground/40 hover:text-foreground"
					)}
				>
					{muted ? <BellOffIcon className="size-4" /> : <BellIcon className="size-4" />}
					{muted && remaining && <span className="text-[10px]">{remaining}</span>}
					<span className="sr-only">{title}</span>
				</Button>
			</DropdownMenuTrigger>
			<DropdownMenuContent align="end">
				<MuteMenuItems type={type} id={id} activeMute={activeMute} onChanged={onChanged} />
			</DropdownMenuContent>
		</DropdownMenu>
	)
}

/** MuteBadge shows a small "Muted" pill — the remaining time when timed, the expiry in its tooltip. */
export function MuteBadge({ activeMute, className }: { activeMute?: ActiveMute; className?: string }) {
	const { t } = useLingui()
	if (!activeMute) return null
	const remaining = activeMute.mutedUntil ? formatRemaining(activeMute.mutedUntil) : ""
	const title = activeMute.mutedUntil ? t`Muted until ${new Date(activeMute.mutedUntil).toLocaleString()}` : t`Muted`
	return (
		<Badge
			variant="outline"
			className={cn("gap-1 border-border/50 px-1.5 py-0 text-[10px] font-normal text-muted-foreground", className)}
			title={title}
		>
			<BellOffIcon className="size-3" />
			{remaining ? remaining : <Trans>Muted</Trans>}
		</Badge>
	)
}
