//go:build testing

package hub

import (
	"context"
	"errors"
	"os/exec"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestSaveResultStartupGraceOnlyAppliesToUnknownMonitors(t *testing.T) {
	hub, testApp, err := createTestHub(t)
	require.NoError(t, err)
	defer cleanupTestHub(hub, testApp)

	monitor, err := createTestRecord(hub, "monitors", map[string]any{
		"name":              "API",
		"type":              "http",
		"status":            monitorStatusUp,
		"failure_count":     0,
		"failure_threshold": 3,
	})
	require.NoError(t, err)

	ms := newMonitorScheduler(hub)
	ms.startedAt = time.Now()

	for range 3 {
		ms.saveResult(monitor, monitorStatusDown, 0, "connection failed")
	}

	updated, err := hub.FindRecordById("monitors", monitor.Id)
	require.NoError(t, err)
	require.Equal(t, monitorStatusDown, updated.GetInt("status"))
	require.Equal(t, 3, updated.GetInt("failure_count"))
}

func TestSaveResultKeepsUnknownMonitorsInStartupGrace(t *testing.T) {
	hub, testApp, err := createTestHub(t)
	require.NoError(t, err)
	defer cleanupTestHub(hub, testApp)

	monitor, err := createTestRecord(hub, "monitors", map[string]any{
		"name":              "API",
		"type":              "http",
		"status":            monitorStatusUnknown,
		"failure_count":     0,
		"failure_threshold": 3,
	})
	require.NoError(t, err)

	ms := newMonitorScheduler(hub)
	ms.startedAt = time.Now()

	for range 3 {
		ms.saveResult(monitor, monitorStatusDown, 0, "connection failed")
	}

	updated, err := hub.FindRecordById("monitors", monitor.Id)
	require.NoError(t, err)
	require.Equal(t, monitorStatusUnknown, updated.GetInt("status"))
	require.Equal(t, 3, updated.GetInt("failure_count"))
}

func TestCheckPingReturnsParsedLatencyOnSuccess(t *testing.T) {
	originalLookPath := pingLookPath
	originalCommandContext := pingCommandContext
	t.Cleanup(func() {
		pingLookPath = originalLookPath
		pingCommandContext = originalCommandContext
	})

	pingLookPath = func(file string) (string, error) {
		return "/usr/bin/ping", nil
	}
	pingCommandContext = func(ctx context.Context, name string, args ...string) *exec.Cmd {
		return exec.CommandContext(ctx, "sh", "-c", "printf '64 bytes from 1.1.1.1: icmp_seq=0 ttl=57 time=12.34 ms\n'")
	}

	hub, testApp, err := createTestHub(t)
	require.NoError(t, err)
	defer cleanupTestHub(hub, testApp)

	monitor, err := createTestRecord(hub, "monitors", map[string]any{
		"name":     "Ping",
		"type":     "ping",
		"hostname": "1.1.1.1",
	})
	require.NoError(t, err)

	status, latency, msg := checkPing(context.Background(), monitor)
	require.Equal(t, monitorStatusUp, status)
	require.Equal(t, int64(12), latency)
	require.Equal(t, "Ping successful", msg)
}

func TestCheckPingReportsMissingExecutable(t *testing.T) {
	originalLookPath := pingLookPath
	t.Cleanup(func() {
		pingLookPath = originalLookPath
	})

	pingLookPath = func(file string) (string, error) {
		return "", errors.New("missing")
	}

	hub, testApp, err := createTestHub(t)
	require.NoError(t, err)
	defer cleanupTestHub(hub, testApp)

	monitor, err := createTestRecord(hub, "monitors", map[string]any{
		"name":     "Ping",
		"type":     "ping",
		"hostname": "1.1.1.1",
	})
	require.NoError(t, err)

	status, latency, msg := checkPing(context.Background(), monitor)
	require.Equal(t, monitorStatusDown, status)
	require.Zero(t, latency)
	require.Equal(t, "Ping executable not available on hub", msg)
}

func TestCheckPingRequiresHostname(t *testing.T) {
	hub, testApp, err := createTestHub(t)
	require.NoError(t, err)
	defer cleanupTestHub(hub, testApp)

	monitor, err := createTestRecord(hub, "monitors", map[string]any{
		"name": "Ping",
		"type": "ping",
	})
	require.NoError(t, err)

	status, latency, msg := checkPing(context.Background(), monitor)
	require.Equal(t, monitorStatusDown, status)
	require.Zero(t, latency)
	require.Equal(t, "Missing hostname", msg)
}
