package hub

import (
	"encoding/json"
	"net/http"

	"github.com/Gu1llaum-3/vigil/internal/common"
	"github.com/pocketbase/pocketbase/core"
)

// DashboardSummary holds fleet-wide KPI counters.
type DashboardSummary struct {
	TotalHosts               int                 `json:"total_hosts"`
	ConnectedHosts           int                 `json:"connected_hosts"`
	OfflineHosts             int                 `json:"offline_hosts"`
	TotalMonitors            int                 `json:"total_monitors"`
	UpMonitors               int                 `json:"up_monitors"`
	HostsNeedingUpdates      int                 `json:"hosts_needing_updates"`
	HostsNeedingReboot       int                 `json:"hosts_needing_reboot"`
	TotalOutdatedPackages    int                 `json:"total_outdated_packages"`
	TotalSecurityUpdates     int                 `json:"total_security_updates"`
	TotalContainers          int                 `json:"total_containers"`
	RunningContainers        int                 `json:"running_containers"`
	InsecureRepositories     int                 `json:"insecure_repositories"`
	OSDistribution           []DistributionEntry `json:"os_distribution"`
	UpdateStatusDistribution []DistributionEntry `json:"update_status_distribution"`
}

// DistributionEntry is a label/value pair for chart data.
type DistributionEntry struct {
	Label string `json:"label"`
	Value int    `json:"value"`
}

// DashboardHost merges agent identity/status with snapshot data.
type DashboardHost struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Status   string `json:"status"`
	LastSeen string `json:"last_seen"`
	common.HostSnapshotResponse
}

// PackageAggregate groups an outdated package across the fleet.
type PackageAggregate struct {
	Name          string `json:"name"`
	AffectedHosts int    `json:"affected_hosts"`
	SecurityHosts int    `json:"security_hosts"`
}

// RepositoryAggregate groups a repository across the fleet.
type RepositoryAggregate struct {
	Name         string `json:"name"`
	URL          string `json:"url"`
	Secure       bool   `json:"secure"`
	EnabledHosts int    `json:"enabled_hosts"`
}

// ContainerFleetEntry is a container with its host context.
type ContainerFleetEntry struct {
	HostID   string `json:"host_id"`
	HostName string `json:"host_name"`
	HostIP   string `json:"host_ip"`
	common.ContainerInfo
}

