//go:build linux

package collectors

import (
	"bufio"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"sort"
	"strings"

	"github.com/Gu1llaum-3/vigil/internal/common"
)

// DockerAvailable returns true if the Docker socket exists and is accessible.
func DockerAvailable() bool {
	_, err := os.Stat("/var/run/docker.sock")
	return err == nil
}

// CollectDocker collects Docker container inventory.
func CollectDocker(ctx context.Context) (common.DockerInfo, error) {
	if !DockerAvailable() {
		return common.DockerInfo{State: "not_configured"}, nil
	}

	if _, err := exec.LookPath("docker"); err != nil {
		return common.DockerInfo{State: "cli_missing"}, err
	}

	cmd := exec.CommandContext(ctx, "docker", "ps", "-a", "--no-trunc", "--format", "{{json .}}")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return common.DockerInfo{State: classifyDockerError(out)}, err
	}

	containers := make([]common.ContainerInfo, 0)
	running := 0

	scanner := bufio.NewScanner(strings.NewReader(string(out)))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}

		var raw struct {
			ID     string `json:"ID"`
			Names  string `json:"Names"`
			Image  string `json:"Image"`
			State  string `json:"State"`
			Status string `json:"Status"`
			Ports  string `json:"Ports"`
		}
		if err := json.Unmarshal([]byte(line), &raw); err != nil {
			continue
		}

		name := strings.TrimPrefix(raw.Names, "/")
		status := strings.ToLower(raw.State)
		if status == "running" {
			running++
		}

		containers = append(containers, common.ContainerInfo{
			ID:         raw.ID,
			Name:       name,
			Image:      raw.Image,
			ImageRef:   raw.Image,
			Status:     status,
			StatusText: raw.Status,
			Ports:      raw.Ports,
		})
	}
	if err := scanner.Err(); err != nil {
		return common.DockerInfo{State: "error"}, err
	}

	if len(containers) > 0 {
		if err := enrichContainers(ctx, containers); err != nil {
			return common.DockerInfo{State: "error"}, err
		}
	}

	return common.DockerInfo{
		State:          "available",
		ContainerCount: len(containers),
		RunningCount:   running,
		Containers:     containers,
	}, nil
}

func classifyDockerError(out []byte) string {
	msg := strings.ToLower(string(out))
	switch {
	case strings.Contains(msg, "permission denied"):
		return "permission_denied"
	case strings.Contains(msg, "cannot connect to the docker daemon"):
		return "daemon_unreachable"
	default:
		return "error"
	}
}

func enrichContainers(ctx context.Context, containers []common.ContainerInfo) error {
	containerIDs := make([]string, 0, len(containers))
	containerByID := make(map[string]*common.ContainerInfo, len(containers))
	for i := range containers {
		containerIDs = append(containerIDs, containers[i].ID)
		containerByID[containers[i].ID] = &containers[i]
	}

	inspectCmd := exec.CommandContext(ctx, "docker", append([]string{"inspect"}, containerIDs...)...)
	inspectOut, err := inspectCmd.CombinedOutput()
	if err != nil {
		return err
	}

	var inspected []struct {
		ID     string `json:"Id"`
		Name   string `json:"Name"`
		Image  string `json:"Image"`
		State  struct {
			Status   string `json:"Status"`
			ExitCode int    `json:"ExitCode"`
		} `json:"State"`
		Config struct {
			Image string `json:"Image"`
		} `json:"Config"`
	}
	if err := json.Unmarshal(inspectOut, &inspected); err != nil {
		return err
	}

	imageIDs := make([]string, 0, len(inspected))
	imageIDSet := make(map[string]struct{}, len(inspected))
	currentRefs := make([]string, 0, len(inspected))
	currentRefSet := make(map[string]struct{}, len(inspected))
	for _, inspectedContainer := range inspected {
		container := containerByID[inspectedContainer.ID]
		if container == nil {
			continue
		}
		container.ImageID = inspectedContainer.Image
		if inspectedContainer.Config.Image != "" {
			container.ImageRef = inspectedContainer.Config.Image
		}
		// Expose ExitCode only for terminal states so "ExitCode: 0" on a
		// running container isn't misread as a successful exit downstream.
		switch strings.ToLower(inspectedContainer.State.Status) {
		case "exited", "dead":
			code := inspectedContainer.State.ExitCode
			container.ExitCode = &code
		}
		if _, exists := imageIDSet[inspectedContainer.Image]; inspectedContainer.Image != "" && !exists {
			imageIDSet[inspectedContainer.Image] = struct{}{}
			imageIDs = append(imageIDs, inspectedContainer.Image)
		}
		if _, exists := currentRefSet[container.ImageRef]; container.ImageRef != "" && !exists {
			currentRefSet[container.ImageRef] = struct{}{}
			currentRefs = append(currentRefs, container.ImageRef)
		}
	}

	if len(imageIDs) == 0 {
		return nil
	}

	imageInspectCmd := exec.CommandContext(ctx, "docker", append([]string{"image", "inspect"}, imageIDs...)...)
	imageInspectOut, err := imageInspectCmd.CombinedOutput()
	if err != nil {
		return err
	}

	var inspectedImages []struct {
		ID          string   `json:"Id"`
		RepoDigests []string `json:"RepoDigests"`
	}
	if err := json.Unmarshal(imageInspectOut, &inspectedImages); err != nil {
		return err
	}

	imageDigests := make(map[string][]string, len(inspectedImages))
	for _, image := range inspectedImages {
		digests := append([]string(nil), image.RepoDigests...)
		sort.Strings(digests)
		imageDigests[image.ID] = digests
	}

	for i := range containers {
		containers[i].RepoDigests = append([]string(nil), imageDigests[containers[i].ImageID]...)
	}

	if len(currentRefs) == 0 {
		return nil
	}

	currentRefInspectCmd := exec.CommandContext(ctx, "docker", append([]string{"image", "inspect"}, currentRefs...)...)
	currentRefInspectOut, err := currentRefInspectCmd.CombinedOutput()
	if err != nil {
		return nil
	}

	var inspectedCurrentRefs []struct {
		ID          string   `json:"Id"`
		RepoTags    []string `json:"RepoTags"`
		RepoDigests []string `json:"RepoDigests"`
	}
	if err := json.Unmarshal(currentRefInspectOut, &inspectedCurrentRefs); err != nil {
		return nil
	}

	currentRefMetadata := make(map[string]struct {
		ID      string
		Digests []string
	}, len(inspectedCurrentRefs))
	if len(inspectedCurrentRefs) == len(currentRefs) {
		for i, currentRef := range currentRefs {
			digests := append([]string(nil), inspectedCurrentRefs[i].RepoDigests...)
			sort.Strings(digests)
			currentRefMetadata[currentRef] = struct {
				ID      string
				Digests []string
			}{ID: inspectedCurrentRefs[i].ID, Digests: digests}
		}
	}
	for _, image := range inspectedCurrentRefs {
		digests := append([]string(nil), image.RepoDigests...)
		sort.Strings(digests)
		for _, repoTag := range image.RepoTags {
			currentRefMetadata[repoTag] = struct {
				ID      string
				Digests []string
			}{ID: image.ID, Digests: digests}
		}
	}

	for i := range containers {
		if metadata, ok := currentRefMetadata[containers[i].ImageRef]; ok {
			containers[i].CurrentRefImageID = metadata.ID
			containers[i].CurrentRefRepoDigests = append([]string(nil), metadata.Digests...)
		}
	}

	return nil
}
