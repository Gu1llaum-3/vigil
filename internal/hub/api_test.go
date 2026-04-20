package hub_test

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"testing"

	appTests "github.com/Gu1llaum-3/vigil/internal/tests"

	"github.com/pocketbase/pocketbase/core"
	pbTests "github.com/pocketbase/pocketbase/tests"
	"github.com/stretchr/testify/require"
)

// marshal to json and return an io.Reader (for use in ApiScenario.Body)
func jsonReader(v any) io.Reader {
	data, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return bytes.NewReader(data)
}

func TestApiRoutesAuthentication(t *testing.T) {
	hub, user := appTests.GetHubWithUser(t)
	defer hub.Cleanup()

	userToken, err := user.NewAuthToken()
	require.NoError(t, err, "Failed to create auth token")

	user2, err := appTests.CreateUser(hub, "testuser@example.com", "password123")
	require.NoError(t, err, "Failed to create test user")
	user2Token, err := user2.NewAuthToken()
	require.NoError(t, err, "Failed to create user2 auth token")
	_ = user2Token

	adminUser, err := appTests.CreateUserWithRole(hub, "admin@example.com", "password123", "admin")
	require.NoError(t, err, "Failed to create admin user")
	adminUserToken, err := adminUser.NewAuthToken()
	require.NoError(t, err)
	_ = adminUserToken

	readOnlyUser, err := appTests.CreateUserWithRole(hub, "readonly@example.com", "password123", "readonly")
	require.NoError(t, err, "Failed to create readonly user")
	readOnlyUserToken, err := readOnlyUser.NewAuthToken()
	require.NoError(t, err, "Failed to create readonly user auth token")

	superuser, err := appTests.CreateSuperuser(hub, "superuser@example.com", "password123")
	require.NoError(t, err, "Failed to create superuser")
	superuserToken, err := superuser.NewAuthToken()
	require.NoError(t, err, "Failed to create superuser auth token")

	testAppFactory := func(t testing.TB) *pbTests.TestApp {
		return hub.TestApp
	}

	scenarios := []appTests.ApiScenario{
		// Enrollment token
		{
			Name:            "GET /agent-enrollment-token - no auth should fail",
			Method:          http.MethodGet,
			URL:             "/api/app/agent-enrollment-token",
			ExpectedStatus:  401,
			ExpectedContent: []string{"requires valid"},
			TestAppFactory:  testAppFactory,
		},
		{
			Name:   "GET /agent-enrollment-token - with auth should succeed",
			Method: http.MethodGet,
			URL:    "/api/app/agent-enrollment-token",
			Headers: map[string]string{
				"Authorization": userToken,
			},
			ExpectedStatus:  200,
			ExpectedContent: []string{"active", "token", "permanent"},
			TestAppFactory:  testAppFactory,
		},
		{
			Name:   "GET /agent-enrollment-token - enable permanent should succeed",
			Method: http.MethodGet,
			URL:    "/api/app/agent-enrollment-token?enable=1&permanent=1&token=permanent-token-123",
			Headers: map[string]string{
				"Authorization": userToken,
			},
			ExpectedStatus:  200,
			ExpectedContent: []string{"\"permanent\":true", "permanent-token-123"},
			TestAppFactory:  testAppFactory,
		},
		{
			Name:   "GET /agent-enrollment-token - superuser should fail",
			Method: http.MethodGet,
			URL:    "/api/app/agent-enrollment-token",
			Headers: map[string]string{
				"Authorization": superuserToken,
			},
			ExpectedStatus:  403,
			ExpectedContent: []string{"Superusers cannot use enrollment tokens"},
			TestAppFactory:  testAppFactory,
		},
		{
			Name:   "GET /agent-enrollment-token - with readonly auth should fail",
			Method: http.MethodGet,
			URL:    "/api/app/agent-enrollment-token",
			Headers: map[string]string{
				"Authorization": readOnlyUserToken,
			},
			ExpectedStatus:  403,
			ExpectedContent: []string{"The authorized record is not allowed to perform this action."},
			TestAppFactory:  testAppFactory,
		},
		// Info (public key + version)
		{
			Name:            "GET /info - no auth should fail",
			Method:          http.MethodGet,
			URL:             "/api/app/info",
			ExpectedStatus:  401,
			ExpectedContent: []string{"requires valid"},
			TestAppFactory:  testAppFactory,
		},
		{
			Name:   "GET /info - with valid auth should succeed",
			Method: http.MethodGet,
			URL:    "/api/app/info",
			Headers: map[string]string{
				"Authorization": userToken,
			},
			ExpectedStatus:  200,
			ExpectedContent: []string{"\"key\":", "\"v\":"},
			TestAppFactory:  testAppFactory,
		},
		// First-run
		{
			Name:            "GET /first-run - no auth should succeed",
			Method:          http.MethodGet,
			URL:             "/api/app/first-run",
			ExpectedStatus:  200,
			ExpectedContent: []string{"\"firstRun\":false"},
			TestAppFactory:  testAppFactory,
		},
		{
			Name:   "GET /first-run - with auth should also succeed",
			Method: http.MethodGet,
			URL:    "/api/app/first-run",
			Headers: map[string]string{
				"Authorization": userToken,
			},
			ExpectedStatus:  200,
			ExpectedContent: []string{"\"firstRun\":false"},
			TestAppFactory:  testAppFactory,
		},
		// Agent connect
		{
			Name:            "GET /agent-connect - no auth (websocket upgrade fails but route is accessible)",
			Method:          http.MethodGet,
			URL:             "/api/app/agent-connect",
			ExpectedStatus:  400,
			ExpectedContent: []string{},
			TestAppFactory:  testAppFactory,
		},
	}

	for _, scenario := range scenarios {
		scenario.Test(t)
	}
}

