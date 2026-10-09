//go:build testing

package hub

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/pocketbase/pocketbase/apis"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/router"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The role a route requires.
type routeAccess int

const (
	accessPublic      routeAccess = iota // no auth (agent connect, push heartbeats, first run)
	accessAuth                           // any authenticated user, readonly included
	accessNonReadonly                    // excludeReadOnlyRole: user and admin
	accessAdmin                          // requireAdminRole
)

// routeAccessTable lists every route Vigil adds to PocketBase's router, with the role it
// requires. TestRouteTableMatchesTheRouter fails when a route is added without an entry.
var routeAccessTable = map[string]routeAccess{
	"POST /api/app/create-user":      accessPublic, // registered only while there is no user
	"GET /api/app/first-run":         accessPublic,
	"GET /api/app/agent-connect":     accessPublic,
	"GET /api/app/push/{pushToken}":  accessPublic,
	"POST /api/app/push/{pushToken}": accessPublic,

	"GET /api/app/api-keys":         accessAuth,
	"POST /api/app/api-keys":        accessAuth,
	"DELETE /api/app/api-keys/{id}": accessAuth,
	"GET /api/mcp":                  accessAuth,
	"POST /api/mcp":                 accessAuth,
	"DELETE /api/mcp":               accessAuth,
	"GET /api/app/info":             accessAuth,
	"GET /api/app/update":           accessAuth, // registered only with CHECK_UPDATES=true

	"GET /api/app/agent-enrollment-token":                             accessNonReadonly,
	"POST /api/app/agent-enrollment-token":                            accessNonReadonly,
	"GET /api/app/agent-tokens":                                       accessNonReadonly,
	"POST /api/app/agents/{id}/rotate-token":                          accessNonReadonly,
	"POST /api/app/agents/{id}/approve":                               accessAdmin,
	"POST /api/app/agents/{id}/merge":                                 accessAdmin,
	"POST /api/app/agents/{id}/reject":                                accessAdmin,
	"GET /api/app/dashboard":                                          accessAuth,
	"GET /api/app/hosts-overview":                                     accessAuth,
	"GET /api/app/hosts/{id}":                                         accessAuth,
	"GET /api/app/hosts/{id}/metrics":                                 accessAuth,
	"GET /api/app/hosts/{id}/maintenance":                             accessAuth,
	"GET /api/app/fleet-metrics":                                      accessAuth,
	"GET /api/app/hosts/{id}/container-metrics":                       accessAuth,
	"GET /api/app/hosts/{id}/container-metrics/latest":                accessAuth,
	"GET /api/app/hosts/{id}/container-metrics/by-name/{name}":        accessAuth,
	"GET /api/app/hosts/{id}/container-metrics/by-name/{name}/latest": accessAuth,
	"POST /api/app/refresh-snapshots":                                 accessNonReadonly,

	"GET /api/app/monitors":                  accessAuth,
	"GET /api/app/monitors/{id}":             accessAuth,
	"POST /api/app/monitors":                 accessNonReadonly,
	"PUT /api/app/monitors/{id}":             accessNonReadonly,
	"POST /api/app/monitors/{id}/move":       accessNonReadonly,
	"DELETE /api/app/monitors/{id}":          accessNonReadonly,
	"GET /api/app/monitors/{id}/events":      accessAuth,
	"GET /api/app/monitors/{id}/series":      accessAuth,
	"GET /api/app/monitors/{id}/maintenance": accessAuth,
	"GET /api/app/monitor-groups":            accessAuth,
	"POST /api/app/monitor-groups":           accessNonReadonly,
	"PUT /api/app/monitor-groups/{id}":       accessNonReadonly,
	"DELETE /api/app/monitor-groups/{id}":    accessNonReadonly,

	"GET /api/app/notifications/channels":            accessAdmin,
	"POST /api/app/notifications/channels":           accessAdmin,
	"PATCH /api/app/notifications/channels/{id}":     accessAdmin,
	"DELETE /api/app/notifications/channels/{id}":    accessAdmin,
	"POST /api/app/notifications/channels/{id}/test": accessAdmin,
	"GET /api/app/notifications/rules":               accessAdmin,
	"POST /api/app/notifications/rules":              accessAdmin,
	"PATCH /api/app/notifications/rules/{id}":        accessAdmin,
	"DELETE /api/app/notifications/rules/{id}":       accessAdmin,
	"GET /api/app/notifications/logs":                accessAdmin,
	"GET /api/app/metric-alerts":                     accessAdmin,
	"PUT /api/app/metric-alerts":                     accessAdmin,
	"DELETE /api/app/metric-alerts/{id}":             accessAdmin,
	"GET /api/app/disk-monitor-rules":                accessAdmin,
	"PUT /api/app/disk-monitor-rules":                accessAdmin,
	"DELETE /api/app/disk-monitor-rules/{id}":        accessAdmin,

	"GET /api/app/system-notifications":               accessAuth,
	"GET /api/app/system-notifications/unread":        accessAuth,
	"POST /api/app/system-notifications/read-all":     accessAuth,
	"GET /api/app/system-notifications/preferences":   accessAuth,
	"PATCH /api/app/system-notifications/preferences": accessAuth,

	"GET /api/app/jobs":                              accessAdmin,
	"PATCH /api/app/jobs/{key}":                      accessAdmin,
	"POST /api/app/jobs/{key}/run":                   accessAdmin,
	"GET /api/app/registry-credentials":              accessAdmin,
	"POST /api/app/registry-credentials":             accessAdmin,
	"PATCH /api/app/registry-credentials/{id}":       accessAdmin,
	"DELETE /api/app/registry-credentials/{id}":      accessAdmin,
	"GET /api/app/container-audit-overrides":         accessAdmin,
	"PUT /api/app/container-audit-overrides":         accessAdmin,
	"DELETE /api/app/container-audit-overrides/{id}": accessAdmin,
	"GET /api/app/maintenance-windows":               accessAdmin,
	"POST /api/app/maintenance-windows":              accessAdmin,
	"PUT /api/app/maintenance-windows/{id}":          accessAdmin,
	"DELETE /api/app/maintenance-windows/{id}":       accessAdmin,
	"GET /api/app/maintenance/active":                accessAuth,
	"GET /api/app/purge/settings":                    accessAdmin,
	"PATCH /api/app/purge/settings":                  accessAdmin,
	"POST /api/app/purge/run":                        accessAdmin,
}

