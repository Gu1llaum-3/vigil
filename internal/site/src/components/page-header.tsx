import type * as React from "react"
import { cn } from "@/lib/utils"

export function PageHeader({
	icon: Icon,
	title,
	meta,
	description,
	actions,
	className,
}: {
	icon?: React.ComponentType<React.SVGProps<SVGSVGElement>>
	title: React.ReactNode
	meta?: React.ReactNode
	description?: React.ReactNode
	actions?: React.ReactNode
	className?: string
}) {
	return (
		<div className={cn("flex flex-col gap-3 sm:flex-row sm:items-start sm:justify-between", className)}>
			<div className="min-w-0">
				<h1 className="flex items-center gap-2 text-2xl font-semibold tracking-tight">
					{Icon && <Icon className="size-5 shrink-0 text-muted-foreground" />}
					<span className="truncate">{title}</span>
				</h1>
				{meta ? <div className="mt-1 text-sm text-muted-foreground">{meta}</div> : null}
				{description ? <p className="mt-1 max-w-3xl text-sm text-muted-foreground">{description}</p> : null}
			</div>
			{actions ? <div className="flex shrink-0 flex-wrap items-center gap-2 sm:justify-end">{actions}</div> : null}
		</div>
	)
}
