//go:build linux

package collectors

import (
	"bufio"
	"encoding/json"
	"os"
	"os/exec"
	"strings"

	"github.com/Gu1llaum-3/vigil/internal/common"
)

// DockerAvailable returns true if the Docker socket exists and is accessible.
func DockerAvailable() bool {
	_, err := os.Stat("/var/run/docker.sock")
	return err == nil
}

// CollectDocker collects Docker container inventory.
func CollectDocker() (common.DockerInfo, error) {
	if !DockerAvailable() {
		return common.DockerInfo{State: "unavailable"}, nil
	}

	cmd := exec.Command("docker", "ps", "-a", "--format", "{{json .}}")
	out, err := cmd.Output()
	if err != nil {
		return common.DockerInfo{State: "error"}, err
	}

	var containers []common.ContainerInfo
	running := 0

	scanner := bufio.NewScanner(strings.NewReader(string(out)))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}

		var raw struct {
			ID      string `json:"ID"`
			Names   string `json:"Names"`
			Image   string `json:"Image"`
			State   string `json:"State"`
			Status  string `json:"Status"`
			Ports   string `json:"Ports"`
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
			Status:     status,
			StatusText: raw.Status,
			Ports:      raw.Ports,
		})
	}

	return common.DockerInfo{
		State:          "available",
		ContainerCount: len(containers),
		RunningCount:   running,
		Containers:     containers,
	}, nil
}
