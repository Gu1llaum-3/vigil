//go:build testing

package hub

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	appmeta "github.com/Gu1llaum-3/vigil"
	"github.com/Gu1llaum-3/vigil/agent"
	"github.com/Gu1llaum-3/vigil/internal/hub/ws"
	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/ssh"
)

// The helpers below are also called from Eventually/Never conditions, which run in their
// own goroutine (and may still run after the test returned): no require in them.

func agentRecordsByFingerprint(hub *Hub, fingerprint string) []*core.Record {
	recs, _ := hub.FindRecordsByFilter("agents", "fingerprint = {:fp}", "", 0, 0, dbx.Params{"fp": fingerprint})
	return recs
}

func agentRecordByID(hub *Hub, id string) *core.Record {
	rec, _ := hub.FindRecordById("agents", id)
	return rec
}

func pendingRecordFor(hub *Hub, existingID string) *core.Record {
	rec, _ := hub.FindFirstRecordByFilter("agents", "duplicate_of = {:id}", dbx.Params{"id": existingID})
	return rec
}

func storedTokenIn(dataDir string) string {
	data, _ := os.ReadFile(filepath.Join(dataDir, "agent-token"))
	return string(data)
}

func connectedWithToken(hub *Hub, id, token string) bool {
	rec := agentRecordByID(hub, id)
	_, live := hub.agentConns.Load(id)
	return rec != nil && live && rec.GetString("status") == "connected" && rec.GetString("token") == token
}

type identityTestEnv struct {
	hub        *Hub
	key        ssh.PublicKey
	enrollment string
	adminAuth  string
}

func newIdentityTestEnv(t *testing.T) identityTestEnv {
	t.Helper()
	hub, testApp, err := createTestHub(t)
	require.NoError(t, err)
	t.Cleanup(func() { cleanupTestHub(hub, testApp) })
	signer, err := hub.GetSSHKey("")
	require.NoError(t, err)
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		acr := &agentConnectRequest{hub: hub, req: r, res: w}
		_ = acr.agentConnect()
	}))
	t.Cleanup(ts.Close)

	admin, err := createTestRecord(hub, "users", map[string]any{"email": "admin@test.com", "password": "testtesttest", "role": "admin"})
	require.NoError(t, err)
	auth, err := admin.NewAuthToken()
	require.NoError(t, err)
	const enrollment = "SharedEnrollmentTokenForTheIdentityTest"
	enrollmentTokenMap.GetMap().Set(enrollment, admin.Id, time.Hour)
	t.Cleanup(func() { enrollmentTokenMap.GetMap().Remove(enrollment) })
	t.Setenv(appmeta.AgentEnvPrefix+"HUB_URL", ts.URL)
	t.Setenv(appmeta.AgentEnvPrefix+"TOKEN", enrollment)
	return identityTestEnv{hub: hub, key: signer.PublicKey(), enrollment: enrollment, adminAuth: auth}
}

// startHost runs a real agent with its own data dir and the given fingerprint.
func (env identityTestEnv) startHost(t *testing.T, fingerprint string) string {
	t.Helper()
	dir := t.TempDir()
	require.NoError(t, agent.SaveFingerprint(dir, fingerprint))
	a, err := agent.NewAgent(dir)
	require.NoError(t, err)
	go func() { _ = a.Start([]ssh.PublicKey{env.key}) }()
	return dir
}

// enrolled waits for the single host with this fingerprint to be issued its own token.
func (env identityTestEnv) enrolled(t *testing.T, fingerprint string) *core.Record {
	t.Helper()
	var rec *core.Record
	require.Eventually(t, func() bool {
		recs := agentRecordsByFingerprint(env.hub, fingerprint)
		if len(recs) != 1 || !recs[0].GetBool("token_issued") {
			return false
		}
		rec = recs[0]
		return connectedWithToken(env.hub, rec.Id, rec.GetString("token"))
	}, 15*time.Second, 50*time.Millisecond, "the host never got its own token")
	return rec
}

func (env identityTestEnv) post(t *testing.T, path string) *httptest.ResponseRecorder {
	t.Helper()
	return performNotificationRequest(t, env.hub, env.hub, http.MethodPost, path, env.adminAuth)
}

