package common

import "github.com/fxamacker/cbor/v2"

type WebSocketAction = uint8

const (
	// GetAgentInfo requests generic agent identity and capability info.
	GetAgentInfo WebSocketAction = iota
	// CheckFingerprint verifies the agent identity via SSH signature.
	CheckFingerprint
	// Ping checks agent liveness.
	Ping
	// GetHostSnapshot requests a full system snapshot from the agent.
	GetHostSnapshot // 3
)

// HubRequest defines the structure for requests sent from hub to agent.
type HubRequest[T any] struct {
	Action WebSocketAction `cbor:"0,keyasint"`
	Data   T               `cbor:"1,keyasint,omitempty,omitzero"`
	Id     *uint32         `cbor:"2,keyasint,omitempty"`
}

// AgentResponse defines the structure for responses sent from agent to hub.
type AgentResponse struct {
	Id    *uint32 `cbor:"0,keyasint,omitempty"`
	Error string  `cbor:"3,keyasint,omitempty,omitzero"`
	// Data is the generic response payload.
	Data cbor.RawMessage `cbor:"7,keyasint,omitempty,omitzero"`
}

// FingerprintRequest is sent by the hub to request agent fingerprint verification.
type FingerprintRequest struct {
	Signature []byte `cbor:"0,keyasint"`
}

// FingerprintResponse is returned by the agent with its fingerprint.
type FingerprintResponse struct {
	Fingerprint string `cbor:"0,keyasint"`
}

// AgentInfoResponse is returned by the agent for GetAgentInfo requests.
type AgentInfoResponse struct {
	Version      string         `cbor:"version"`
	Capabilities map[string]any `cbor:"capabilities"`
	Metadata     map[string]any `cbor:"metadata"`
}

// HostSnapshotResponse is the top-level snapshot returned by the agent for GetHostSnapshot requests.
type HostSnapshotResponse struct {
	Hostname      string           `cbor:"hostname"        json:"hostname"`
	PrimaryIP     string           `cbor:"primary_ip"      json:"primary_ip"`
	OS            OSInfo           `cbor:"os"              json:"os"`
	Kernel        string           `cbor:"kernel"          json:"kernel"`
	Architecture  string           `cbor:"architecture"    json:"architecture"`
	UptimeSeconds uint64           `cbor:"uptime_seconds"  json:"uptime_seconds"`
	Resources     ResourceInfo     `cbor:"resources"       json:"resources"`
	Network       NetworkInfo      `cbor:"network"         json:"network"`
	Storage       []StorageMount   `cbor:"storage"         json:"storage"`
	Packages      PackageInfo      `cbor:"packages"        json:"packages"`
	Repositories  []RepositoryInfo `cbor:"repositories"    json:"repositories"`
	Reboot        RebootInfo       `cbor:"reboot"          json:"reboot"`
	Docker        DockerInfo       `cbor:"docker"          json:"docker"`
	CollectedAt   string           `cbor:"collected_at"    json:"collected_at"`
}

// OSInfo holds operating system identification fields.
type OSInfo struct {
	Family  string `cbor:"family"  json:"family"`
	Name    string `cbor:"name"    json:"name"`
	Version string `cbor:"version" json:"version"`
}

// ResourceInfo holds CPU and memory metrics.
type ResourceInfo struct {
	CPUModel string `cbor:"cpu_model" json:"cpu_model"`
	CPUCores int    `cbor:"cpu_cores" json:"cpu_cores"`
	RAMMB    uint64 `cbor:"ram_mb"    json:"ram_mb"`
	SwapMB   uint64 `cbor:"swap_mb"   json:"swap_mb"`
}

// NetworkInfo holds gateway and DNS configuration.
type NetworkInfo struct {
	Gateway    string   `cbor:"gateway"     json:"gateway"`
	DNSServers []string `cbor:"dns_servers" json:"dns_servers"`
}

