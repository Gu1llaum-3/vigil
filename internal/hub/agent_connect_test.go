//go:build testing

package hub

import (
	"context"
	"crypto/ed25519"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	appmeta "github.com/Gu1llaum-3/vigil"
	"github.com/Gu1llaum-3/vigil/agent"
	"github.com/Gu1llaum-3/vigil/internal/common"
	"github.com/blang/semver"
	"github.com/pocketbase/pocketbase/core"
	pbtests "github.com/pocketbase/pocketbase/tests"
	"github.com/pocketbase/pocketbase/tools/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/ssh"
)

// Helper function to create a test hub without import cycle
func createTestHub(t testing.TB) (*Hub, *pbtests.TestApp, error) {
	testDataDir := t.TempDir()
	testApp, err := pbtests.NewTestApp(testDataDir)
	if err != nil {
		return nil, nil, err
	}
	return NewHub(testApp), testApp, err
}

// cleanupTestHub tears down the test app.
func cleanupTestHub(_ *Hub, testApp *pbtests.TestApp) {
	if testApp != nil {
		testApp.Cleanup()
	}
}

// Helper function to create a test record
func createTestRecord(app core.App, collection string, data map[string]any) (*core.Record, error) {
	col, err := app.FindCachedCollectionByNameOrId(collection)
	if err != nil {
		return nil, err
	}
	record := core.NewRecord(col)
	for key, value := range data {
		record.Set(key, value)
	}

	return record, app.Save(record)
}

// Helper function to create a test user
func createTestUser(app core.App) (*core.Record, error) {
	userRecord, err := createTestRecord(app, "users", map[string]any{
		"email":    "test@test.com",
		"password": "testtesttest",
	})
	return userRecord, err
}

// TestValidateAgentHeaders tests the validateAgentHeaders function
func TestValidateAgentHeaders(t *testing.T) {
	hub, testApp, err := createTestHub(t)
	if err != nil {
		t.Fatal(err)
	}
	defer cleanupTestHub(hub, testApp)

	testCases := []struct {
		name          string
		headers       http.Header
		expectError   bool
		expectedToken string
		expectedAgent string
	}{
		{
			name: "valid headers",
			headers: http.Header{
				"X-Token": []string{"valid-token-123"},
				"X-App":   []string{"0.5.0"},
			},
			expectError:   false,
			expectedToken: "valid-token-123",
			expectedAgent: "0.5.0",
		},
		{
			name: "missing token",
			headers: http.Header{
				"X-App": []string{"0.5.0"},
			},
			expectError: true,
		},
		{
			name: "missing agent version",
			headers: http.Header{
				"X-Token": []string{"valid-token-123"},
			},
			expectError: true,
		},
		{
			name: "empty token",
			headers: http.Header{
				"X-Token": []string{""},
				"X-App":   []string{"0.5.0"},
			},
			expectError: true,
		},
		{
			name: "empty agent version",
			headers: http.Header{
				"X-Token": []string{"valid-token-123"},
				"X-App":   []string{""},
			},
			expectError: true,
		},
		{
			name: "token too long",
			headers: http.Header{
				"X-Token": []string{strings.Repeat("a", 65)},
				"X-App":   []string{"0.5.0"},
			},
			expectError: true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			acr := &agentConnectRequest{hub: hub}
			token, agentVersion, err := acr.validateAgentHeaders(tc.headers)

			if tc.expectError {
				assert.Error(t, err)
			} else {
				require.NoError(t, err)
				assert.Equal(t, tc.expectedToken, token)
				assert.Equal(t, tc.expectedAgent, agentVersion)
			}
		})
	}
}