// Enrolling with the shared enrollment token gives the host a token of its own, which it
// reconnects with; rotating it pushes the new value to the connected host.
func TestEnrolledAgentGetsItsOwnToken(t *testing.T) {
	env := newIdentityTestEnv(t)
	dir := env.startHost(t, "victim-fp")
	victim := env.enrolled(t, "victim-fp")
	issued := victim.GetString("token")
	assert.Len(t, issued, 40)
	assert.NotEqual(t, env.enrollment, issued)
	assert.Contains(t, storedTokenIn(dir), issued)
	// The hub proved its identity by signing the agent's nonce: the agent recorded its key,
	// and will refuse a static (replayable) signature from it from now on.
	challenge, err := os.ReadFile(filepath.Join(dir, "hub-challenge"))
	require.NoError(t, err)
	assert.Contains(t, string(challenge), ssh.FingerprintSHA256(env.key))

	// A real reconnect: the hub drops the connection, the agent comes back with its token.
	env.hub.dropAgentConn(victim.Id)
	require.Eventually(t, func() bool { return connectedWithToken(env.hub, victim.Id, issued) }, 30*time.Second, 50*time.Millisecond)
	assert.Len(t, agentRecordsByFingerprint(env.hub, "victim-fp"), 1)

	res := env.post(t, "/api/app/agents/"+victim.Id+"/rotate-token")
	require.Equal(t, http.StatusOK, res.Code, res.Body.String())
	assert.Contains(t, res.Body.String(), `"pushed":true`)
	rotated := agentRecordByID(env.hub, victim.Id).GetString("token")
	assert.NotEqual(t, issued, rotated)
	assert.Contains(t, storedTokenIn(dir), rotated)
}

// Someone holding the enrollment token presents the fingerprint of a host that has its own
// token: it waits for approval — nothing collected, not in the fleet, the real host untouched.
// Rejecting deletes it.
func TestImpersonationAttemptAwaitsApproval(t *testing.T) {
	env := newIdentityTestEnv(t)
	env.startHost(t, "victim-fp")
	victim := env.enrolled(t, "victim-fp")
	issued := victim.GetString("token")

	env.startHost(t, "victim-fp")
	var pending *core.Record
	require.Eventually(t, func() bool {
		pending = pendingRecordFor(env.hub, victim.Id)
		_, live := env.hub.agentConns.Load(pendingID(pending))
		return pending != nil && live
	}, 15*time.Second, 50*time.Millisecond, "no pending record for the impersonation attempt")
	assert.Equal(t, agentStatusAwaitingApproval, pending.GetString("status"))
	assert.False(t, pending.GetBool("token_issued"), "no token issued before a decision")
	_, err := env.hub.FindFirstRecordByFilter("host_snapshots", "agent = {:id}", dbx.Params{"id": pending.Id})
	assert.Error(t, err, "nothing is collected from a pending host")
	assert.Equal(t, issued, agentRecordByID(env.hub, victim.Id).GetString("token"), "the real host keeps its token")
	hosts, err := env.hub.loadHostsOverview()
	require.NoError(t, err)
	for _, h := range hosts {
		assert.NotEqual(t, pending.Id, h.ID, "a pending host is not part of the fleet")
	}
	notes, err := env.hub.FindRecordsByFilter(systemNotificationsCollection, "event_kind = 'agent.duplicate_fingerprint'", "", 0, 0)
	require.NoError(t, err)
	assert.Len(t, notes, 1)

	// It reconnects: still one pending record, still pending, no second notification.
	env.hub.dropAgentConn(pending.Id)
	require.Eventually(t, func() bool { _, live := env.hub.agentConns.Load(pending.Id); return live }, 30*time.Second, 50*time.Millisecond)
	assert.Equal(t, agentStatusAwaitingApproval, agentRecordByID(env.hub, pending.Id).GetString("status"))
	notes, _ = env.hub.FindRecordsByFilter(systemNotificationsCollection, "event_kind = 'agent.duplicate_fingerprint'", "", 0, 0)
	assert.Len(t, notes, 1)

	res := env.post(t, "/api/app/agents/"+pending.Id+"/reject")
	require.Equal(t, http.StatusOK, res.Code, res.Body.String())
	assert.Nil(t, agentRecordByID(env.hub, pending.Id))
}

func pendingID(rec *core.Record) string {
	if rec == nil {
		return ""
	}
	return rec.Id
}

// A reinstalled host (data dir lost) comes back pending; merging hands it the original
// record (history kept) with a token pushed to it.
func TestMergingAReinstalledHostKeepsItsRecord(t *testing.T) {
	env := newIdentityTestEnv(t)
	env.startHost(t, "web1-fp")
	original := env.enrolled(t, "web1-fp")

	reinstalled := env.startHost(t, "web1-fp")
	var pending *core.Record
	require.Eventually(t, func() bool {
		pending = pendingRecordFor(env.hub, original.Id)
		_, live := env.hub.agentConns.Load(pendingID(pending))
		return pending != nil && live && agentRecordHasCapability(pending, "agent_token")
	}, 15*time.Second, 50*time.Millisecond)

	// The original agent of this test still runs (a real reinstall would have replaced it),
	// so the merge has to be forced — the guard itself is covered by TestApprovalGuards.
	res := env.post(t, "/api/app/agents/"+pending.Id+"/merge?force=1")
	require.Equal(t, http.StatusOK, res.Code, res.Body.String())
	merged := agentRecordByID(env.hub, original.Id).GetString("token")
	assert.NotEqual(t, original.GetString("token"), merged)
	assert.Contains(t, storedTokenIn(reinstalled), merged, "the reinstalled host got the original record's token")
	assert.Nil(t, agentRecordByID(env.hub, pending.Id))
	require.Eventually(t, func() bool { return connectedWithToken(env.hub, original.Id, merged) }, 30*time.Second, 50*time.Millisecond)
}

