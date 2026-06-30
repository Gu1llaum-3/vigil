//go:build testing

package hub_test

import (
	"net/http"
	"testing"
	"time"

	appTests "github.com/Gu1llaum-3/vigil/internal/tests"
	pbTests "github.com/pocketbase/pocketbase/tests"
)

// TestGetFleetMetricsReturnsAllMetrics locks the bulk fleet-metrics contract: a single
// GET /api/app/fleet-metrics returns one series block per metric (cpu/memory/disk/load),
// each carrying the host's series — so the Metrics page renders every chart from one call.
func TestGetFleetMetricsReturnsAllMetrics(t *testing.T) {
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

	agent, err := appTests.CreateRecord(hub, "agents", map[string]any{"name": "web-1", "token": "tok-web-1"})
	if err != nil {
		t.Fatalf("failed to create agent: %v", err)
	}
	if _, err := appTests.CreateRecord(hub, "host_metric_samples", map[string]any{
		"agent":               agent.Id,
		"cpu_percent":         50.0,
		"memory_used_percent": 60.0,
		"disk_used_percent":   70.0,
		"load5":               1.2,
		"collected_at":        time.Now().UTC().Add(-5 * time.Minute),
	}); err != nil {
		t.Fatalf("failed to create host metric sample: %v", err)
	}

	scenario := appTests.ApiScenario{
		Name:           "GET /fleet-metrics returns all four metric blocks in one response",
		Method:         http.MethodGet,
		URL:            "/api/app/fleet-metrics?range=24h",
		Headers:        map[string]string{"Authorization": token},
		ExpectedStatus: http.StatusOK,
		ExpectedContent: []string{
			`"cpu":`, `"memory":`, `"disk":`, `"load":`,
			`"name":"web-1"`,
		},
		TestAppFactory: func(t testing.TB) *pbTests.TestApp { return hub.TestApp },
	}
	scenario.Test(t)
}