// TestGetAgentsByToken tests the getAgentsByToken function
func TestGetAgentsByToken(t *testing.T) {
	hub, testApp, err := createTestHub(t)
	if err != nil {
		t.Fatal(err)
	}
	defer cleanupTestHub(hub, testApp)

	// Create test agent records
	agentRecord, err := createTestRecord(testApp, "agents", map[string]any{
		"name":        "test-agent",
		"token":       "test-token-123",
		"fingerprint": "test-fingerprint",
		"status":      "pending",
	})
	if err != nil {
		t.Fatal(err)
	}

	for i := range 3 {
		createTestRecord(testApp, "agents", map[string]any{
			"name":        fmt.Sprintf("test-agent-%d", i),
			"token":       "duplicate-token",
			"fingerprint": fmt.Sprintf("test-fingerprint-%d", i),
			"status":      "pending",
		})
	}

	testCases := []struct {
		name       string
		token      string
		expectedId string
		expectLen  int
	}{
		{
			name:       "valid token",
			token:      "test-token-123",
			expectLen:  1,
			expectedId: agentRecord.Id,
		},
		{
			name:      "invalid token",
			token:     "invalid-token",
			expectLen: 0,
		},
		{
			name:      "empty token",
			token:     "",
			expectLen: 0,
		},
		{
			name:      "duplicate token",
			token:     "duplicate-token",
			expectLen: 3,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			records := getAgentsByToken(tc.token, hub)

			require.Len(t, records, tc.expectLen)
			if tc.expectedId != "" {
				assert.Equal(t, tc.expectedId, records[0].Id)
			}
		})
	}
}

// TestUpdateAgent tests the UpdateAgent function
func TestUpdateAgent(t *testing.T) {
	hub, testApp, err := createTestHub(t)
	if err != nil {
		t.Fatal(err)
	}
	defer cleanupTestHub(hub, testApp)

	// Create test agent record
	agentRecord, err := createTestRecord(testApp, "agents", map[string]any{
		"name":        "test-agent",
		"token":       "test-token-123",
		"fingerprint": "",
		"status":      "pending",
	})
	if err != nil {
		t.Fatal(err)
	}

	testCases := []struct {
		name           string
		recordId       string
		newFingerprint string
		expectError    bool
	}{
		{
			name:           "successful agent update",
			recordId:       agentRecord.Id,
			newFingerprint: "new-test-fingerprint",
			expectError:    false,
		},
		{
			name:           "empty fingerprint",
			recordId:       agentRecord.Id,
			newFingerprint: "",
			expectError:    false,
		},
		{
			name:           "invalid record ID",
			recordId:       "invalid-id",
			newFingerprint: "fingerprint",
			expectError:    true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			err := hub.UpdateAgent(&AgentRecord{Id: tc.recordId, Token: "test-token-123"}, tc.newFingerprint, "connected", "1.0.0")

			if tc.expectError {
				assert.Error(t, err)
			} else {
				require.NoError(t, err)

				// Verify agent was updated
				updatedRecord, err := testApp.FindRecordById("agents", tc.recordId)
				require.NoError(t, err)
				assert.Equal(t, tc.newFingerprint, updatedRecord.GetString("fingerprint"))
				assert.Equal(t, "connected", updatedRecord.GetString("status"))
			}
		})
	}
}