func TestFirstUserCreation(t *testing.T) {
	t.Run("CreateUserEndpoint available when no users exist", func(t *testing.T) {
		cases := []struct {
			name     string
			seedUser bool
			scenario appTests.ApiScenario
		}{
			{
				name:     "POST /create-user - should be available when no users exist",
				seedUser: false,
				scenario: appTests.ApiScenario{
					Name:   "POST /create-user - should be available when no users exist",
					Method: http.MethodPost,
					URL:    "/api/app/create-user",
					Body: jsonReader(map[string]any{
						"email":    "firstuser@example.com",
						"password": "password123",
					}),
					ExpectedStatus:  200,
					ExpectedContent: []string{"User created"},
				},
			},
			{
				name:     "POST /create-user - should not be available when users exist",
				seedUser: true,
				scenario: appTests.ApiScenario{
					Name:   "POST /create-user - should not be available when users exist",
					Method: http.MethodPost,
					URL:    "/api/app/create-user",
					Body: jsonReader(map[string]any{
						"email":    "firstuser@example.com",
						"password": "password123",
					}),
					ExpectedStatus:  404,
					ExpectedContent: []string{"wasn't found"},
				},
			},
		}

		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				hub, _ := appTests.NewTestHub(t.TempDir())
				defer hub.Cleanup()

				if tc.seedUser {
					_, err := appTests.CreateUser(hub, "existing@example.com", "password")
					require.NoError(t, err)
				}

				hub.StartHub()

				tc.scenario.TestAppFactory = func(t testing.TB) *pbTests.TestApp {
					return hub.TestApp
				}

				tc.scenario.Test(t)
			})
		}
	})

	t.Run("CreateUserEndpoint not available when USER_EMAIL, USER_PASSWORD are set", func(t *testing.T) {
		t.Setenv("APP_HUB_USER_EMAIL", "me@example.com")
		t.Setenv("APP_HUB_USER_PASSWORD", "password123")

		hub, _ := appTests.NewTestHub(t.TempDir())
		defer hub.Cleanup()

		hub.StartHub()

		testAppFactory := func(t testing.TB) *pbTests.TestApp {
			return hub.TestApp
		}

		scenario := appTests.ApiScenario{
			Name:            "POST /create-user - should not be available when USER_EMAIL, USER_PASSWORD are set",
			Method:          http.MethodPost,
			URL:             "/api/app/create-user",
			ExpectedStatus:  404,
			ExpectedContent: []string{"wasn't found"},
			TestAppFactory:  testAppFactory,
			BeforeTestFunc: func(t testing.TB, app *pbTests.TestApp, e *core.ServeEvent) {
				users, err := hub.FindAllRecords("users")
				require.NoError(t, err)
				require.EqualValues(t, 1, len(users), "Should start with one user")
				require.EqualValues(t, "me@example.com", users[0].GetString("email"), "Should have created one user")
				superusers, err := hub.FindAllRecords(core.CollectionNameSuperusers)
				require.NoError(t, err)
				require.EqualValues(t, 1, len(superusers), "Should start with one superuser")
				require.EqualValues(t, "me@example.com", superusers[0].GetString("email"), "Should have created one superuser")
			},
			AfterTestFunc: func(t testing.TB, app *pbTests.TestApp, res *http.Response) {
				users, err := hub.FindAllRecords("users")
				require.NoError(t, err)
				require.EqualValues(t, 1, len(users), "Should still have one user")
				require.EqualValues(t, "me@example.com", users[0].GetString("email"), "Should have created one user")
				superusers, err := hub.FindAllRecords(core.CollectionNameSuperusers)
				require.NoError(t, err)
				require.EqualValues(t, 1, len(superusers), "Should still have one superuser")
				require.EqualValues(t, "me@example.com", superusers[0].GetString("email"), "Should have created one superuser")
			},
		}

		scenario.Test(t)
	})
}

