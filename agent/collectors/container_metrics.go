//go:build linux

package collectors

import (
	"bufio"
	"context"
	"encoding/json"
	"math"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Gu1llaum-3/vigil/internal/common"
)

type containerMetricsState struct {
	mu         sync.Mutex
	prevNet    map[string]containerNetworkTotals
	prevNetAt  map[string]time.Time
}

type containerNetworkTotals struct {
	rx uint64
	tx uint64
}

var runningContainerMetricsState = containerMetricsState{
	prevNet:   map[string]containerNetworkTotals{},
	prevNetAt: map[string]time.Time{},
}

// CollectContainerMetrics gathers lightweight running-container metrics suitable for frequent polling.
func CollectContainerMetrics() common.ContainerMetricsSnapshotResponse {
	now := time.Now().UTC()
	result := common.ContainerMetricsSnapshotResponse{
		Containers:  []common.ContainerMetricsPoint{},
		CollectedAt: now.Format(time.RFC3339),
	}

	if !DockerAvailable() {
		return result
	}
	if _, err := exec.LookPath("docker"); err != nil {
		return result
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "docker", "stats", "--no-stream", "--format", "{{json .}}")
	out, err := cmd.Output()
	if err != nil {
		return result
	}

	points := make([]common.ContainerMetricsPoint, 0)
	seenIDs := make(map[string]struct{})
	scanner := bufio.NewScanner(strings.NewReader(string(out)))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var raw struct {
			ID      string `json:"ID"`
			Name    string `json:"Name"`
			CPUPerc string `json:"CPUPerc"`
			MemPerc string `json:"MemPerc"`
			MemUsage string `json:"MemUsage"`
			NetIO   string `json:"NetIO"`
		}
		if err := json.Unmarshal([]byte(line), &raw); err != nil {
			continue
		}
		if raw.ID == "" {
			continue
		}
		seenIDs[raw.ID] = struct{}{}
		memoryUsed, memoryLimit := parseDockerMemUsage(raw.MemUsage)
		rxBytes, txBytes := parseDockerNetIO(raw.NetIO)
		point := common.ContainerMetricsPoint{
			ID:               raw.ID,
			Name:             raw.Name,
			CPUPercent:       parseDockerPercent(raw.CPUPerc),
			MemoryUsedBytes:  memoryUsed,
			MemoryLimitBytes: memoryLimit,
		}
		applyContainerNetworkRates(now, raw.ID, rxBytes, txBytes, &point)
		points = append(points, point)
	}

	runningContainerMetricsState.mu.Lock()
	for id := range runningContainerMetricsState.prevNet {
		if _, ok := seenIDs[id]; !ok {
			delete(runningContainerMetricsState.prevNet, id)
			delete(runningContainerMetricsState.prevNetAt, id)
		}
	}
	runningContainerMetricsState.mu.Unlock()

	result.Containers = points
	return result
}

func applyContainerNetworkRates(now time.Time, id string, rxBytes, txBytes uint64, point *common.ContainerMetricsPoint) {
	runningContainerMetricsState.mu.Lock()
	defer runningContainerMetricsState.mu.Unlock()

	prev, ok := runningContainerMetricsState.prevNet[id]
	prevAt := runningContainerMetricsState.prevNetAt[id]
	runningContainerMetricsState.prevNet[id] = containerNetworkTotals{rx: rxBytes, tx: txBytes}
	runningContainerMetricsState.prevNetAt[id] = now
	if !ok || prevAt.IsZero() {
		return
	}
	elapsed := now.Sub(prevAt)
	if elapsed <= 0 {
		return
	}
	if rxBytes >= prev.rx {
		point.NetworkRxBps = uint64(float64(rxBytes-prev.rx) / elapsed.Seconds())
	}
	if txBytes >= prev.tx {
		point.NetworkTxBps = uint64(float64(txBytes-prev.tx) / elapsed.Seconds())
	}
}

func parseDockerPercent(raw string) float64 {
	clean := strings.TrimSpace(strings.TrimSuffix(raw, "%"))
	if clean == "" {
		return 0
	}
	value, err := strconv.ParseFloat(clean, 64)
	if err != nil {
		return 0
	}
	return math.Round(value*100) / 100
}

func parseDockerMemUsage(raw string) (used, limit uint64) {
	parts := strings.Split(raw, "/")
	if len(parts) != 2 {
		return 0, 0
	}
	return parseDockerSize(parts[0]), parseDockerSize(parts[1])
}

func parseDockerNetIO(raw string) (rx, tx uint64) {
	parts := strings.Split(raw, "/")
	if len(parts) != 2 {
		return 0, 0
	}
	return parseDockerSize(parts[0]), parseDockerSize(parts[1])
}

func parseDockerSize(raw string) uint64 {
	value := strings.TrimSpace(raw)
	if value == "" || value == "--" {
		return 0
	}
	fields := strings.Fields(value)
	if len(fields) == 0 {
		return 0
	}
	if len(fields) == 1 {
		fields = splitDockerSizeToken(fields[0])
	}
	if len(fields) != 2 {
		return 0
	}
	number, err := strconv.ParseFloat(fields[0], 64)
	if err != nil {
		return 0
	}
	switch strings.ToLower(strings.TrimSpace(fields[1])) {
	case "b":
		return uint64(number)
	case "kb", "kib":
		return uint64(number * 1024)
	case "mb", "mib":
		return uint64(number * 1024 * 1024)
	case "gb", "gib":
		return uint64(number * 1024 * 1024 * 1024)
	case "tb", "tib":
		return uint64(number * 1024 * 1024 * 1024 * 1024)
	default:
		return 0
	}
}

func splitDockerSizeToken(token string) []string {
	for i, r := range token {
		if (r < '0' || r > '9') && r != '.' {
			return []string{token[:i], token[i:]}
		}
	}
	return []string{token}
}