// credentialRoutes return secrets (agent tokens, the enrollment token): a read-scoped API
// key, which promises to read data only, is refused there (rejectReadOnlyApiKey).
var credentialRoutes = map[string]bool{
	"GET /api/app/agent-enrollment-token": true,
	"GET /api/app/agent-tokens":           true,
}

// routeTestServer is the hub's router as served, built once.
type routeTestServer struct {
	mux    http.Handler
	routes []string // the routes Vigil adds to PocketBase's
}

func newRouteTestServer(t *testing.T, app core.App, hub *Hub) routeTestServer {
	t.Helper()
	app.Settings().RateLimits.Enabled = false // hundreds of calls from one address
	base, err := apis.NewRouter(app)
	require.NoError(t, err)
	pocketbase := map[string]bool{}
	for _, route := range routerRoutes(base.RouterGroup) {
		pocketbase[route] = true
	}

	se := &core.ServeEvent{App: app}
	se.Router, err = apis.NewRouter(app)
	require.NoError(t, err)
	hub.registerMiddlewares(se)
	require.NoError(t, hub.registerApiRoutes(se))
	var routes []string
	for _, route := range routerRoutes(se.Router.RouterGroup) {
		if !pocketbase[route] {
			routes = append(routes, route)
		}
	}
	mux, err := se.Router.BuildMux()
	require.NoError(t, err)
	return routeTestServer{mux: mux, routes: routes}
}

// routerRoutes lists every "METHOD /path" of a router group and its subgroups, wherever
// and however they were registered. The router keeps them unexported: read by reflection.
func routerRoutes(group *router.RouterGroup[*core.RequestEvent]) []string {
	var routes []string
	var walk func(v reflect.Value, prefix string)
	walk = func(v reflect.Value, prefix string) {
		prefix += v.FieldByName("Prefix").String()
		children := v.FieldByName("children")
		for i := range children.Len() {
			child := children.Index(i).Elem().Elem() // any → pointer → struct
			if child.FieldByName("children").IsValid() {
				walk(child, prefix)
				continue
			}
			method := child.FieldByName("Method").String()
			if method == "" {
				method = "ANY"
			}
			routes = append(routes, method+" "+prefix+child.FieldByName("Path").String())
		}
	}
	walk(reflect.ValueOf(group).Elem(), "")
	return routes
}