func TestUpdateAgentDispatchesOnlineNotificationOnReconnect(t *testing.T) {
	hub, testApp, err := createTestHub(t)
	require.NoError(t, err)
	defer cleanupTestHub(hub, testApp)

	requests := make(chan struct{}, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests <- struct{}{}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	channelRecord, err := createTestRecord(testApp, "notification_channels", map[string]any{
		"name":    "agent-webhook",
		"kind":    "webhook",
		"enabled": true,
		"config":  map[string]any{"url": server.URL},
	})
	require.NoError(t, err)

	_, err = createTestRecord(testApp, "notification_rules", map[string]any{
		"name":             "agent-online",
		"enabled":          true,
		"events":           []string{"agent.online"},
		"channels":         []string{channelRecord.Id},
		"min_severity":     "info",
		"throttle_seconds": 0,
	})
	require.NoError(t, err)

	agentRecord, err := createTestRecord(testApp, "agents", map[string]any{
		"name":        "test-agent",
		"token":       "test-token-123",
		"fingerprint": "known-fingerprint",
		"status":      "offline",
	})
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go hub.notifier.Start(ctx)

	err = hub.UpdateAgent(&AgentRecord{Id: agentRecord.Id, Token: "test-token-123"}, "known-fingerprint", "connected", "1.0.0")
	require.NoError(t, err)

	select {
	case <-requests:
	case <-time.After(time.Second):
		t.Fatal("expected reconnect notification event")
	}
}

// TestEnrollmentTokenFlow tests the enrollment token authentication flow
func TestEnrollmentTokenFlow(t *testing.T) {
	_, testApp, err := createTestHub(t)
	if err != nil {
		t.Fatal(err)
	}
	defer cleanupTestHub(nil, testApp)

	// Create test user
	userRecord, err := createTestUser(testApp)
	if err != nil {
		t.Fatal(err)
	}

	// Set up enrollment token in the token map
	enrollmentToken := "enrollment-token-123"
	enrollmentTokenMap.GetMap().Set(enrollmentToken, userRecord.Id, time.Hour)

	testCases := []struct {
		name                 string
		token                string
		expectEnrollmentAuth bool
		expectError          bool
		description          string
	}{
		{
			name:                 "valid enrollment token",
			token:                enrollmentToken,
			expectEnrollmentAuth: true,
			expectError:          false,
			description:          "Should recognize valid enrollment token",
		},
		{
			name:                 "invalid enrollment token",
			token:                "invalid-enrollment-token",
			expectEnrollmentAuth: false,
			expectError:          true,
			description:          "Should reject invalid enrollment token",
		},
		{
			name:                 "empty token",
			token:                "",
			expectEnrollmentAuth: false,
			expectError:          true,
			description:          "Should reject empty token",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			acr := &agentConnectRequest{}

			acr.userId, acr.isEnrollmentToken = enrollmentTokenMap.GetMap().GetOk(tc.token)

			if tc.expectError {
				assert.False(t, acr.isEnrollmentToken)
				assert.Empty(t, acr.userId)
			} else {
				assert.Equal(t, tc.expectEnrollmentAuth, acr.isEnrollmentToken)
				if tc.expectEnrollmentAuth {
					assert.Equal(t, userRecord.Id, acr.userId)
				}
			}
		})
	}
}

func TestUpdateAgentInfoPersistsHostname(t *testing.T) {
	hub, testApp, err := createTestHub(t)
	require.NoError(t, err)
	defer cleanupTestHub(hub, testApp)

	agentRecord, err := createTestRecord(testApp, "agents", map[string]any{
		"token":       "test-token-hostname",
		"fingerprint": "test-fingerprint",
		"status":      "connected",
	})
	require.NoError(t, err)

	hub.updateAgentInfo(agentRecord.Id, common.AgentInfoResponse{
		Capabilities: map[string]any{"ws": true},
		Metadata: map[string]any{
			"hostname": "vector",
		},
	})

	updatedRecord, err := testApp.FindRecordById("agents", agentRecord.Id)
	require.NoError(t, err)
	assert.Equal(t, "vector", updatedRecord.GetString("name"))
	assert.JSONEq(t, `{"hostname":"vector"}`, string(updatedRecord.Get("metadata").(types.JSONRaw)))
}

func TestUpdateAgentInfoKeepsExistingNameWhenHostnameMissing(t *testing.T) {
	hub, testApp, err := createTestHub(t)
	require.NoError(t, err)
	defer cleanupTestHub(hub, testApp)

	agentRecord, err := createTestRecord(testApp, "agents", map[string]any{
		"name":        "existing-name",
		"token":       "test-token-no-hostname",
		"fingerprint": "test-fingerprint",
		"status":      "connected",
	})
	require.NoError(t, err)

	hub.updateAgentInfo(agentRecord.Id, common.AgentInfoResponse{
		Capabilities: map[string]any{"ws": true},
		Metadata:     map[string]any{},
	})

	updatedRecord, err := testApp.FindRecordById("agents", agentRecord.Id)
	require.NoError(t, err)
	assert.Equal(t, "existing-name", updatedRecord.GetString("name"))
}

