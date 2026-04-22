//go:build testing

package hub_test

import (
	"net/http"
	"testing"
	"time"

	appTests "github.com/Gu1llaum-3/vigil/internal/tests"
	pbTests "github.com/pocketbase/pocketbase/tests"
)

// createMonitorWithEvents is a helper that creates a monitor and inserts monitor_events
// with the given checked_at offsets (relative to now) and statuses.
func createMonitorWithEvents(t *testing.T, hub *appTests.TestHub, monitorType string, active bool, events []struct {
	offset time.Duration
	status int
}) string {
	t.Helper()
	monitor, err := appTests.CreateRecord(hub, "monitors", map[string]any{
		"name":   "test-monitor",
		"type":   monitorType,
		"active": active,
	})
	if err != nil {
		t.Fatalf("failed to create monitor: %v", err)
	}
	now := time.Now().UTC()
	for _, ev := range events {
		_, err := appTests.CreateRecord(hub, "monitor_events", map[string]any{
			"monitor":    monitor.Id,
			"status":     ev.status,
			"latency_ms": int64(42),
			"msg":        "",
			"checked_at": now.Add(ev.offset),
		})
		if err != nil {
			t.Fatalf("failed to create monitor_event: %v", err)
		}
	}
	return monitor.Id
}

// TestLoadMonitorMetrics_EventsInLast24h verifies that uptime_24h and uptime_30d are
// present in the API response when there are recent events (within the last 24h).
func TestLoadMonitorMetrics_EventsInLast24h(t *testing.T) {
	hub, err := appTests.NewTestHub(t.TempDir())
	if err != nil {
		t.Fatalf("failed to create test hub: %v", err)
	}
	defer hub.Cleanup()

	hub.StartHub()

	user, err := appTests.CreateUser(hub, "test@example.com", "password123")
	if err != nil {
		t.Fatalf("failed to create user: %v", err)
	}
	token, err := user.NewAuthToken()
	if err != nil {
		t.Fatalf("failed to create auth token: %v", err)
	}

	monitorID := createMonitorWithEvents(t, hub, "http", true, []struct {
		offset time.Duration
		status int
	}{
		{offset: -1 * time.Hour, status: 1},
	})

	scenario := appTests.ApiScenario{
		Name:   "GET /monitors/{id} - events in last 24h → uptime_24h and uptime_30d present",
		Method: http.MethodGet,
		URL:    "/api/app/monitors/" + monitorID,
		Headers: map[string]string{
			"Authorization": token,
		},
		ExpectedStatus:  http.StatusOK,
		ExpectedContent: []string{`"uptime_24h":`, `"uptime_30d":`, `"avg_latency_24h_ms":`},
		TestAppFactory: func(t testing.TB) *pbTests.TestApp {
			return hub.TestApp
		},
	}

	scenario.Test(t)
}

// TestLoadMonitorMetrics_NoEvents verifies that uptime_24h and uptime_30d are absent
// when no monitor_events exist for the monitor.
func TestLoadMonitorMetrics_NoEvents(t *testing.T) {
	hub, err := appTests.NewTestHub(t.TempDir())
	if err != nil {
		t.Fatalf("failed to create test hub: %v", err)
	}
	defer hub.Cleanup()

	hub.StartHub()

	user, err := appTests.CreateUser(hub, "test@example.com", "password123")
	if err != nil {
		t.Fatalf("failed to create user: %v", err)
	}
	token, err := user.NewAuthToken()
	if err != nil {
		t.Fatalf("failed to create auth token: %v", err)
	}

	monitor, err := appTests.CreateRecord(hub, "monitors", map[string]any{
		"name":   "test-monitor-no-events",
		"type":   "http",
		"active": false,
	})
	if err != nil {
		t.Fatalf("failed to create monitor: %v", err)
	}

	scenario := appTests.ApiScenario{
		Name:   "GET /monitors/{id} - no events → uptime_24h and uptime_30d absent",
		Method: http.MethodGet,
		URL:    "/api/app/monitors/" + monitor.Id,
		Headers: map[string]string{
			"Authorization": token,
		},
		ExpectedStatus:     http.StatusOK,
		NotExpectedContent: []string{`"uptime_24h":`, `"uptime_30d":`, `"avg_latency_24h_ms":`},
		TestAppFactory: func(t testing.TB) *pbTests.TestApp {
			return hub.TestApp
		},
	}

	scenario.Test(t)
}

// TestLoadMonitorMetrics_PushTypeNoLatency verifies that a push monitor with events
// exposes uptime_24h but not avg_latency_24h_ms.
func TestLoadMonitorMetrics_PushTypeNoLatency(t *testing.T) {
	hub, err := appTests.NewTestHub(t.TempDir())
	if err != nil {
		t.Fatalf("failed to create test hub: %v", err)
	}
	defer hub.Cleanup()

	hub.StartHub()

	user, err := appTests.CreateUser(hub, "test@example.com", "password123")
	if err != nil {
		t.Fatalf("failed to create user: %v", err)
	}
	token, err := user.NewAuthToken()
	if err != nil {
		t.Fatalf("failed to create auth token: %v", err)
	}

	monitorID := createMonitorWithEvents(t, hub, "push", true, []struct {
		offset time.Duration
		status int
	}{
		{offset: -30 * time.Minute, status: 1},
	})

	scenario := appTests.ApiScenario{
		Name:   "GET /monitors/{id} - push type → uptime_24h present, avg_latency_24h_ms absent",
		Method: http.MethodGet,
		URL:    "/api/app/monitors/" + monitorID,
		Headers: map[string]string{
			"Authorization": token,
		},
		ExpectedStatus:     http.StatusOK,
		ExpectedContent:    []string{`"uptime_24h":`},
		NotExpectedContent: []string{`"avg_latency_24h_ms":`},
		TestAppFactory: func(t testing.TB) *pbTests.TestApp {
			return hub.TestApp
		},
	}

	scenario.Test(t)
}

// TestLoadMonitorMetrics_EventsBeyond24hOnly verifies that when events exist only
// between 24h and 30d ago, uptime_30d is present but uptime_24h is absent.
func TestLoadMonitorMetrics_EventsBeyond24hOnly(t *testing.T) {
	hub, err := appTests.NewTestHub(t.TempDir())
	if err != nil {
		t.Fatalf("failed to create test hub: %v", err)
	}
	defer hub.Cleanup()

	hub.StartHub()

	user, err := appTests.CreateUser(hub, "test@example.com", "password123")
	if err != nil {
		t.Fatalf("failed to create user: %v", err)
	}
	token, err := user.NewAuthToken()
	if err != nil {
		t.Fatalf("failed to create auth token: %v", err)
	}

	// Event at 48h ago: inside 30d window but outside 24h window.
	// Monitor is inactive to prevent the scheduler from adding a recent check event.
	monitorID := createMonitorWithEvents(t, hub, "http", false, []struct {
		offset time.Duration
		status int
	}{
		{offset: -48 * time.Hour, status: 1},
	})

	scenario := appTests.ApiScenario{
		Name:   "GET /monitors/{id} - events only 24-30d ago → uptime_30d present, uptime_24h absent",
		Method: http.MethodGet,
		URL:    "/api/app/monitors/" + monitorID,
		Headers: map[string]string{
			"Authorization": token,
		},
		ExpectedStatus:     http.StatusOK,
		ExpectedContent:    []string{`"uptime_30d":`},
		NotExpectedContent: []string{`"uptime_24h":`},
		TestAppFactory: func(t testing.TB) *pbTests.TestApp {
			return hub.TestApp
		},
	}

	scenario.Test(t)
}