func (s routeTestServer) call(method, url, auth string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, url, strings.NewReader("{}"))
	req.Header.Set("Content-Type", "application/json")
	if auth != "" {
		req.Header.Set("Authorization", auth)
	}
	res := httptest.NewRecorder()
	s.mux.ServeHTTP(res, req)
	return res
}

func TestRouteTableMatchesTheRouter(t *testing.T) {
	t.Setenv("CHECK_UPDATES", "true")
	hub, testApp, err := createTestHub(t)
	require.NoError(t, err)
	defer cleanupTestHub(hub, testApp)
	server := newRouteTestServer(t, testApp, hub) // no user yet: create-user is registered

	seen := map[string]bool{}
	for _, route := range server.routes {
		if _, ok := routeAccessTable[route]; !ok {
			t.Errorf("new route %s: add it to routeAccessTable with the role it requires", route)
		}
		seen[route] = true
	}
	for route := range routeAccessTable {
		assert.True(t, seen[route], "%s is no longer registered: remove it from routeAccessTable", route)
	}
	for route := range credentialRoutes {
		_, ok := routeAccessTable[route]
		assert.True(t, ok, route)
	}
}

const (
	roleMiddlewareDenied = "The authorized record is not allowed to perform this action."
	readKeyDenied        = "This API key is read-only"
)

// Every route answers each caller as its table entry says: RequireAuth refuses anonymous
// callers, the role middlewares refuse exactly the roles below the required one, and a
// read-scoped API key gets safe methods only, credentials excepted.
//
// Routes are really called, with a "{}" body and nonexistent ids: a route that would act on
// such a request (e.g. purge everything) must be called with a body it rejects.
func TestRouteRoles(t *testing.T) {
	t.Setenv("CHECK_UPDATES", "true")
	hub, testApp, err := createTestHub(t)
	require.NoError(t, err)
	defer cleanupTestHub(hub, testApp)
	server := newRouteTestServer(t, testApp, hub)

	callers := map[string]string{}
	users := map[string]*core.Record{}
	for _, role := range []string{"readonly", "user", "admin"} {
		user, err := createTestRecord(hub, "users", map[string]any{"email": role + "@example.com", "password": "password123", "role": role})
		require.NoError(t, err)
		users[role] = user
		callers[role], err = user.NewAuthToken()
		require.NoError(t, err)
	}
	superuser, err := createTestRecord(hub, core.CollectionNameSuperusers, map[string]any{"email": "root@example.com", "password": "password123"})
	require.NoError(t, err)
	callers["superuser"], err = superuser.NewAuthToken()
	require.NoError(t, err)
	callers["admin read key"] = newTestApiKey(t, hub, users["admin"].Id, apiScopeRead)
	callers["admin read-write key"] = newTestApiKey(t, hub, users["admin"].Id, apiScopeReadWrite)

	allowed := func(access routeAccess, caller string) bool {
		switch access {
		case accessAdmin:
			// requireAdminRole reads users.role: superusers administer PocketBase, not the app.
			return caller == "admin" || caller == "admin read-write key" || caller == "admin read key"
		case accessNonReadonly:
			return caller != "readonly"
		default:
			return true
		}
	}
	concrete := strings.NewReplacer("{id}", "nonexistent", "{key}", "nonexistent", "{name}", "nonexistent", "{pushToken}", "nonexistent")

	routes := make([]string, 0, len(routeAccessTable))
	for route := range routeAccessTable {
		routes = append(routes, route)
	}
	sort.Strings(routes)
	for _, route := range routes {
		access := routeAccessTable[route]
		method, path, _ := strings.Cut(route, " ")
		url := concrete.Replace(path)
		t.Run(route, func(t *testing.T) {
			res := server.call(method, url, "")
			if access == accessPublic {
				assert.NotContains(t, []int{http.StatusUnauthorized, http.StatusNotFound, http.StatusMethodNotAllowed}, res.Code, res.Body.String())
				return
			}
			assert.Equal(t, http.StatusUnauthorized, res.Code, "anonymous: %s", res.Body.String())
			switch route {
			case "GET /api/mcp", "POST /api/mcp", "DELETE /api/mcp":
				return // a session-based stream: TestMcpEndpoint covers it authenticated
			case "GET /api/app/update":
				return // would call GitHub; RequireAuth only (update_check_test.go)
			}
			for caller, auth := range callers {
				res := server.call(method, url, auth)
				body := res.Body.String()
				readKey := caller == "admin read key"
				switch {
				case readKey && method != http.MethodGet:
					assert.Equal(t, http.StatusForbidden, res.Code, "%s: %s", caller, body)
					assert.Contains(t, body, readKeyDenied, caller)
				case readKey && credentialRoutes[route]:
					assert.Equal(t, http.StatusForbidden, res.Code, "%s: %s", caller, body)
					assert.Contains(t, body, "cannot read credentials", caller)
				case allowed(access, caller):
					assert.NotEqual(t, http.StatusUnauthorized, res.Code, "%s: %s", caller, body)
					assert.NotContains(t, body, roleMiddlewareDenied, caller)
					assert.NotContains(t, body, readKeyDenied, caller)
				default:
					assert.Equal(t, http.StatusForbidden, res.Code, "%s: %s", caller, body)
					assert.Contains(t, body, roleMiddlewareDenied, caller)
				}
			}
		})
	}
}