func TestFindOrUpsertAgentAllowsEnrollmentTokenReuse(t *testing.T) {
	hub, testApp, err := createTestHub(t)
	require.NoError(t, err)
	defer cleanupTestHub(hub, testApp)

	userRecord, err := createTestUser(testApp)
	require.NoError(t, err)

	const sharedToken = "enrollment-token-shared"
	_, err = createTestRecord(testApp, "agents", map[string]any{
		"name":        "existing-agent",
		"token":       sharedToken,
		"fingerprint": "existing-fingerprint",
		"status":      "connected",
	})
	require.NoError(t, err)

	acr := &agentConnectRequest{
		hub:               hub,
		token:             sharedToken,
		isEnrollmentToken: true,
		userId:            userRecord.Id,
		agentSemVer:       semver.MustParse("1.0.0"),
	}

	newAgent, err := acr.findOrUpsertAgent(getAgentsByToken(sharedToken, hub), "new-fingerprint")
	require.NoError(t, err)
	assert.NotEmpty(t, newAgent.Id)
	assert.Equal(t, "new-fingerprint", newAgent.Fingerprint)

	agentRecords := getAgentsByToken(sharedToken, hub)
	require.Len(t, agentRecords, 2)

	fingerprints := []string{agentRecords[0].Fingerprint, agentRecords[1].Fingerprint}
	assert.Contains(t, fingerprints, "existing-fingerprint")
	assert.Contains(t, fingerprints, "new-fingerprint")
}

// TestAgentConnect tests the agentConnect function with various scenarios
func TestAgentConnect(t *testing.T) {
	hub, testApp, err := createTestHub(t)
	if err != nil {
		t.Fatal(err)
	}
	defer cleanupTestHub(hub, testApp)

	// Create test agent record
	testToken := "test-token-456"
	_, err = createTestRecord(testApp, "agents", map[string]any{
		"name":        "test-agent",
		"token":       testToken,
		"fingerprint": "",
		"status":      "pending",
	})
	if err != nil {
		t.Fatal(err)
	}

	testCases := []struct {
		name           string
		headers        map[string]string
		expectedStatus int
		description    string
		errorMessage   string
	}{
		{
			name: "missing token header",
			headers: map[string]string{
				"X-App": "0.5.0",
			},
			expectedStatus: http.StatusBadRequest,
			description:    "Should fail due to missing token",
			errorMessage:   "",
		},
		{
			name: "missing agent version header",
			headers: map[string]string{
				"X-Token": testToken,
			},
			expectedStatus: http.StatusBadRequest,
			description:    "Should fail due to missing agent version",
			errorMessage:   "",
		},
		{
			name: "invalid token",
			headers: map[string]string{
				"X-Token": "invalid-token",
				"X-App":   "0.5.0",
			},
			expectedStatus: http.StatusUnauthorized,
			description:    "Should fail due to invalid token",
			errorMessage:   "Invalid token",
		},
		{
			name: "invalid agent version",
			headers: map[string]string{
				"X-Token": testToken,
				"X-App":   "0.5.0.0.0",
			},
			expectedStatus: http.StatusUnauthorized,
			description:    "Should fail due to invalid agent version",
			errorMessage:   "Invalid agent version",
		},
		{
			name: "valid headers but websocket upgrade will fail in test",
			headers: map[string]string{
				"X-Token": testToken,
				"X-App":   "0.5.0",
			},
			expectedStatus: http.StatusInternalServerError,
			description:    "Should pass validation but fail at WebSocket upgrade due to test limitations",
			errorMessage:   "WebSocket upgrade failed",
		},
		{
			name:           "Token too long",
			headers:        map[string]string{"X-Token": strings.Repeat("a", 65), "X-App": "0.5.0"},
			expectedStatus: http.StatusBadRequest,
			description:    "Should reject token exceeding 64 characters",
			errorMessage:   "",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", "/api/app/agent-connect", nil)
			for key, value := range tc.headers {
				req.Header.Set(key, value)
			}

			recorder := httptest.NewRecorder()
			acr := &agentConnectRequest{
				hub: hub,
				req: req,
				res: recorder,
			}
			err = acr.agentConnect()

			assert.Equal(t, tc.expectedStatus, recorder.Code, tc.description)
			assert.Equal(t, tc.errorMessage, recorder.Body.String(), tc.description)
		})
	}
}