func TestCreateUserEndpointAvailability(t *testing.T) {
	t.Run("CreateUserEndpoint available when no users exist", func(t *testing.T) {
		hub, _ := appTests.NewTestHub(t.TempDir())
		defer hub.Cleanup()

		userCount, err := hub.CountRecords("users")
		require.NoError(t, err)
		require.Zero(t, userCount, "Should start with no users")

		hub.StartHub()

		testAppFactory := func(t testing.TB) *pbTests.TestApp {
			return hub.TestApp
		}

		scenario := appTests.ApiScenario{
			Name:   "POST /create-user - should be available when no users exist",
			Method: http.MethodPost,
			URL:    "/api/app/create-user",
			Body: jsonReader(map[string]any{
				"email":    "firstuser@example.com",
				"password": "password123",
			}),
			ExpectedStatus:  200,
			ExpectedContent: []string{"User created"},
			TestAppFactory:  testAppFactory,
		}

		scenario.Test(t)

		userCount, err = hub.CountRecords("users")
		require.NoError(t, err)
		require.EqualValues(t, 1, userCount, "Should have created one user")
	})

	t.Run("CreateUserEndpoint not available when users exist", func(t *testing.T) {
		hub, _ := appTests.NewTestHub(t.TempDir())
		defer hub.Cleanup()

		_, err := appTests.CreateUser(hub, "existing@example.com", "password")
		require.NoError(t, err)

		hub.StartHub()

		testAppFactory := func(t testing.TB) *pbTests.TestApp {
			return hub.TestApp
		}

		scenario := appTests.ApiScenario{
			Name:   "POST /create-user - should not be available when users exist",
			Method: http.MethodPost,
			URL:    "/api/app/create-user",
			Body: jsonReader(map[string]any{
				"email":    "another@example.com",
				"password": "password123",
			}),
			ExpectedStatus:  404,
			ExpectedContent: []string{"wasn't found"},
			TestAppFactory:  testAppFactory,
		}

		scenario.Test(t)
	})
}