func newTestApiKey(t *testing.T, hub *Hub, userID, scope string) string {
	t.Helper()
	token := "vk_" + strings.ReplaceAll(scope, "-", "") + "_0123456789abcdef0123456789abcdef"
	col, err := hub.FindCachedCollectionByNameOrId("user_api_keys")
	require.NoError(t, err)
	rec := core.NewRecord(col)
	rec.Set("name", scope)
	rec.Set("created_by", userID)
	sum := sha256.Sum256([]byte(token))
	rec.Set("token_hash", hex.EncodeToString(sum[:]))
	rec.Set("scope", scope)
	require.NoError(t, hub.SaveNoValidate(rec))
	return "Bearer " + token
}

// collectionRules is every rule of every Vigil collection (list, view, create, update,
// delete; nil = superusers only). A collection without an entry fails the test.
var collectionRules = map[string][5]*string{}

func init() {
	nobody := (*string)(nil)
	authed := ptr(`@request.auth.id != ""`)
	admin := ptr(`@request.auth.role = "admin"`)
	writer := ptr(`@request.auth.id != "" && @request.auth.role != "readonly"`)
	own := ptr(`@request.auth.id != "" && user = @request.auth.id`)
	readAuthed := [5]*string{authed, authed, nobody, nobody, nobody}
	readAdmin := [5]*string{admin, admin, nobody, nobody, nobody}
	closed := [5]*string{nobody, nobody, nobody, nobody, nobody}
	collectionRules = map[string][5]*string{
		// Users read themselves only; nobody but superusers creates or edits a user (a
		// user editing its own record could make itself admin).
		"users":                     {ptr("id = @request.auth.id"), ptr("id = @request.auth.id"), nobody, nobody, nobody},
		"user_settings":             {own, nobody, own, own, nobody},
		"agents":                    {authed, authed, nobody, writer, writer},
		"agent_enrollment_tokens":   closed,
		"scheduled_jobs":            readAdmin,
		"container_image_audits":    readAuthed,
		"registry_credentials":      closed,
		"container_audit_overrides": readAuthed,
		"system_notifications":      readAuthed,
		"host_metric_samples":       readAuthed,
		"host_metric_current":       readAuthed,
		"container_metric_samples":  readAuthed,
		"metric_alerts":             closed,
		"host_snapshots":            readAuthed,
		"user_api_keys":             closed,
		"notification_mutes":        {authed, authed, writer, writer, writer},
		"maintenance":               readAuthed,
		"disk_monitor_rules":        closed,
		"monitor_groups":            readAuthed,
		"monitors":                  readAuthed,
		"monitor_events":            readAuthed,
		// The config holds secrets: only /api/app/notifications (which redacts them) reads it.
		"notification_channels":       closed,
		"notification_rules":          readAdmin,
		"notification_logs":           readAdmin,
		"data_retention_settings":     readAdmin,
		core.CollectionNameSuperusers: closed,
	}
}

