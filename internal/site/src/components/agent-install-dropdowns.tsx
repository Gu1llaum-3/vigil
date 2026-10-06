import { memo } from "react"
import { installScriptCommand } from "@/lib/agent-install"
import { copyToClipboard, getHubURL } from "@/lib/utils"
import { DropdownMenuContent, DropdownMenuItem } from "./ui/dropdown-menu"

export function copyDockerCompose(publicKey: string, token: string) {
	copyToClipboard(`services:
  vigil-agent:
    image: 'gu1llaum3/vigil-agent'
    container_name: 'vigil-agent'
    restart: unless-stopped
    network_mode: host
    environment:
      KEY: '${publicKey}'
      TOKEN: '${token}'
      HUB_URL: '${getHubURL()}'
    volumes:
      - vigil_agent_data:/var/lib/vigil-agent

volumes:
  vigil_agent_data:`)
}

export function copyDockerRun(publicKey: string, token: string) {
	copyToClipboard(
		`docker run -d --name vigil-agent --network host --restart unless-stopped -v vigil_agent_data:/var/lib/vigil-agent -e KEY="${publicKey}" -e TOKEN="${token}" -e HUB_URL="${getHubURL()}" gu1llaum3/vigil-agent`
	)
}

export function copyInstallScriptCommand(publicKey: string, token: string) {
	copyToClipboard(installScriptCommand({ version: globalThis.APP?.HUB_VERSION, publicKey, token, hubURL: getHubURL() }))
}

export function copyBinaryEnvCommand(publicKey: string, token: string) {
	copyToClipboard(`export HUB_URL="${getHubURL()}"\nexport TOKEN="${token}"\nexport KEY="${publicKey}"\n./vigil-agent`)
}

export interface DropdownItem {
	text: string
	onClick?: () => void
	icons?: React.ComponentType<React.SVGProps<SVGSVGElement>>[]
}

export const InstallDropdown = memo(({ items }: { items: DropdownItem[] }) => {
	return (
		<DropdownMenuContent align="end">
			{items.map((item) => (
				<DropdownMenuItem key={item.text} onClick={item.onClick} className="cursor-pointer flex items-center gap-1.5">
					{item.text}
					{item.icons?.map((Icon, iconIndex) => (
						<Icon key={`${item.text}-${iconIndex}`} className="size-4" />
					))}
				</DropdownMenuItem>
			))}
		</DropdownMenuContent>
	)
})
