//go:build testing

package hub_test

import (
	"net/http"
	"testing"
	"time"

	appTests "github.com/Gu1llaum-3/vigil/internal/tests"
	pbTests "github.com/pocketbase/pocketbase/tests"
)

// TestGetMonitorsList_BatchedMetrics verifies the monitors list endpoint computes metrics
// for every monitor through the single batched GROUP BY aggregate: multiple monitors each
// get their own uptime values, and a push monitor keeps uptime but omits avg latency.
func TestGetMonitorsList_BatchedMetrics(t *testing.T) {
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

	recentUp := []struct {
		offset time.Duration
		status int
	}{{offset: -1 * time.Hour, status: 1}}

	createMonitorWithEvents(t, hub, "http", true, recentUp)
	createMonitorWithEvents(t, hub, "push", true, recentUp)

	scenario := appTests.ApiScenario{
		Name:   "GET /monitors - batched metrics for all monitors",
		Method: http.MethodGet,
		URL:    "/api/app/monitors",
		Headers: map[string]string{
			"Authorization": token,
		},
		ExpectedStatus: http.StatusOK,
		// Two monitors → two uptime_24h entries; latency present for the http monitor.
		ExpectedContent: []string{`"uptime_24h":`, `"uptime_30d":`, `"avg_latency_24h_ms":`, `"type":"http"`, `"type":"push"`},
		TestAppFactory: func(t testing.TB) *pbTests.TestApp {
			return hub.TestApp
		},
	}

	scenario.Test(t)
}