// TestSendResponseError tests the sendResponseError function
func TestSendResponseError(t *testing.T) {
	testCases := []struct {
		name           string
		statusCode     int
		message        string
		expectedStatus int
		expectedBody   string
	}{
		{
			name:           "unauthorized error",
			statusCode:     http.StatusUnauthorized,
			message:        "Invalid token",
			expectedStatus: http.StatusUnauthorized,
			expectedBody:   "Invalid token",
		},
		{
			name:           "bad request error",
			statusCode:     http.StatusBadRequest,
			message:        "Missing required header",
			expectedStatus: http.StatusBadRequest,
			expectedBody:   "Missing required header",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			acr := &agentConnectRequest{}
			acr.sendResponseError(recorder, tc.statusCode, tc.message)

			assert.Equal(t, tc.expectedStatus, recorder.Code)
			assert.Equal(t, tc.expectedBody, recorder.Body.String())
		})
	}
}

// TestHandleAgentConnect tests the HTTP handler
func TestHandleAgentConnect(t *testing.T) {
	hub, testApp, err := createTestHub(t)
	if err != nil {
		t.Fatal(err)
	}
	defer cleanupTestHub(hub, testApp)

	// Create test agent record
	testToken := "test-token-789"
	_, err = createTestRecord(testApp, "agents", map[string]any{
		"name":        "test-agent",
		"token":       testToken,
		"fingerprint": "",
		"status":      "pending",
	})
	if err != nil {
		t.Fatal(err)
	}

	testCases := []struct {
		name           string
		method         string
		headers        map[string]string
		expectedStatus int
		description    string
	}{
		{
			name:   "GET with invalid token",
			method: "GET",
			headers: map[string]string{
				"X-Token": "invalid",
				"X-App":   "0.5.0",
			},
			expectedStatus: http.StatusUnauthorized,
			description:    "Should reject invalid token",
		},
		{
			name:   "GET with valid token",
			method: "GET",
			headers: map[string]string{
				"X-Token": testToken,
				"X-App":   "0.5.0",
			},
			expectedStatus: http.StatusInternalServerError, // WebSocket upgrade fails in test
			description:    "Should pass validation but fail at WebSocket upgrade",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(tc.method, "/api/app/agent-connect", nil)
			for key, value := range tc.headers {
				req.Header.Set(key, value)
			}

			recorder := httptest.NewRecorder()
			acr := &agentConnectRequest{
				hub: hub,
				req: req,
				res: recorder,
			}
			err = acr.agentConnect()

			assert.Equal(t, tc.expectedStatus, recorder.Code, tc.description)
		})
	}
}