func TestAutoLoginMiddleware(t *testing.T) {
	var hubs []*appTests.TestHub

	defer func() {
		for _, hub := range hubs {
			hub.Cleanup()
		}
	}()

	t.Setenv("AUTO_LOGIN", "user@test.com")

	testAppFactory := func(t testing.TB) *pbTests.TestApp {
		hub, _ := appTests.NewTestHub(t.TempDir())
		hubs = append(hubs, hub)
		hub.StartHub()
		return hub.TestApp
	}

	scenarios := []appTests.ApiScenario{
		{
			Name:            "GET /info - without auto login should fail",
			Method:          http.MethodGet,
			URL:             "/api/app/info",
			ExpectedStatus:  401,
			ExpectedContent: []string{"requires valid"},
			TestAppFactory:  testAppFactory,
		},
		{
			Name:            "GET /info - with auto login should fail if no matching user",
			Method:          http.MethodGet,
			URL:             "/api/app/info",
			ExpectedStatus:  401,
			ExpectedContent: []string{"requires valid"},
			TestAppFactory:  testAppFactory,
		},
		{
			Name:            "GET /info - with auto login should succeed",
			Method:          http.MethodGet,
			URL:             "/api/app/info",
			ExpectedStatus:  200,
			ExpectedContent: []string{"\"key\":", "\"v\":"},
			TestAppFactory:  testAppFactory,
			BeforeTestFunc: func(t testing.TB, app *pbTests.TestApp, e *core.ServeEvent) {
				appTests.CreateUser(app, "user@test.com", "password123")
			},
		},
	}

	for _, scenario := range scenarios {
		scenario.Test(t)
	}
}

func TestTrustedHeaderMiddleware(t *testing.T) {
	var hubs []*appTests.TestHub

	defer func() {
		for _, hub := range hubs {
			hub.Cleanup()
		}
	}()

	t.Setenv("TRUSTED_AUTH_HEADER", "X-App-Trusted")

	testAppFactory := func(t testing.TB) *pbTests.TestApp {
		hub, _ := appTests.NewTestHub(t.TempDir())
		hubs = append(hubs, hub)
		hub.StartHub()
		return hub.TestApp
	}

	scenarios := []appTests.ApiScenario{
		{
			Name:            "GET /info - without trusted header should fail",
			Method:          http.MethodGet,
			URL:             "/api/app/info",
			ExpectedStatus:  401,
			ExpectedContent: []string{"requires valid"},
			TestAppFactory:  testAppFactory,
		},
		{
			Name:   "GET /info - with trusted header should fail if no matching user",
			Method: http.MethodGet,
			URL:    "/api/app/info",
			Headers: map[string]string{
				"X-App-Trusted": "user@test.com",
			},
			ExpectedStatus:  401,
			ExpectedContent: []string{"requires valid"},
			TestAppFactory:  testAppFactory,
		},
		{
			Name:   "GET /info - with trusted header should succeed",
			Method: http.MethodGet,
			URL:    "/api/app/info",
			Headers: map[string]string{
				"X-App-Trusted": "user@test.com",
			},
			ExpectedStatus:  200,
			ExpectedContent: []string{"\"key\":", "\"v\":"},
			TestAppFactory:  testAppFactory,
			BeforeTestFunc: func(t testing.TB, app *pbTests.TestApp, e *core.ServeEvent) {
				appTests.CreateUser(app, "user@test.com", "password123")
			},
		},
	}

	for _, scenario := range scenarios {
		scenario.Test(t)
	}
}

func TestUpdateEndpoint(t *testing.T) {
	t.Setenv("CHECK_UPDATES", "true")

	hub, _ := appTests.NewTestHub(t.TempDir())
	defer hub.Cleanup()
	hub.StartHub()

	testAppFactory := func(t testing.TB) *pbTests.TestApp {
		return hub.TestApp
	}

	scenarios := []appTests.ApiScenario{
		{
			Name:            "update endpoint shouldn't work without auth",
			Method:          http.MethodGet,
			URL:             "/api/app/update",
			ExpectedStatus:  401,
			ExpectedContent: []string{"requires valid"},
			TestAppFactory:  testAppFactory,
		},
	}

	for _, scenario := range scenarios {
		scenario.Test(t)
	}
}