// StorageMount represents a single mounted filesystem.
type StorageMount struct {
	Device         string  `cbor:"device"          json:"device"`
	Mountpoint     string  `cbor:"mountpoint"      json:"mountpoint"`
	FSType         string  `cbor:"fs_type"         json:"fs_type"`
	TotalBytes     uint64  `cbor:"total_bytes"     json:"total_bytes"`
	UsedBytes      uint64  `cbor:"used_bytes"      json:"used_bytes"`
	AvailableBytes uint64  `cbor:"available_bytes" json:"available_bytes"`
	UsedPercent    float64 `cbor:"used_percent"    json:"used_percent"`
}

// PackageInfo holds package state summary.
type PackageInfo struct {
	InstalledCount     int               `cbor:"installed_count"      json:"installed_count"`
	OutdatedCount      int               `cbor:"outdated_count"       json:"outdated_count"`
	SecurityCount      int               `cbor:"security_count"       json:"security_count"`
	LastUpgradeAt      string            `cbor:"last_upgrade_at"      json:"last_upgrade_at"`
	LastUpgradeAgeDays int               `cbor:"last_upgrade_age_days" json:"last_upgrade_age_days"`
	LastUpgradeKnown   bool              `cbor:"last_upgrade_known"   json:"last_upgrade_known"`
	Outdated           []OutdatedPackage `cbor:"outdated"             json:"outdated"`
}

// OutdatedPackage describes a single package that has a newer version available.
type OutdatedPackage struct {
	Name             string `cbor:"name"               json:"name"`
	InstalledVersion string `cbor:"installed_version"  json:"installed_version"`
	CandidateVersion string `cbor:"candidate_version"  json:"candidate_version"`
	IsSecurity       bool   `cbor:"is_security"        json:"is_security"`
}

// RepositoryInfo describes a single package repository.
type RepositoryInfo struct {
	Name         string `cbor:"name"         json:"name"`
	URL          string `cbor:"url"          json:"url"`
	Enabled      bool   `cbor:"enabled"      json:"enabled"`
	Secure       bool   `cbor:"secure"       json:"secure"`
	Distribution string `cbor:"distribution" json:"distribution"`
	Components   string `cbor:"components"   json:"components"`
}

// RebootInfo holds reboot requirement state.
type RebootInfo struct {
	Required bool   `cbor:"required" json:"required"`
	Reason   string `cbor:"reason"   json:"reason"`
}

// DockerInfo holds Docker daemon state and container inventory.
type DockerInfo struct {
	State          string          `cbor:"state"           json:"state"`
	ContainerCount int             `cbor:"container_count" json:"container_count"`
	RunningCount   int             `cbor:"running_count"   json:"running_count"`
	Containers     []ContainerInfo `cbor:"containers"      json:"containers"`
}

// ContainerInfo describes a single Docker container.
type ContainerInfo struct {
	ID                    string   `cbor:"id"                     json:"id"`
	Name                  string   `cbor:"name"                   json:"name"`
	Image                 string   `cbor:"image"                  json:"image"`
	ImageRef              string   `cbor:"image_ref"              json:"image_ref"`
	ImageID               string   `cbor:"image_id"               json:"image_id"`
	RepoDigests           []string `cbor:"repo_digests"           json:"repo_digests"`
	CurrentRefImageID     string   `cbor:"current_ref_image_id"   json:"current_ref_image_id"`
	CurrentRefRepoDigests []string `cbor:"current_ref_repo_digests" json:"current_ref_repo_digests"`
	Status                string   `cbor:"status"                 json:"status"`
	StatusText            string   `cbor:"status_text"            json:"status_text"`
	Ports                 string   `cbor:"ports"                  json:"ports"`
	// ExitCode is the container's last exit code, set only when the container
	// is in a terminal state (exited/dead). nil means "not applicable"
	// (e.g. running, restarting, paused, created) or "not reported by the agent".
	ExitCode *int `cbor:"exit_code,omitempty"           json:"exit_code,omitempty"`
}