// A different machine that happens to share the fingerprint (same hostname) is approved as
// a host of its own.
func TestApprovingAPendingHostMakesItAHost(t *testing.T) {
	env := newIdentityTestEnv(t)
	env.startHost(t, "raspberrypi-fp")
	first := env.enrolled(t, "raspberrypi-fp")

	env.startHost(t, "raspberrypi-fp")
	var pending *core.Record
	require.Eventually(t, func() bool {
		pending = pendingRecordFor(env.hub, first.Id)
		_, live := env.hub.agentConns.Load(pendingID(pending))
		return pending != nil && live
	}, 15*time.Second, 50*time.Millisecond)

	res := env.post(t, "/api/app/agents/"+pending.Id+"/approve")
	require.Equal(t, http.StatusOK, res.Code, res.Body.String())
	require.Eventually(t, func() bool {
		rec := agentRecordByID(env.hub, pending.Id)
		return rec != nil && rec.GetBool("token_issued") && connectedWithToken(env.hub, rec.Id, rec.GetString("token"))
	}, 30*time.Second, 50*time.Millisecond, "the approved host never became a host with its own token")
	assert.Empty(t, agentRecordByID(env.hub, pending.Id).GetString("duplicate_of"))
	assert.NotEqual(t, first.GetString("token"), agentRecordByID(env.hub, pending.Id).GetString("token"))
}

// A capable agent whose record still carries a token the hub did not issue (the last host
// left on an expired enrollment token) is migrated as well.
func TestUpgradedAgentOnALeftoverSharedTokenIsMigrated(t *testing.T) {
	env := newIdentityTestEnv(t)
	const leftover = "ExpiredEnrollmentTokenStillOnThisHost00"
	_, err := createTestRecord(env.hub, "agents", map[string]any{"name": "last", "token": leftover, "fingerprint": "last-host-fp"})
	require.NoError(t, err)
	t.Setenv(appmeta.AgentEnvPrefix+"TOKEN", leftover)
	env.startHost(t, "last-host-fp")

	rec := env.enrolled(t, "last-host-fp")
	assert.NotEqual(t, leftover, rec.GetString("token"))
}

// Only records whose token the hub issued count as holding their fingerprint; agents that
// predate SetAgentToken keep re-attaching through the shared token until they upgrade.
func TestOnlyIssuedTokensHoldAFingerprint(t *testing.T) {
	hub, testApp, err := createTestHub(t)
	require.NoError(t, err)
	defer cleanupTestHub(hub, testApp)

	_, err = createTestRecord(hub, "agents", map[string]any{"name": "legacy", "token": "old-shared-token", "fingerprint": "legacy-fp"})
	require.NoError(t, err)
	assert.Nil(t, hub.recordHeldByTokenAgent("legacy-fp"))

	issued, err := createTestRecord(hub, "agents", map[string]any{"name": "new", "token": "unique-token", "fingerprint": "new-fp", "token_issued": true})
	require.NoError(t, err)
	held := hub.recordHeldByTokenAgent("new-fp")
	require.NotNil(t, held)
	assert.Equal(t, issued.Id, held.Id)
}

// Guard rails on the admin decisions: no rotation of a pending host, no merge while the host
// it claims to be is connected (the strongest sign of impersonation) unless forced.
func TestApprovalGuards(t *testing.T) {
	env := newIdentityTestEnv(t)
	host, err := createTestRecord(env.hub, "agents", map[string]any{"name": "web1", "token": "t1", "fingerprint": "fp1", "status": "connected", "token_issued": true})
	require.NoError(t, err)
	pending, err := createTestRecord(env.hub, "agents", map[string]any{"name": "web1", "token": "e", "fingerprint": "fp1", "status": agentStatusAwaitingApproval, "duplicate_of": host.Id})
	require.NoError(t, err)

	res := env.post(t, "/api/app/agents/"+pending.Id+"/rotate-token")
	assert.Equal(t, http.StatusConflict, res.Code, res.Body.String())

	env.hub.agentConns.Store(host.Id, &ws.WsConn{})
	defer env.hub.agentConns.Delete(host.Id)
	res = env.post(t, "/api/app/agents/"+pending.Id+"/merge")
	assert.Equal(t, http.StatusConflict, res.Code)
	assert.Contains(t, res.Body.String(), "connected right now")
	assert.Equal(t, "t1", agentRecordByID(env.hub, host.Id).GetString("token"))
}