func ptr(s string) *string { return &s }

func TestCollectionRules(t *testing.T) {
	hub, testApp, err := createTestHub(t)
	require.NoError(t, err)
	defer cleanupTestHub(hub, testApp)
	collections, err := hub.FindAllCollections()
	require.NoError(t, err)
	show := func(rule *string) string {
		if rule == nil {
			return "<superusers only>"
		}
		return *rule
	}
	names := []string{"list", "view", "create", "update", "delete"}
	for _, c := range collections {
		if c.System && c.Name != core.CollectionNameSuperusers {
			continue
		}
		want, ok := collectionRules[c.Name]
		if !assert.True(t, ok, "new collection %s: add its rules to collectionRules", c.Name) {
			continue
		}
		got := [5]*string{c.ListRule, c.ViewRule, c.CreateRule, c.UpdateRule, c.DeleteRule}
		for i := range got {
			assert.Equal(t, show(want[i]), show(got[i]), fmt.Sprintf("%s %s rule", c.Name, names[i]))
		}
	}
}

// A readonly user's API keys are read keys, whatever scope they were created with (or the
// user had when they were): a read-write key would let it write through the MCP server,
// whose write tools are chosen by the key scope alone.
func TestReadonlyUsersHoldReadKeysOnly(t *testing.T) {
	hub, testApp, err := createTestHub(t)
	require.NoError(t, err)
	defer cleanupTestHub(hub, testApp)
	server := newRouteTestServer(t, testApp, hub)
	tokenFor := func(role string) (*core.Record, string) {
		user, err := createTestRecord(hub, "users", map[string]any{"email": role + "@example.com", "password": "password123", "role": role})
		require.NoError(t, err)
		token, err := user.NewAuthToken()
		require.NoError(t, err)
		return user, token
	}
	readonly, readonlyToken := tokenFor("readonly")
	_, userToken := tokenFor("user")
	create := func(token, scope string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, "/api/app/api-keys", strings.NewReader(`{"name":"k","scope":"`+scope+`"}`))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", token)
		res := httptest.NewRecorder()
		server.mux.ServeHTTP(res, req)
		return res
	}

	res := create(readonlyToken, apiScopeReadWrite)
	assert.Equal(t, http.StatusForbidden, res.Code, res.Body.String())
	assert.Equal(t, http.StatusOK, create(readonlyToken, apiScopeRead).Code)
	assert.Equal(t, http.StatusOK, create(userToken, apiScopeReadWrite).Code)

	// A read-write key of a user since made readonly acts as a read key.
	key := newTestApiKey(t, hub, readonly.Id, apiScopeReadWrite)
	res = server.call(http.MethodPost, "/api/app/system-notifications/read-all", key)
	assert.Equal(t, http.StatusForbidden, res.Code, res.Body.String())
	assert.Contains(t, res.Body.String(), readKeyDenied)
}

// The auth collections' other rules, and the fields that hold secrets in collections any
// authenticated user reads: hidden, so the collection API and realtime never return them.
func TestAuthRulesAndHiddenSecrets(t *testing.T) {
	hub, testApp, err := createTestHub(t)
	require.NoError(t, err)
	defer cleanupTestHub(hub, testApp)

	for name, authRule := range map[string]string{
		"users":                       "verified=true", // only verified accounts sign in
		core.CollectionNameSuperusers: "",
	} {
		c, err := hub.FindCollectionByNameOrId(name)
		require.NoError(t, err)
		assert.Nil(t, c.ManageRule, "%s manage rule: nobody manages accounts through the API", name)
		require.NotNil(t, c.AuthRule, name)
		assert.Equal(t, authRule, *c.AuthRule, "%s auth rule", name)
	}
	for collection, field := range map[string]string{
		"agents":        "token",
		"monitors":      "push_token",
		"user_api_keys": "token_hash",
	} {
		c, err := hub.FindCollectionByNameOrId(collection)
		require.NoError(t, err)
		f := c.Fields.GetByName(field)
		require.NotNil(t, f, collection+"."+field)
		assert.True(t, f.GetHidden(), "%s.%s must stay hidden", collection, field)
	}
}
