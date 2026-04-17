//go:build testing

package hub

import (
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
