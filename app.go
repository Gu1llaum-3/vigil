// Package app provides the application metadata for Vigil.
package app

// Version is the current application version. It is a var (not a const) so release
// builds can override it via -ldflags "-X github.com/Gu1llaum-3/vigil.Version=..."
// (see .goreleaser.yml). Keep the default a valid semver: the hub rejects agents
// whose reported version does not parse as semver.
var Version = "0.1.0"

const (
	// DisplayName is the user-facing product name shown in the UI.
	DisplayName = "Vigil"
	// AppName is the technical slug used for binaries, services, and data paths.
	AppName = "vigil"

	HubBinary      = AppName
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
