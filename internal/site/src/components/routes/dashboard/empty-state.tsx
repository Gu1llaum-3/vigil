import { getPagePath } from "@nanostores/router"
import { Trans } from "@lingui/react/macro"
import { ServerIcon } from "lucide-react"
import { memo } from "react"
import { $router, Link } from "@/components/router.tsx"
import { Button } from "@/components/ui/button"

export const EmptyState = memo(function EmptyState() {
	return (
		<div className="flex flex-1 flex-col items-center justify-center gap-6 py-20 text-center">
			<div className="flex size-16 items-center justify-center rounded-2xl border border-border/70 bg-muted/40">
				<ServerIcon className="size-8 text-muted-foreground" />
			</div>
			<div className="space-y-2">
				<h2 className="text-xl font-semibold">
					<Trans>No agents connected</Trans>
				</h2>
				<p className="max-w-sm text-sm text-muted-foreground leading-relaxed">
					<Trans>
						Connect your first agent to start collecting patch audit data. The dashboard will appear once snapshots are
						available.
					</Trans>
				</p>
			</div>
			<Button asChild>
				<Link href={getPagePath($router, "settings", { name: "agents" })}>
					<Trans>Set up agents</Trans>
				</Link>
			</Button>
		</div>
	)
})
