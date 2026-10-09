// Package app provides the application metadata for Vigil.
package app

// Version is the current application version. It is a var (not a const) so release
// builds can override it via -ldflags "-X github.com/Gu1llaum-3/vigil.Version=..."
// (see .goreleaser.yml). Keep the default a valid semver: the hub rejects agents
// whose reported version does not parse as semver. The "-dev" pre-release marks an
// unstamped build: it matches no git tag and sorts below every real release (the web
// UI relies on it to avoid pinning install commands to a nonexistent tag).
var Version = "0.0.0-dev"

const (
	// DisplayName is the user-facing product name shown in the UI.
	DisplayName = "Vigil"
	// AppName is the technical slug used for binaries, services, and data paths.
	AppName = "vigil"

	HubBinary      = AppName
	// HubServiceName is the hub's systemd unit / rc service name (install-hub.sh).
	HubServiceName = AppName + "-hub"
	AgentBinary    = AppName + "-agent"
	HubEnvPrefix   = "VIGIL_HUB_"
	AgentEnvPrefix = "VIGIL_AGENT_"

	HubDataDirName     = AppName + "_data"
	AgentDataDirName   = AppName + "-agent"
	AgentConfigDirName = AppName
	HealthFileName     = AppName + "_health"
	UpdateTempDirName  = "." + AppName + "_update"

	ReleaseOwner      = "Gu1llaum-3"
	ReleaseRepo       = AppName
	ReleaseMirrorHost = ""
)
