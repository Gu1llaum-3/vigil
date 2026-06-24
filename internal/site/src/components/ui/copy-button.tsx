import { useLingui } from "@lingui/react/macro"
import { CheckIcon, CopyIcon } from "lucide-react"
import { useEffect, useRef, useState } from "react"
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip"
import { cn, copyToClipboard } from "@/lib/utils"

/**
 * Small inline "copy to clipboard" affordance: a borderless icon button that swaps
 * the copy icon for a checkmark for 1.5s after a successful copy, with a tooltip.
 * Shared so the copy affordance (icon size, success feedback, a11y label) stays
 * consistent across the IP, image-ref, push-URL, … copy sites instead of drifting.
 */
export function CopyButton({
	value,
	label,
	className,
	iconClassName,
}: {
	value: string
	/** Tooltip + aria label shown in the idle state (defaults to "Click to copy"). */
	label?: string
	className?: string
	iconClassName?: string
}) {
	const { t } = useLingui()
	const resetTimeoutRef = useRef<number | null>(null)
	const [copied, setCopied] = useState(false)

	useEffect(() => {
		return () => {
			if (resetTimeoutRef.current !== null) {
				window.clearTimeout(resetTimeoutRef.current)
			}
		}
	}, [])

	async function handleCopy() {
		await copyToClipboard(value)
		setCopied(true)
		if (resetTimeoutRef.current !== null) {
			window.clearTimeout(resetTimeoutRef.current)
		}
		resetTimeoutRef.current = window.setTimeout(() => {
			setCopied(false)
			resetTimeoutRef.current = null
		}, 1500)
	}

	const tip = label ?? t`Click to copy`
	return (
		<Tooltip disableHoverableContent={true}>
			<TooltipTrigger asChild>
				<button
					type="button"
					onClick={handleCopy}
					aria-label={copied ? t`Copied to clipboard` : tip}
					className={cn(
						"shrink-0 text-muted-foreground/70 transition-colors hover:text-foreground",
						copied && "text-emerald-500 hover:text-emerald-500",
						className
					)}
				>
					{copied ? (
						<CheckIcon className={cn("size-3.5", iconClassName)} />
					) : (
						<CopyIcon className={cn("size-3.5", iconClassName)} />
					)}
				</button>
			</TooltipTrigger>
			<TooltipContent>
				<p>{copied ? t`Copied to clipboard` : tip}</p>
			</TooltipContent>
		</Tooltip>
	)
}