// TestAgentWebSocketIntegration tests WebSocket connection scenarios with an actual agent
func TestAgentWebSocketIntegration(t *testing.T) {
	// Create hub and test app
	hub, testApp, err := createTestHub(t)
	require.NoError(t, err)
	defer cleanupTestHub(hub, testApp)

	// Get the hub's SSH key
	hubSigner, err := hub.GetSSHKey("")
	require.NoError(t, err)
	goodPubKey := hubSigner.PublicKey()

	// Generate bad key pair (should be rejected)
	_, badPrivKey, err := ed25519.GenerateKey(nil)
	require.NoError(t, err)
	badPubKey, err := ssh.NewPublicKey(badPrivKey.Public().(ed25519.PublicKey))
	require.NoError(t, err)

	// Create HTTP server with the actual API route
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/app/agent-connect" {
			acr := &agentConnectRequest{
				hub: hub,
				req: r,
				res: w,
			}
			acr.agentConnect()
		} else {
			http.NotFound(w, r)
		}
	}))
	defer ts.Close()

	testCases := []struct {
		name              string
		agentToken        string        // Token agent will send
		dbToken           string        // Token in database (empty means no record created)
		agentFingerprint  string        // Fingerprint agent will send (empty means agent generates its own)
		dbFingerprint     string        // Fingerprint in database
		agentKey          ssh.PublicKey // Hub public key the agent will trust
		expectConnection  bool
		expectFingerprint string // "empty", "unchanged", or "updated"
		description       string
	}{
		{
			name:              "empty fingerprint - agent sets fingerprint on first connection",
			agentToken:        "test-token-1",
			dbToken:           "test-token-1",
			agentFingerprint:  "agent-fingerprint-1",
			dbFingerprint:     "",
			agentKey:          goodPubKey,
			expectConnection:  true,
			expectFingerprint: "updated",
			description:       "Agent should connect and set its fingerprint when DB fingerprint is empty",
		},
		{
			name:              "matching fingerprint should be accepted",
			agentToken:        "test-token-2",
			dbToken:           "test-token-2",
			agentFingerprint:  "matching-fingerprint-123",
			dbFingerprint:     "matching-fingerprint-123",
			agentKey:          goodPubKey,
			expectConnection:  true,
			expectFingerprint: "unchanged",
			description:       "Agent should connect when its fingerprint matches existing DB fingerprint",
		},
		{
			name:              "fingerprint mismatch should be rejected",
			agentToken:        "test-token-3",
			dbToken:           "test-token-3",
			agentFingerprint:  "different-fingerprint-456",
			dbFingerprint:     "original-fingerprint-123",
			agentKey:          goodPubKey,
			expectConnection:  false,
			expectFingerprint: "unchanged",
			description:       "Agent should be rejected when its fingerprint doesn't match existing DB fingerprint",
		},
		{
			name:              "invalid token should be rejected",
			agentToken:        "invalid-token-999",
			dbToken:           "test-token-4",
			agentFingerprint:  "matching-fingerprint-456",
			dbFingerprint:     "matching-fingerprint-456",
			agentKey:          goodPubKey,
			expectConnection:  false,
			expectFingerprint: "unchanged",
			description:       "Connection should fail when using invalid token",
		},
		{
			name:              "wrong hub key should be rejected",
			agentToken:        "test-token-5",
			dbToken:           "test-token-5",
			agentFingerprint:  "matching-fingerprint-789",
			dbFingerprint:     "matching-fingerprint-789",
			agentKey:          badPubKey,
			expectConnection:  false,
			expectFingerprint: "unchanged",
			description:       "Connection should fail when agent trusts a different hub key",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Create test agent record
			agentRecord, err := createTestRecord(testApp, "agents", map[string]any{
				"name":        fmt.Sprintf("test-agent-%s", tc.name),
				"token":       tc.dbToken,
				"fingerprint": tc.dbFingerprint,
				"status":      "pending",
			})
			require.NoError(t, err)

			// Create and configure agent
			agentDataDir := t.TempDir()

			// Set up agent fingerprint if specified
			err = os.WriteFile(filepath.Join(agentDataDir, "fingerprint"), []byte(tc.agentFingerprint), 0644)
			require.NoError(t, err)
			t.Logf("Pre-created fingerprint file for agent: %s", tc.agentFingerprint)

			testAgent, err := agent.NewAgent(agentDataDir)
			require.NoError(t, err)

			// Set up environment variables for the agent
			t.Setenv(appmeta.AgentEnvPrefix+"HUB_URL", ts.URL)
			t.Setenv(appmeta.AgentEnvPrefix+"TOKEN", tc.agentToken)

			// Start agent in background
			done := make(chan error, 1)
			go func() {
				done <- testAgent.Start([]ssh.PublicKey{tc.agentKey})
			}()

			// Wait for connection result
			maxWait := 2 * time.Second
			time.Sleep(40 * time.Millisecond)
			checkInterval := 20 * time.Millisecond
			timeout := time.After(maxWait)
			ticker := time.Tick(checkInterval)

			connectionManager := testAgent.GetConnectionManager()

			connectionResult := false
			for {
				select {
				case <-timeout:
					if tc.expectConnection {
						t.Fatalf("Expected connection to succeed but timed out - agent state: %d", connectionManager.State)
					} else {
						t.Logf("Connection properly rejected (timeout) - agent state: %d", connectionManager.State)
					}
					connectionResult = false
				case <-ticker:
					if connectionManager.State == agent.WebSocketConnected {
						if tc.expectConnection {
							t.Logf("WebSocket connection successful - agent state: %d", connectionManager.State)
							connectionResult = true
						} else {
							t.Errorf("Unexpected: Connection succeeded when it should have been rejected")
							return
						}
					}
				case err := <-done:
					if err != nil {
						if !tc.expectConnection {
							t.Logf("Agent connection properly rejected: %v", err)
							connectionResult = false
						} else {
							t.Fatalf("Agent failed to start: %v", err)
						}
					}
				}

				if connectionResult == tc.expectConnection || connectionResult {
					break
				}
			}

			time.Sleep(20 * time.Millisecond)

			// Verify agent state by re-reading the record
			updatedAgentRecord, err := testApp.FindRecordById("agents", agentRecord.Id)
			require.NoError(t, err)
			finalFingerprint := updatedAgentRecord.GetString("fingerprint")

			switch tc.expectFingerprint {
			case "empty":
				assert.Empty(t, finalFingerprint, "Fingerprint should be empty")
			case "unchanged":
				assert.Equal(t, tc.dbFingerprint, finalFingerprint, "Fingerprint should not change when connection is rejected")
			case "updated":
				if tc.dbFingerprint == "" {
					assert.NotEmpty(t, finalFingerprint, "Fingerprint should be updated after successful connection")
				} else {
					assert.NotEqual(t, tc.dbFingerprint, finalFingerprint, "Fingerprint should be updated after successful connection")
				}
			}

			t.Logf("%s - Fingerprint: %s", tc.description, finalFingerprint)
		})
	}
}

