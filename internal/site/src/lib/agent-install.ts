// Builds the agent install commands shown in the "Add agent" dialog.
// Kept free of imports so the logic stays pure and testable on its own.

const repoRawURL = "https://raw.githubusercontent.com/Gu1llaum-3/vigil"
const installScriptPath = "supplemental/scripts/install-agent.sh"

// A release hub reports the version stamped at build time (e.g. "0.2.16-beta", "1.0.0").
// Unstamped dev builds report "0.0.0-dev", which has no matching git tag.
const releaseVersionPattern = /^\d+\.\d+\.\d+(-[0-9A-Za-z.-]+)?$/

export function isReleaseVersion(version: string | undefined): version is string {
	return !!version && releaseVersionPattern.test(version) && !version.endsWith("-dev")
}

export interface InstallScriptCommandOptions {
	version: string | undefined
	publicKey: string
	token: string
	hubURL: string
	// Grant the agent Docker socket access (container inventory). Root-equivalent, so opt-in.
	docker?: boolean
}

// On a release hub, both the script and the agent are pinned to the hub's own version, so a
// new agent always matches the hub (the script's default, GitHub's latest stable release, can
// lag far behind during a beta). Dev builds fall back to the script on main and its default.
// curl -f makes a missing tag abort the chain instead of saving GitHub's 404 page as the script.
export function installScriptCommand({
	version,
	publicKey,
	token,
	hubURL,
	docker = false,
}: InstallScriptCommandOptions): string {
	const pinned = isReleaseVersion(version)
	const ref = pinned ? `v${version}` : "main"
	const versionArg = pinned ? ` -v v${version}` : ""
	const dockerArg = docker ? " --docker" : ""
	return `curl -fsSL ${repoRawURL}/${ref}/${installScriptPath} -o install-agent.sh && chmod +x install-agent.sh && ./install-agent.sh${versionArg}${dockerArg} -k "${publicKey}" -t "${token}" -url "${hubURL}"`
}
