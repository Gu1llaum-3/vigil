# AGENTS.md — Vigil

## Project Overview

This is the **Vigil** project, a hub/agent patch audit application built on top of [PocketBase](https://pocketbase.io).
The hub is the central server (database, REST API, web UI, WebSocket endpoint) for monitoring patches and compliance.
Agents are lightweight processes deployed on remote systems that collect patch/package data and connect outbound to the hub.

The hub **pulls** data from agents — agents never push proactively.

---

## Documentation Routing

Use `docs/` as the detailed source of truth. Use this file as the high-signal summary and routing layer.

Use targeted doc reads. Do not read the entire `docs/` tree for every task.

When a task clearly matches one of the docs below, read the relevant doc before editing. If the task is small and obviously isolated, use judgment and read only what is needed.

- Start with `docs/README.md` for overall navigation.
- Read `docs/project-overview.md` when you need the boilerplate mental model or want to understand what should stay generic.
- Read `docs/architecture/hub-agent-architecture.md` for hub/agent runtime flow, WebSocket lifecycle, and protocol changes.
- Read `docs/architecture/auth-and-data-model.md` for users, roles, settings, tokens, collections, and environment behavior.
- Read `docs/backend/hub-backend.md` for hub startup, API routes, middleware, PocketBase integration, heartbeat, and update behavior.
- Read `docs/agent/agent-runtime.md` for agent CLI, fingerprinting, data-dir behavior, handshake verification, and handler changes.
- Read `docs/frontend/frontend-app.md` for routing, injected app metadata, PocketBase JS usage, login flows, settings UI, stores, and i18n.
- Read `docs/development/workflow-and-testing.md` before running builds, dev flows, or tests.
- Read `docs/customization/renaming-and-derived-projects.md` for rename-sensitive work in derived projects.
- Read `docs/conventions-and-gotchas.md` for repo-specific rules that can cause regressions.
- Read `docs/operations/deployment-and-packaging.md` for Docker, scripts, systemd, Helm, releases, and self-update behavior.
- Read `docs/troubleshooting/common-issues.md` when diagnosing recurring repo-specific failures.
- Read `docs/ai/agent-navigation.md` when you need the fastest task-to-file routing.

### Task Routing

- If the task is “add or change a hub API endpoint”, read `docs/backend/hub-backend.md` and `docs/architecture/auth-and-data-model.md`.
- If the task is “change auth, roles, users, tokens, or settings”, read `docs/architecture/auth-and-data-model.md` first.
- If the task is “add or change a hub-to-agent action”, read `docs/architecture/hub-agent-architecture.md`, `docs/agent/agent-runtime.md`, and `docs/conventions-and-gotchas.md`.
- If the task is “change the agent handshake or connection lifecycle”, read `docs/architecture/hub-agent-architecture.md` and `docs/agent/agent-runtime.md`.
- If the task is “change the frontend app shell or settings UI”, read `docs/frontend/frontend-app.md`.
- If the task is “change the dashboard home page, snapshot data, or chart display”, read `docs/frontend/frontend-app.md` and `docs/backend/hub-backend.md`.
- If the task is “change snapshot collection or the host_snapshots collection”, read `docs/architecture/hub-agent-architecture.md`, `docs/agent/agent-runtime.md`, and `docs/backend/hub-backend.md`.
- If the task is “add or change a monitor type, monitor scheduler behavior, or monitor API”, read `docs/backend/hub-backend.md` (Monitor Scheduler section) and `docs/architecture/auth-and-data-model.md`.
- If the task is “change the monitors page, monitor groups UI, or push heartbeat flow”, read `docs/frontend/frontend-app.md` (Monitors Route section) and `docs/backend/hub-backend.md`.
- If the task is “add or change a notification channel, rule, or log — or change dispatcher, providers, throttle, or retry behavior”, read `docs/backend/hub-backend.md` (Notification Dispatcher section), `docs/architecture/auth-and-data-model.md` (notification collections), and `docs/conventions-and-gotchas.md` (no-hook pattern).
- If the task is “change the notifications settings UI or notification logs history UI”, read `docs/frontend/frontend-app.md` and `docs/backend/hub-backend.md`.
- If the task is “add or change a scheduled job, cron job, or jobs settings UI”, read `docs/backend/hub-backend.md`, `docs/architecture/auth-and-data-model.md`, and `docs/frontend/frontend-app.md`.
- If the task is “change retention, purge, or data lifecycle behavior”, read `docs/backend/hub-backend.md`, `docs/architecture/auth-and-data-model.md`, and `docs/frontend/frontend-app.md` when the settings UI is affected.
- If the task is “change build, dev, or test workflow”, read `docs/development/workflow-and-testing.md`.
- If the task is “rename or derive a new product from Nexus”, read `docs/customization/renaming-and-derived-projects.md`.
- If the task is “deployment, packaging, or release work”, read `docs/operations/deployment-and-packaging.md`.

---

## Architecture

```
┌──────────────────────────────────────┐
│  Hub (PocketBase + WebSocket server) │
│  - REST/realtime API                 │
│  - Web UI (embedded Vite/React)      │
│  - Agent connection manager          │
└────────────────┬─────────────────────┘
                 │ WebSocket (outbound from agent)
     ┌───────────▼──────────┐
     │  Agent               │
     │  - handler registry  │
     │  - fingerprint       │
     └──────────────────────┘
```

**Key design decisions:**
- Agents make outbound connections only → works behind firewalls
- All communication is WebSocket over CBOR encoding
- The hub cryptographically verifies its own identity to each agent at connection time (SSH challenge over WebSocket)
- No SSH server fallback — WebSocket is the only transport

---

## Directory Structure

```
.
├── agent/                          # Agent process
│   ├── agent.go                    # Agent struct, Start()
│   ├── client.go                   # WebSocket client, auth challenge handling
│   ├── connection_manager.go       # State machine (Disconnected ↔ WebSocketConnected)
│   ├── handlers.go                 # Handler registry + built-in handlers (incl. GetHostSnapshotHandler)
│   ├── keys.go                     # ParseKeys() for SSH public key parsing
│   ├── response.go                 # newAgentResponse() helper
│   ├── fingerprint.go              # Stable agent identity (persisted to disk)
│   ├── collectors/                 # Linux-only system collectors + snapshot orchestrator
│   │   ├── snapshot.go             # Orchestrates all collectors → HostSnapshotResponse
│   │   ├── system.go               # OS info, CPU, memory (linux)
│   │   ├── storage.go              # Mounted filesystems (linux)
│   │   ├── packages_debian.go      # APT packages and pending updates (linux)
│   │   ├── packages_redhat.go      # DNF/YUM packages and pending updates (linux)
│   │   ├── repositories_debian.go  # APT repo sources (linux)
│   │   ├── repositories_redhat.go  # DNF/YUM repo sources (linux)
│   │   ├── reboot.go               # Reboot-required detection (linux)
│   │   ├── docker.go               # Docker container inventory (linux)
│   │   └── *_stub.go               # No-op stubs for non-Linux builds
│   └── health/                     # Health check endpoint
│
├── internal/
│   ├── cmd/
│   │   ├── agent/agent.go          # Agent CLI entry point
│   │   └── hub/hub.go              # Hub CLI entry point
│   │
│   ├── common/
│   │   └── common-ws.go            # Shared WebSocket types (actions, request/response structs)
│   │
│   ├── hub/
│   │   ├── hub.go                  # Hub struct, GetSSHKey(), MakeLink(), agentConns sync.Map
│   │   ├── agent_connect.go        # WebSocket handshake, lifecycle management, snapshot on connect
│   │   ├── api.go                  # API routes, middlewares
│   │   ├── collections.go          # Collection auth settings
│   │   ├── snapshots.go            # upsertHostSnapshot(), refreshSnapshots() handler
│   │   ├── dashboard.go            # getDashboard() handler
│   │   ├── jobs.go                 # Scheduled job registry + persisted job state
│   │   ├── jobs_api.go             # Scheduled jobs admin API handlers
│   │   ├── monitors.go             # MonitorScheduler, check goroutines (HTTP/TCP/DNS/push)
│   │   ├── monitors_api.go         # Monitor REST API handlers and response types
│   │   ├── server.go               # PublicAppInfo, HTML injection
│   │   ├── transport/              # Transport abstraction (WebSocket only; SSHTransport is dead code — see Known Leftovers)
│   │   └── ws/                     # WebSocket connection management and hub-side handlers
│   │
│   ├── migrations/                 # PocketBase schema migrations (Go files)
│   │   ├── 0_collections_snapshot_*.go  # Initial collections
│   │   ├── initial-settings.go
│   │   ├── 2_create_host_snapshots.go   # host_snapshots collection
│   │   └── 3_create_monitors.go         # monitor_groups, monitors, monitor_events collections
│   ├── site/                       # Frontend (React/Vite)
│   └── tests/                      # Shared test helpers (build tag: testing)
```

---

## WebSocket Protocol

Actions are defined as `uint8` constants in `internal/common/common-ws.go`.
**Order matters** — values are iota-assigned and encoded on the wire.

| Value | Constant | Direction | Description |
|-------|----------|-----------|-------------|
| 0 | `GetAgentInfo` | Hub → Agent | Fetch version, capabilities, metadata |
| 1 | `CheckFingerprint` | Hub → Agent | Hub identity challenge (SSH signature) |
| 2 | `Ping` | Hub → Agent | Liveness check |
| 3 | `GetHostSnapshot` | Hub → Agent | Full system snapshot (OS, resources, storage, packages, repos, reboot, Docker) |

**Adding a new action:**
1. Add a constant in `internal/common/common-ws.go` (append only — never reorder)
2. Add the handler struct + `Handle()` in `agent/handlers.go`
3. Register it in `NewHandlerRegistry()` in `agent/handlers.go`
4. Add the hub-side call method on `*ws.WsConn` in `internal/hub/ws/handlers.go`

**Example — adding a `GetMetrics` action:**

```go
// internal/common/common-ws.go
const (
    GetAgentInfo     WebSocketAction = iota // 0
    CheckFingerprint                        // 1
    Ping                                    // 2
    GetMetrics                              // 3 ← new
)

// Add response type
type MetricsResponse struct {
    CPU    float64 `cbor:"cpu"`
    Memory float64 `cbor:"memory"`
}
```

```go
// agent/handlers.go — add handler
type GetMetricsHandler struct{}

func (h *GetMetricsHandler) Handle(hctx *HandlerContext) error {
    metrics := map[string]any{
        "cpu":    getCPUUsage(),
        "memory": getMemoryUsage(),
    }
    return hctx.SendResponse(metrics, hctx.RequestID)
}

// register in NewHandlerRegistry():
registry.Register(common.GetMetrics, &GetMetricsHandler{})
```

```go
// internal/hub/ws/handlers.go — add hub-side caller
func (ws *WsConn) GetMetrics(ctx context.Context) (common.MetricsResponse, error) {
    req, err := ws.requestManager.SendRequest(ctx, common.GetMetrics, nil)
    // ... (follow GetAgentInfo pattern)
}
```

---

## Connection Lifecycle

```
agent.Start(keys)
  └─ connectionManager.Start()
       ├─ newWebSocketClient()         // reads HUB_URL + TOKEN from env
       ├─ connect()
       │    └─ wsClient.Connect()      // outbound WS to /api/app/agent-connect
       └─ event loop (ticker + DownChan)

Hub side (agent_connect.go):
  handleAgentConnect()
    └─ agentConnect()                  // validate headers, upgrade to WS
         └─ verifyWsConn() [goroutine]
              ├─ GetFingerprint()      // hub signs token → agent verifies
              ├─ findOrUpsertAgent()   // upsert agents record in DB; store WsConn in Hub.agentConns
              ├─ GetAgentInfo()        // fetch version/capabilities/metadata
              ├─ GetHostSnapshot()     // collect full snapshot (60s timeout) → upsertHostSnapshot()
              └─ manageAgentLifecycle() [goroutine]
                   ├─ Ping every 30s
                   ├─ DownChan → status=offline
                   └─ delete from Hub.agentConns on disconnect
```

**All handlers that are not `CheckFingerprint` require `HubVerified = true`.**
The agent sets `hubVerified = true` only after successfully verifying the hub's SSH signature.

---

## Database Collections

Defined in `internal/migrations/0_collections_snapshot_*.go`.

| Collection | ID | Notes |
|---|---|---|
| `users` | `_pb_users_auth_` | Auth collection. Role: `user`, `admin`, `readonly` |
| `user_settings` | `4afacsdnlu8q8r2` | One record per user. JSON `settings` field |
| `agents` | `pbc_4000000001` | One record per agent. Key fields: `token`, `fingerprint`, `status`, `capabilities`, `metadata` |
| `agent_enrollment_tokens` | `pbc_4000000002` | One per user (unique index on `created_by`) |
| `host_snapshots` | — | One record per agent (unique index). Relation to `agents`, JSON `data` field. Created by migration `2_create_host_snapshots.go` |
| `monitor_groups` | `pbc_5000000001` | Monitor grouping. Fields: `name`, `weight`. Created by migration `3_create_monitors.go` |
| `monitors` | `pbc_5000000002` | One record per uptime monitor. Type: `http`/`tcp`/`dns`/`push`. Status written by scheduler via `SaveNoValidate`. Created by migration `3_create_monitors.go` |
| `monitor_events` | `pbc_5000000003` | Append-only check history. Relation to `monitors` (cascadeDelete=true). Indexed on `(monitor, checked_at)`. Created by migration `3_create_monitors.go` |
| `notification_channels` | `pbc_6000000001` | One record per notification destination. Kind: `email`/`webhook`/`slack`/`teams`/`gchat`/`ntfy`/`gotify`/`in-app`. Sensitive config redacted in API. Extended by migration `6_notification_in_app.go`. |
| `notification_rules` | `pbc_6000000002` | Routing rules: events array, optional resource filter, multi-relation to channels, throttle_seconds. Multi-channel persistence corrected by migration `7_notification_rule_channels_multi.go`. |
| `notification_logs` | `pbc_6000000003` | Append-only delivery log written by dispatcher via `SaveNoValidate`. Includes `created_by` and `channel_kind` for frontend realtime toast filtering. Indexed on `(rule, sent_at)` and `(resource_id, sent_at)`, plus `(created_by, sent_at)` via migration `6_notification_in_app.go`. |
| `data_retention_settings` | `pbc_6000000004` | Global singleton-like lifecycle settings for automatic retention and manual purge defaults. |
| `scheduled_jobs` | `pbc_6000000005` | Admin-visible persisted state for registered scheduled jobs. Stores `key`, `schedule`, last run/success/error, last result payload, and duration. |
| `container_image_audits` | `pbc_6000000006` | Latest read-only image audit result per `(agent, container_id)` for public Docker / GHCR images discovered in snapshots. Stores normalized image metadata, audit policy, status, latest candidate tag/digest, and last check error/timestamp. Created by migration `13_create_container_image_audits.go`. |

**Adding a collection:**
1. Edit the JSON in the migration file — or create a new migration file
2. Use `ImportCollectionsByMarshaledJSON(data, false)` for additive migrations (non-destructive) or `(data, true)` to delete collections absent from the snapshot
3. Never edit collection IDs — they are stable references

**Updating agent records from hub code — always use `SaveNoValidate`:**
```go
// ✓ correct — skips validation hooks
h.SaveNoValidate(rec)

// ✗ avoid for internal status updates — triggers full validation
h.Save(rec)
```

---

## Hub Authentication Mechanisms

### User auth
- Password auth via email (can be disabled with `DISABLE_PASSWORD_AUTH=true`)
- OAuth2 (user creation controlled by `USER_CREATION=true`)
- MFA/OTP (enabled with `MFA_OTP=true`)

### Agent auth
1. **Enrollment token** — shared token for self-registering new agents (ephemeral 1h or permanent in DB)
2. **Agent token** — per-agent token stored in `agents.token`, checked at every reconnection
3. **Hub identity verification** — hub signs the agent token with its ED25519 private key; agent verifies against hub's public key (`KEY` env var). Prevents impersonation of the hub.

The hub's keypair is stored as `<datadir>/id_ed25519` and generated on first run.
The hub's public key is served at `GET /api/app/info` (authenticated).

---

## Hub Environment Variables

| Variable | Description | Default |
|---|---|---|
| `APP_URL` | Public URL of the hub | `http://localhost:8090` |
| `DISABLE_PASSWORD_AUTH` | Disable email/password login | — |
| `USER_CREATION` | Allow OAuth2 user self-registration | — |
| `MFA_OTP` | Enable MFA/OTP (`true` = all users, `superusers` = superusers only) | — |
| `AUTO_LOGIN` | Trusted email — auto-authenticate this user | — |
| `TRUSTED_AUTH_HEADER` | HTTP header containing a trusted user email | — |
| `HEARTBEAT_URL` | External monitoring endpoint to ping periodically | — |
| `HEARTBEAT_INTERVAL` | Seconds between heartbeat pings | `60` |
| `HEARTBEAT_METHOD` | HTTP method for heartbeat (`GET`, `POST`) | `POST` |
| `CHECK_UPDATES` | Enable GitHub update check endpoint | — |
| `SNAPSHOT_INTERVAL` | Interval between periodic snapshot collections (e.g. `5m`, `10m`, `1h`) | `15m` |

---

## Agent Environment Variables

| Variable | Description | Required |
|---|---|---|
| `HUB_URL` | Full URL of the hub (e.g. `https://hub.example.com`) | Yes |
| `TOKEN` | Enrollment token or agent token | Yes (or `TOKEN_FILE`) |
| `TOKEN_FILE` | Path to a file containing the token | Alt. to `TOKEN` |
| `KEY` | Hub's public key for identity verification | Recommended |
| `KEY_FILE` | Path to a file containing the hub's public key | Alt. to `KEY` |
| `LOG_LEVEL` | `debug`, `warn`, `error` | No |

Without `KEY`/`KEY_FILE`, the agent skips hub identity verification. Acceptable for development; use in production only in trusted network environments.

---

## Build

```bash
# Build both binaries for current OS/ARCH
make build

# Agent only
make build-agent

# Hub only (builds web UI first)
make build-hub

# Cross-compile
OS=linux ARCH=amd64 make build-agent
OS=linux ARCH=arm64 make build-agent

# Dev mode with hot-reload (requires entr)
make dev-hub    # hub on :8090, rebuilds on .go changes (app_data/ created at project root)
make dev-agent  # agent, rebuilds on .go changes
make dev-server # Vite dev server for the frontend

# Run tests
make test
# or
go test -tags=testing ./...
```

Output binaries land in `./build/`.

**Hub dev build** (`make build-hub-dev`) uses `-tags development` which serves the frontend from the Vite dev server instead of the embedded dist folder.

---

## Testing

All test files use the `//go:build testing` build tag.
**The LSP will show "No packages found" warnings for these files — this is expected.**

Always run tests with: `go test -tags=testing ./...`

**Test helpers:**
- `internal/tests/hub.go` — `NewTestHub(t.TempDir())`, `CreateUser()`, `CreateRecord()`
- `internal/hub/agent_connect_test.go` — `createTestHub()`, `createTestRecord()`

**Integration test pattern:**
```go
//go:build testing

func TestSomething(t *testing.T) {
    hub, testApp, err := createTestHub(t)
    require.NoError(t, err)
    defer cleanupTestHub(hub, testApp)

    // create records, simulate requests, assert DB state
}
```

**Agent + hub integration tests** (`TestAgentWebSocketIntegration`) spin up a real HTTP server and a real agent — they test the full WebSocket handshake including `CheckFingerprint`. They require an actual hub SSH keypair to be generated.

---

## Known Leftovers / Technical Debt

- **`internal/hub/transport/transport.go`** — the `Transport` interface abstracts over WebSocket and SSH. Now only WebSocket is used; the interface is still valid and useful but `SSHTransport` is the dead implementation.

---

## Code Conventions

- **No `Save` for internal agent status updates** — use `SaveNoValidate` to avoid triggering validation hooks
- **Always `context.WithTimeout`** when calling agent actions from the hub (the request manager applies a 5s default if no deadline is set, but be explicit)
- **`CollectSnapshot` owns a 45s subprocess timeout** — the context is created inside `CollectSnapshot()` and propagated to collectors that spawn subprocesses (`apt-get`, `dnf`, `rpm`, `needs-restarting`, `docker`). File-only collectors do not receive it. The 45s sits below the hub's 60s WebSocket timeout for `GetHostSnapshot`.
- **Append-only for WebSocket actions** — never reorder or renumber constants in `common-ws.go`; values are encoded on the wire
- **`agent.keys` is nil when no KEY is provided** — `verifySignature` in `client.go` iterates over them; an empty slice means hub verification is skipped silently
- **Multiple agents can share one enrollment token** — there is intentionally no unique constraint on `agents.token`
- **`DownChan` signals disconnect after a 5-second delay** (see `ws.go OnClose`), followed by a 30s grace period in `manageAgentLifecycle` before `status=offline` is written — total ~35s. Ping failures bypass the grace period and mark offline immediately. This prevents spurious notifications on service restarts and upgrades.
- **No PocketBase hooks for monitor scheduler lifecycle** — the scheduler calls `SaveNoValidate` on every check result, which fires PocketBase update events. Adding `OnRecordAfterUpdateSuccess` hooks for the `monitors` collection that call `startMonitor` creates an infinite loop: save → hook → startMonitor → doCheck → save → … . Goroutine lifecycle is managed only from the API handlers (`createMonitor`, `updateMonitor`, `deleteMonitor`) and the `OnRecordAfterDeleteSuccess` hook.
- **Debounce realtime subscriptions for high-frequency collections** — the `monitors` collection is updated on every check cycle. The frontend uses a 1s debounce on the realtime subscription to avoid a flood of re-fetches. Follow this pattern for any other collection updated by a background loop.

---

## Documentation Maintenance

Documentation is part of the implementation in this repository.

When you change behavior, structure, workflows, or conventions, update the affected docs in the same task unless the user explicitly asks you not to.

### When Docs Must Be Updated

Update documentation whenever a change affects:

- runtime architecture or request flow
- WebSocket actions, payloads, or lifecycle behavior
- collections, roles, settings, tokens, or env variables
- hub routes, middleware, startup, or service behavior
- agent startup, fingerprinting, handshake, or health behavior
- frontend routing, auth flows, settings UX, stores, or i18n workflow
- build commands, test commands, or development workflow
- deployment, packaging, install, or update behavior
- rename-sensitive metadata or derived-project guidance
- project-specific conventions, gotchas, or troubleshooting steps

### How To Keep Docs In Sync

- Update the most specific doc first in `docs/`.
- Update this `AGENTS.md` file too if the change affects task routing, global conventions, or the high-signal summary here.
- Keep `README.md` short; do not move detailed contributor guidance there unless the user asks.
- Prefer editing an existing doc over creating a new one unless the topic is clearly new and substantial.
- If a code change invalidates examples or wording in docs, fix the docs in the same change.

### Minimum Documentation Check Before Finishing

Before concluding a non-trivial task, do a targeted documentation impact check.

Do not reread every doc by default. Instead:

1. Identify which subsystem changed.
2. Check only the docs that could realistically be affected.
3. Update those docs if behavior, workflows, conventions, examples, or routing changed.

Use this task-to-doc map for the final documentation check:

- Hub runtime, protocol, or connection lifecycle changes: `docs/architecture/hub-agent-architecture.md`
- Auth, collections, users, roles, tokens, settings, or env changes: `docs/architecture/auth-and-data-model.md`
- Hub API, middleware, startup, heartbeat, or update-command changes: `docs/backend/hub-backend.md`
- Agent CLI, fingerprint, data-dir, handshake, health, or handler changes: `docs/agent/agent-runtime.md`
- Frontend routing, login, settings, stores, base-path, or i18n changes: `docs/frontend/frontend-app.md`
- Build, test, dev, or verification workflow changes: `docs/development/workflow-and-testing.md`
- Rename-sensitive or derived-project behavior changes: `docs/customization/renaming-and-derived-projects.md`
- Deployment, packaging, install, release, or operational changes: `docs/operations/deployment-and-packaging.md`
- Repo-specific rules, gotchas, or troubleshooting changes: `docs/conventions-and-gotchas.md` and/or `docs/troubleshooting/common-issues.md`
- Task routing or agent workflow changes: `docs/ai/agent-navigation.md` and `AGENTS.md`
- Major project-positioning or navigation changes: `docs/README.md` and/or `docs/project-overview.md`

Only if the scope is broad or cross-cutting should you review multiple docs from this list.

Common docs to consider for broader changes:

- `docs/README.md`
- `docs/project-overview.md`
- `docs/architecture/hub-agent-architecture.md`
- `docs/architecture/auth-and-data-model.md`
- `docs/backend/hub-backend.md`
- `docs/agent/agent-runtime.md`
- `docs/frontend/frontend-app.md`
- `docs/development/workflow-and-testing.md`
- `docs/customization/renaming-and-derived-projects.md`
- `docs/operations/deployment-and-packaging.md`
- `docs/conventions-and-gotchas.md`
- `docs/troubleshooting/common-issues.md`
- `docs/ai/agent-navigation.md`
- `AGENTS.md`

If no documentation changes are needed, make that a conscious scope-based decision, not an assumption.

### Translation Check Before Finishing

When a task adds or changes user-facing text, always check whether locale catalogs need updates.

- Update translations in the same task when new strings are introduced.
- Prioritize the main shipped languages, especially French (`fr`), for new or changed UI text.
- Do not assume the generated catalogs are complete just because the build succeeds.

---

## Adding a New API Endpoint

Register in `registerApiRoutes()` in `internal/hub/api.go`:

```go
// Authenticated endpoint
apiAuth.GET("/my-endpoint", h.myHandler)

// Authenticated + admin only
apiAuth.GET("/admin-only", h.myHandler).BindFunc(requireAdminRole)

// Unauthenticated endpoint
apiNoAuth.GET("/public", h.publicHandler)
```

Handler signature:
```go
func (h *Hub) myHandler(e *core.RequestEvent) error {
    return e.JSON(http.StatusOK, map[string]any{"ok": true})
}
```
