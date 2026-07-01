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

func TestSaveResultWritesPendingUnderThreshold(t *testing.T) {
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
	// Past the startup grace so threshold transitions apply immediately.
	ms.startedAt = time.Now().Add(-time.Hour)

	// Two failing checks under the threshold: events are pending, monitor stays up.
	ms.saveResult(monitor, monitorStatusDown, 0, "fail 1")
	ms.saveResult(monitor, monitorStatusDown, 0, "fail 2")
	require.Equal(t, monitorStatusUp, monitor.GetInt("status"), "monitor must stay up under threshold")

	// Third failing check hits the threshold: event is down, monitor flips down.
	ms.saveResult(monitor, monitorStatusDown, 0, "fail 3")
	require.Equal(t, monitorStatusDown, monitor.GetInt("status"))

	// Recovery: event is up.
	ms.saveResult(monitor, monitorStatusUp, 12, "ok")
	require.Equal(t, monitorStatusUp, monitor.GetInt("status"))

	countByStatus := func(status int) int {
		events, err := hub.FindRecordsByFilter("monitor_events",
			"monitor = {:id} && status = {:s}", "", 0, 0,
			map[string]any{"id": monitor.Id, "s": status})
		require.NoError(t, err)
		return len(events)
	}
	require.Equal(t, 2, countByStatus(monitorStatusPending), "two under-threshold failures are pending")
	require.Equal(t, 1, countByStatus(monitorStatusDown), "the threshold-crossing failure is down")
	require.Equal(t, 1, countByStatus(monitorStatusUp), "the recovery is up")
}

// TestSaveResultGraceWindowOutageStillRecordsDown locks the fix for the regression where an
// outage during the startup grace window was hidden as pending. The grace delays the
// monitor's own status flip, but a check that reached the failure threshold must still be
// recorded as down (0) so it counts toward uptime — not pending (2).
func TestSaveResultGraceWindowOutageStillRecordsDown(t *testing.T) {
	hub, testApp, err := createTestHub(t)
	require.NoError(t, err)
	defer cleanupTestHub(hub, testApp)

	monitor, err := createTestRecord(hub, "monitors", map[string]any{
		"name":              "API",
		"type":              "http",
		"status":            monitorStatusUnknown,
		"failure_count":     0,
		"failure_threshold": 2,
	})
	require.NoError(t, err)

	ms := newMonitorScheduler(hub)
	ms.startedAt = time.Now() // inside the startup grace window

	ms.saveResult(monitor, monitorStatusDown, 0, "fail 1") // count 1 < 2 → pending
	ms.saveResult(monitor, monitorStatusDown, 0, "fail 2") // count 2 ≥ 2 → down, but grace keeps status unknown

	require.Equal(t, monitorStatusUnknown, monitor.GetInt("status"), "grace must keep the monitor unknown")

	countByStatus := func(status int) int {
		events, err := hub.FindRecordsByFilter("monitor_events",
			"monitor = {:id} && status = {:s}", "", 0, 0,
			map[string]any{"id": monitor.Id, "s": status})
		require.NoError(t, err)
		return len(events)
	}
	require.Equal(t, 1, countByStatus(monitorStatusPending), "first sub-threshold failure is pending")
	require.Equal(t, 1, countByStatus(monitorStatusDown), "threshold-crossing failure is down even during grace")
}

func TestSaveResultFlagsMaintenance(t *testing.T) {
	hub, testApp, err := createTestHub(t)
	require.NoError(t, err)
	defer cleanupTestHub(hub, testApp)

	monitor, err := createTestRecord(hub, "monitors", map[string]any{
		"name":              "API",
		"type":              "http",
		"status":            monitorStatusUp,
		"failure_count":     0,
		"failure_threshold": 1,
	})
	require.NoError(t, err)

	now := time.Now().UTC()
	_, err = createTestRecord(hub, maintenanceCollection, map[string]any{
		"title":    "window",
		"enabled":  true,
		"strategy": "single",
		"start_at": now.Add(-time.Hour).Format(time.RFC3339),
		"end_at":   now.Add(time.Hour).Format(time.RFC3339),
		"scope":    map[string]any{}, // global → covers the monitor
	})
	require.NoError(t, err)
	require.NoError(t, hub.refreshMaintenanceCache())

	ms := newMonitorScheduler(hub)
	ms.startedAt = time.Now().Add(-time.Hour)
	require.True(t, hub.monitorUnderMaintenance(monitor.Id, now))

	ms.saveResult(monitor, monitorStatusUp, 20, "ok")

	events, err := hub.FindRecordsByFilter("monitor_events", "monitor = {:id}", "-checked_at", 1, 0,
		map[string]any{"id": monitor.Id})
	require.NoError(t, err)
	require.Len(t, events, 1)
	require.True(t, events[0].GetBool("maintenance"), "check inside an active window must be flagged maintenance")
}

func TestFamilyNetwork(t *testing.T) {
	cases := []struct {
		network, ipFamily, want string
	}{
		{"tcp", "", "tcp"},         // auto → unchanged (dual-stack Happy Eyeballs)
		{"tcp", "ipv4", "tcp4"},    // pin IPv4
		{"tcp", "ipv6", "tcp6"},    // pin IPv6
		{"tcp4", "ipv6", "tcp6"},   // explicit network still honors the pin
		{"tcp", "garbage", "tcp"},  // unknown value → unchanged
		{"udp", "ipv4", "udp"},     // non-tcp networks pass through
	}
	for _, tc := range cases {
		if got := familyNetwork(tc.network, tc.ipFamily); got != tc.want {
			t.Errorf("familyNetwork(%q, %q) = %q, want %q", tc.network, tc.ipFamily, got, tc.want)
		}
	}
}

// TestMonitorIPFamilyColumnPersists proves the 9_add_monitor_ip_family migration actually
// created the column in a fresh DB (the single-digit prefix must sort after 3_create_monitors)
// and that the value round-trips — an unknown/skipped field would silently read back "".
func TestMonitorIPFamilyColumnPersists(t *testing.T) {
	hub, testApp, err := createTestHub(t)
	require.NoError(t, err)
	defer cleanupTestHub(hub, testApp)

	mon, err := createTestRecord(hub, "monitors", map[string]any{
		"name":      "API",
		"type":      "http",
		"url":       "https://example.com",
		"ip_family": "ipv4",
	})
	require.NoError(t, err)

	reloaded, err := hub.FindRecordById("monitors", mon.Id)
	require.NoError(t, err)
	require.Equal(t, "ipv4", reloaded.GetString("ip_family"))
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

func TestInvertMonitorResult(t *testing.T) {
	// up becomes down (reachable target is the alert condition for inverted monitors);
	// the raw message is kept verbatim (no presentation baked into stored data)
	status, msg := invertMonitorResult(monitorStatusUp, "HTTP 200")
	require.Equal(t, monitorStatusDown, status)
	require.Equal(t, "HTTP 200", msg)

	// down becomes up (target is unreachable as expected)
	status, msg = invertMonitorResult(monitorStatusDown, "Connection failed")
	require.Equal(t, monitorStatusUp, status)
	require.Equal(t, "Connection failed", msg)

	// unknown is left untouched
	status, msg = invertMonitorResult(monitorStatusUnknown, "pending")
	require.Equal(t, monitorStatusUnknown, status)
	require.Equal(t, "pending", msg)
}