// getDashboard returns an aggregated view of all host snapshots.
func (h *Hub) getDashboard(e *core.RequestEvent) error {
	// Fetch all agents
	agentRecords, err := h.FindAllRecords("agents")
	if err != nil {
		return e.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
	}

	// Index agents by ID
	type agentMeta struct {
		name     string
		status   string
		lastSeen string
	}
	agentsMap := make(map[string]agentMeta, len(agentRecords))
	for _, a := range agentRecords {
		agentsMap[a.Id] = agentMeta{
			name:     a.GetString("name"),
			status:   a.GetString("status"),
			lastSeen: a.GetDateTime("last_seen").String(),
		}
	}

	// Fetch all snapshots
	snapshotRecords, _ := h.FindAllRecords("host_snapshots")
	monitorRecords, _ := h.FindAllRecords("monitors")

	// Build hosts list and aggregations
	var hosts []DashboardHost
	pkgMap := make(map[string]*PackageAggregate)
	repoMap := make(map[string]*RepositoryAggregate)
	var containers []ContainerFleetEntry
	summary := DashboardSummary{}
	osCount := make(map[string]int)
	updateStatusCount := make(map[string]int)

	// Track all known agents
	summary.TotalHosts = len(agentRecords)
	for _, a := range agentRecords {
		if a.GetString("status") == "connected" {
			summary.ConnectedHosts++
		} else {
			summary.OfflineHosts++
		}
	}

	summary.TotalMonitors = len(monitorRecords)
	for _, m := range monitorRecords {
		if m.GetInt("status") == 1 {
			summary.UpMonitors++
		}
	}

	for _, snap := range snapshotRecords {
		agentId := snap.GetString("agent")
		agent, ok := agentsMap[agentId]
		if !ok {
			continue
		}

		dataStr := snap.GetString("data")
		var snapshot common.HostSnapshotResponse
		if err := json.Unmarshal([]byte(dataStr), &snapshot); err != nil {
			continue
		}

		host := DashboardHost{
			ID:                   agentId,
			Name:                 agent.name,
			Status:               agent.status,
			LastSeen:             agent.lastSeen,
			HostSnapshotResponse: snapshot,
		}
		hosts = append(hosts, host)

		// KPI accumulators
		summary.TotalOutdatedPackages += snapshot.Packages.OutdatedCount
		summary.TotalSecurityUpdates += snapshot.Packages.SecurityCount
		if snapshot.Reboot.Required {
			summary.HostsNeedingReboot++
		}
		if snapshot.Packages.OutdatedCount > 0 && snapshot.Packages.LastUpgradeAgeDays > 30 {
			summary.HostsNeedingUpdates++
		}

		// Docker
		if snapshot.Docker.State == "available" {
			summary.TotalContainers += snapshot.Docker.ContainerCount
			summary.RunningContainers += snapshot.Docker.RunningCount
			for _, c := range snapshot.Docker.Containers {
				containers = append(containers, ContainerFleetEntry{
					HostID:        agentId,
					HostName:      agent.name,
					HostIP:        snapshot.PrimaryIP,
					ContainerInfo: c,
				})
			}
		}

		// OS distribution
		osLabel := snapshot.OS.Name
		if osLabel == "" {
			osLabel = snapshot.OS.Family
		}
		osCount[osLabel]++

		// Update status distribution
		switch {
		case snapshot.Packages.SecurityCount > 0:
			updateStatusCount["security"]++
		case snapshot.Packages.OutdatedCount > 0:
			updateStatusCount["needs_update"]++
		case !snapshot.Packages.LastUpgradeKnown:
			updateStatusCount["pending"]++
		default:
			updateStatusCount["up_to_date"]++
		}

		// Repositories
		for _, repo := range snapshot.Repositories {
			if !repo.Secure {
				summary.InsecureRepositories++
			}
			key := repo.Name + "|" + repo.URL
			if agg, exists := repoMap[key]; exists {
				if repo.Enabled {
					agg.EnabledHosts++
				}
			} else {
				enabled := 0
				if repo.Enabled {
					enabled = 1
				}
				repoMap[key] = &RepositoryAggregate{
					Name:         repo.Name,
					URL:          repo.URL,
					Secure:       repo.Secure,
					EnabledHosts: enabled,
				}
			}
		}

		// Packages
		for _, pkg := range snapshot.Packages.Outdated {
			if agg, exists := pkgMap[pkg.Name]; exists {
				agg.AffectedHosts++
				if pkg.IsSecurity {
					agg.SecurityHosts++
				}
			} else {
				security := 0
				if pkg.IsSecurity {
					security = 1
				}
				pkgMap[pkg.Name] = &PackageAggregate{
					Name:          pkg.Name,
					AffectedHosts: 1,
					SecurityHosts: security,
				}
			}
		}
	}

	// Build distribution slices
	for label, count := range osCount {
		summary.OSDistribution = append(summary.OSDistribution, DistributionEntry{Label: label, Value: count})
	}
	for label, count := range updateStatusCount {
		summary.UpdateStatusDistribution = append(summary.UpdateStatusDistribution, DistributionEntry{Label: label, Value: count})
	}

	// Flatten maps to slices
	var packages []PackageAggregate
	for _, v := range pkgMap {
		packages = append(packages, *v)
	}
	var repositories []RepositoryAggregate
	for _, v := range repoMap {
		repositories = append(repositories, *v)
	}

	return e.JSON(http.StatusOK, map[string]any{
		"summary":      summary,
		"hosts":        hosts,
		"packages":     packages,
		"repositories": repositories,
		"containers":   containers,
	})
}
