// Package app provides the application metadata for Vigil.
package app

const (
	// Version is the current version of the application.
	Version = "0.1.0"
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
