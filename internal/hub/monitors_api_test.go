//go:build testing

package hub_test

import (
	"net/http"
	"testing"

	appTests "github.com/Gu1llaum-3/vigil/internal/tests"
	pbTests "github.com/pocketbase/pocketbase/tests"
	"github.com/stretchr/testify/require"
)

func TestMoveMonitorUpdatesGroup(t *testing.T) {
	hub, err := appTests.NewTestHub(t.TempDir())
	require.NoError(t, err)
	defer hub.Cleanup()

	user, err := appTests.CreateUser(hub, "test@example.com", "password123")
	require.NoError(t, err)
	token, err := user.NewAuthToken()
	require.NoError(t, err)

	group, err := appTests.CreateRecord(hub, "monitor_groups", map[string]any{
		"name":   "Production",
		"weight": 10,
	})
	require.NoError(t, err)

	monitor, err := appTests.CreateRecord(hub, "monitors", map[string]any{
		"name":   "API",
		"type":   "http",
		"active": true,
	})
	require.NoError(t, err)

	hub.StartHub()

	scenario := appTests.ApiScenario{
		Name:   "POST /monitors/{id}/move",
		Method: http.MethodPost,
		URL:    "/api/app/monitors/" + monitor.Id + "/move",
		Headers: map[string]string{
			"Authorization": token,
		},
		Body:            jsonReader(map[string]any{"group": group.Id}),
		ExpectedStatus:   http.StatusOK,
		ExpectedContent:  []string{"\"group\":\"" + group.Id + "\""},
		TestAppFactory:   func(t testing.TB) *pbTests.TestApp { return hub.TestApp },
	}

	scenario.Test(t)

	updated, err := hub.FindRecordById("monitors", monitor.Id)
	require.NoError(t, err)
	require.Equal(t, group.Id, updated.GetString("group"))
}