// TestGetRealIP tests the getRealIP function
func TestGetRealIP(t *testing.T) {
	testCases := []struct {
		name       string
		headers    map[string]string
		remoteAddr string
		expectedIP string
	}{
		{
			name:       "CF-Connecting-IP header",
			headers:    map[string]string{"CF-Connecting-IP": "192.168.1.1"},
			remoteAddr: "127.0.0.1:12345",
			expectedIP: "192.168.1.1",
		},
		{
			name:       "X-Forwarded-For header with single IP",
			headers:    map[string]string{"X-Forwarded-For": "192.168.1.2"},
			remoteAddr: "127.0.0.1:12345",
			expectedIP: "192.168.1.2",
		},
		{
			name:       "X-Forwarded-For header with multiple IPs",
			headers:    map[string]string{"X-Forwarded-For": "192.168.1.3, 10.0.0.1, 172.16.0.1"},
			remoteAddr: "127.0.0.1:12345",
			expectedIP: "192.168.1.3",
		},
		{
			name:       "X-Forwarded-For header with spaces",
			headers:    map[string]string{"X-Forwarded-For": "  192.168.1.4  "},
			remoteAddr: "127.0.0.1:12345",
			expectedIP: "192.168.1.4",
		},
		{
			name:       "No headers, fallback to RemoteAddr with port",
			headers:    map[string]string{},
			remoteAddr: "192.168.1.5:54321",
			expectedIP: "192.168.1.5",
		},
		{
			name:       "No headers, fallback to RemoteAddr without port",
			headers:    map[string]string{},
			remoteAddr: "192.168.1.6",
			expectedIP: "192.168.1.6",
		},
		{
			name:       "Both headers present, CF takes precedence",
			headers:    map[string]string{"CF-Connecting-IP": "192.168.1.1", "X-Forwarded-For": "192.168.1.2"},
			remoteAddr: "127.0.0.1:12345",
			expectedIP: "192.168.1.1",
		},
		{
			name:       "X-Forwarded-For present, takes precedence over RemoteAddr",
			headers:    map[string]string{"X-Forwarded-For": "192.168.1.2"},
			remoteAddr: "192.168.1.5:54321",
			expectedIP: "192.168.1.2",
		},
		{
			name:       "Empty X-Forwarded-For, fallback to RemoteAddr",
			headers:    map[string]string{"X-Forwarded-For": ""},
			remoteAddr: "192.168.1.7:12345",
			expectedIP: "192.168.1.7",
		},
		{
			name:       "Empty CF-Connecting-IP, fallback to X-Forwarded-For",
			headers:    map[string]string{"CF-Connecting-IP": "", "X-Forwarded-For": "192.168.1.8"},
			remoteAddr: "127.0.0.1:12345",
			expectedIP: "192.168.1.8",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", "/", nil)
			for key, value := range tc.headers {
				req.Header.Set(key, value)
			}
			req.RemoteAddr = tc.remoteAddr

			ip := getRealIP(req)
			assert.Equal(t, tc.expectedIP, ip)
		})
	}
}
