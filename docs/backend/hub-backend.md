# Hub Backend

## Role Of The Hub

The hub is the central application runtime. It wraps PocketBase and adds project-specific behavior for:

- custom API routes
- frontend serving and app-info injection
- agent enrollment and connection lifecycle
- auth and collection setup
- update and heartbeat features

The hub code is concentrated under `internal/hub/` with the CLI entrypoint in `internal/cmd/hub/hub.go`.

## Bootstrap Path

The startup flow begins in `internal/cmd/hub/hub.go`.

High-level flow:

1. create a base PocketBase app
2. register migration support
3. construct the project `Hub`
4. call `StartHub()`
5. start serving HTTP

This split keeps generic PocketBase setup separate from project-specific boot logic.

## The `Hub` Type

The main project type lives in `internal/hub/hub.go`.

The `Hub` type wraps the PocketBase app and owns project state such as:

- computed app URL
- SSH public key material used for hub identity verification
- helpers related to linking, keys, and shared config

If you are adding backend behavior that needs app-wide state or helper methods, this is usually the right place to add it.

## `StartHub()` Responsibilities

`StartHub()` is the main composition method. It wires together:

- collection auth settings
- frontend serving behavior
- custom API routes
- connection and lifecycle hooks

Read it before changing initialization behavior because it is the coordination point for the hub runtime.

## PocketBase Integration

Nexus uses PocketBase as the application core rather than as a separate dependency hidden behind a service layer.

That means backend work often combines:

- direct collection lookups
- PocketBase hooks and event handlers
- project-specific helper methods

Common access patterns include:

- `FindCollectionByNameOrId`
- `FindCachedCollectionByNameOrId`
- `FindFirstRecordByFilter`
- `CountRecords`

## Collection Auth Configuration

Collection-level auth configuration lives in `internal/hub/collections.go`.

This file controls project behavior such as:

- whether password auth is enabled
- whether self-service user creation is enabled
- whether OTP and MFA settings are applied

This is one of the first places to check when auth behavior seems inconsistent between the frontend and PocketBase.

## Custom API Routes

Custom routes are registered in `internal/hub/api.go`.

The project uses two major route groups:

- authenticated routes
- unauthenticated routes

Some authenticated routes are further restricted to admins.

Examples of route responsibilities in this file include:

- first-run checks
- app info responses
- trusted-auth and auto-login helpers
- enrollment-token management
- heartbeat testing
- update-check information
- `GET /api/app/dashboard` — aggregated dashboard payload with host, snapshot, and monitor KPI counters (auth required); implemented in `internal/hub/dashboard.go`
- `POST /api/app/refresh-snapshots` — triggers on-demand snapshot collection from all connected agents (auth required, non-readonly); implemented in `internal/hub/snapshots.go`

## Middleware And Role Enforcement

Route registration combines PocketBase route groups with project-specific bind functions.

Patterns to follow:

- use authenticated route groups for logged-in flows
- add explicit admin-only bind functions for admin routes
- keep auth and role checks close to route registration so intent stays obvious

## How To Add A New API Endpoint

Typical workflow:

1. add the route in `registerApiRoutes()` in `internal/hub/api.go`
2. choose the right route group
3. bind any required admin middleware
4. implement the handler on `*Hub`
5. update related frontend or tests if the endpoint is consumed there

Examples:

- authenticated endpoint
- authenticated admin-only endpoint
- unauthenticated endpoint

Prefer small handlers that keep data access and response shaping obvious.

## Agent Connection Flow Inside The Hub

The agent connection lifecycle is implemented in `internal/hub/agent_connect.go`.

Important responsibilities there:

- validate connection headers
- upgrade to WebSocket
- verify the hub challenge flow
- find or create agent records
- request initial agent info
- manage liveness and offline transitions

Most protocol-sensitive backend work ends up touching this file or the `internal/hub/ws/` package.

On a successful connection, `agent_connect.go` also:

- stores the live `*ws.WsConn` in `Hub.agentConns` (a `sync.Map` keyed by agent ID) so refresh endpoints can reach connected agents
- removes the entry from `agentConns` when the connection closes
- collects an initial host snapshot with a 60-second timeout and upserts it into the `host_snapshots` collection
- restores an existing agent from `offline` back to `connected` on reconnect so recovery notifications can be emitted

## Hub WebSocket Layer

The hub-side WebSocket runtime lives under `internal/hub/ws/`.

Important files:

- `handlers.go`
- `ws.go`
- `request_manager.go`

Responsibilities:

- encoding and decoding protocol requests
- tracking in-flight request IDs
- matching responses to requests
- exposing convenience methods like `GetAgentInfo()` and `Ping()`

When adding new agent-facing behavior, this package is the hub-side implementation surface.

## Frontend Serving

Frontend serving behavior is split by build mode.

### Production

Production-serving logic lives in:

- `internal/hub/server.go`
- `internal/hub/server_production.go`
- `internal/site/embed.go`

The frontend bundle is embedded into the Go binary and served from the hub.

### Development

Development-serving logic lives in:

- `internal/hub/server.go`
- `internal/hub/server_development.go`

In development mode, the hub proxies requests to the Vite dev server and still injects app metadata into the HTML shell.

## App Info Injection

The hub injects project information into the frontend via the global `APP` object.

This includes values such as:

- display name
- version
- app URL or hub URL
- base path and related frontend boot information

If frontend behavior depends on deployment context, inspect the app-info injection path before adding new client-only assumptions.

## Internal Record Updates

For internal agent status and similar internal bookkeeping, the codebase prefers `SaveNoValidate` instead of `Save`.

Why:

- internal status changes should not trigger full validation hooks unnecessarily
- it avoids coupling operational lifecycle updates to user-facing validation logic

If you are updating agent records during connection lifecycle code, follow the existing pattern.

## Heartbeat Feature

Heartbeat support lives in `internal/hub/heartbeat/heartbeat.go`.

This feature:

- reads configuration from env
- sends periodic outbound requests to an external monitoring endpoint
- exposes a send path that can also be used by the heartbeat test route

Important env values:

- `HEARTBEAT_URL`
- `HEARTBEAT_INTERVAL`
- `HEARTBEAT_METHOD`

## Update Feature

Self-update behavior for the hub command lives in:

- `internal/hub/update.go`
- `internal/ghupdate/*`

This feature:

- fetches the latest release metadata
- downloads the matching archive
- replaces the current executable
- attempts a service restart when possible

This is operational behavior, but backend maintainers may still need to understand it when packaging or deployment changes are involved.

## Monitor Scheduler

The monitor scheduler is the hub-side engine that drives uptime monitoring checks. It has no agent dependency — all checks are performed directly from the hub process.

### Files

- `internal/hub/monitors.go` — scheduler, check logic, result persistence
- `internal/hub/monitors_api.go` — REST API handlers and response types

### Monitor Types

| Type | Check mechanism |
|------|----------------|
| `http` | HTTP/HTTPS request with configurable method, accepted status codes, optional keyword match |
| `ping` | ICMP echo via the hub host's `ping` executable; measures round-trip latency from the hub itself |
| `tcp` | TCP dial — success means the port is reachable |
| `dns` | DNS lookup via Go resolver (supports A, AAAA, CNAME, MX, NS, TXT; optional custom DNS server) |
| `push` | Passive — hub checks `last_push_at` against `interval + 30s` grace period |

### Scheduler Lifecycle

`MonitorScheduler` manages per-monitor goroutines via a `sync.Map` of cancel functions.

```
hub.StartHub()
  └─ go h.monitorScheduler.start(ctx)
       └─ loads all active monitors from DB
            └─ for each: go startMonitor(id)

startMonitor(id):
  - cancels any existing goroutine for this id
  - creates a child context with a new cancel func
  - stores cancel func in sync.Map
  - launches runMonitor goroutine

runMonitor(ctx, id):
  - calls doCheck() immediately
  - loops: re-reads interval from DB (min 30s), waits, checks, repeats
  - exits cleanly when ctx is cancelled

stopMonitor(id):
  - LoadAndDelete from sync.Map
  - calls cancel func → goroutine exits
```

The scheduler also applies a startup grace period after hub boot so monitors that are still in the initial `unknown` state do not immediately flip to `down` because of transient restart noise.

### API Lifecycle Control

Goroutine start/stop is **only triggered from the API handlers**, not from PocketBase hooks:

- `createMonitor` — calls `go h.monitorScheduler.startMonitor(rec.Id)` if active
- `updateMonitor` — calls `stopMonitor` then `startMonitor` if active
- `deleteMonitor` — goroutine stopped via `OnRecordAfterDeleteSuccess` hook (the only remaining hook for monitors)

**Critical: do NOT add `OnRecordAfterCreateSuccess` or `OnRecordAfterUpdateSuccess` hooks for the monitors collection.** The scheduler calls `SaveNoValidate` to write check results, which triggers PocketBase update events. A hook that calls `startMonitor` from those events creates an infinite loop: save → hook → startMonitor → doCheck → save → hook → …

### Result Persistence

`saveResult(monitor, status, latencyMs, msg)`:

1. inserts a `monitor_events` record via `SaveNoValidate`
2. updates the monitor record fields (`status`, `failure_count`, `last_checked_at`, `last_latency_ms`, `last_msg`) via `SaveNoValidate`

Monitors also have a `failure_threshold` field. The default is `3`, `0` means instant down, and the scheduler flips the monitor to `down` after that many consecutive failures.

The startup grace period only softens the initial `unknown` path after hub boot. Monitors that already had a known status such as `up` still honor their configured threshold immediately, and low thresholds (`0` and `1`) still apply immediately.

Both use `SaveNoValidate` — see the critical note above.

### Ping Monitor Runtime Notes

The `ping` monitor type is intentionally minimal in this repository:

- it reuses the existing `hostname`, `timeout`, `interval`, and `failure_threshold` fields
- phase 1 adds `ping_count`, `ping_per_request_timeout`, and `ping_ip_family`
- the hub executes the system `ping` binary and passes those values through to the command line
- latency is parsed from the command output when available and otherwise falls back to the wall-clock runtime of the probe

The backend maps the advanced options to common `ping` flags:

- `ping_count` -> `-c`
- `ping_per_request_timeout` -> `-W`
- `ping_ip_family` -> `-4` / `-6`

Operational implication:

- the hub environment must have a working `ping` executable available on `PATH`
- if the binary is missing or the runtime does not permit ICMP echo, the monitor goes `down` with an explicit `last_msg`

Those historical `monitor_events` records are also used to derive rolling stats for the monitors page:

- `avg_latency_24h_ms`
- `uptime_24h`
- `uptime_30d`
- `recent_checks` (last 10 statuses for the timeline bars in the table)

The rolling uptime and average latency values stay unavailable until the monitor has enough history to cover the full window, so the frontend can show `N/A` instead of a misleading partial value.

The monitor detail page uses the same history data through:

- `GET /api/app/monitors/{id}` — single monitor payload with current status and rolling metrics
- `GET /api/app/monitors/{id}/events` — event history, with optional `since`, `until` (RFC3339), and `limit` query parameters for time-bounded charts

### Push Heartbeat Endpoint

Unauthenticated endpoint used by external services to signal liveness:

```
GET  /api/app/push/:pushToken
POST /api/app/push/:pushToken
```

The hub looks up the monitor by push_token (where `type='push'` and `active=true`), sets `last_push_at = time.Now()`, and saves via `SaveNoValidate`. The push check goroutine then reads `last_push_at` and computes elapsed time vs `interval + 30s`.

### How To Use A Push Monitor

1. Create a monitor with type `push` in the Monitors page.
2. Copy the generated `push_url` shown in the table or in the monitor detail page.
3. Call that URL from the system or job you want to monitor, usually from a cron job, a systemd timer, or a shell script.

Example:

```bash
curl -fsS -X POST "https://hub.example.com/api/app/push/your-token-here"
```

`GET` works too, but `POST` is the default recommendation.

The monitor is considered up as long as the last heartbeat is newer than `interval + 30s`.

### How To Test It

1. Create or open a push monitor.
2. Send a manual heartbeat with `curl`.
3. Refresh the UI and verify the monitor switches to `Up` with a recent `Last check` and `Last message`.
4. Stop sending heartbeats and wait longer than `interval + 30s`; the monitor should flip to `Down`.

Useful quick test:

```bash
curl -i -X POST "https://hub.example.com/api/app/push/your-token-here"
```

The response is always a generic `{"msg":"ok"}` so the endpoint does not reveal whether the token exists.

### Monitor API Routes

Authenticated routes (non-readonly required for write operations):

```
GET    /api/app/monitors              — all groups with their monitors
POST   /api/app/monitors              — create monitor
PUT    /api/app/monitors/:id          — update monitor
DELETE /api/app/monitors/:id          — delete monitor
GET    /api/app/monitors/:id/events   — last 50 check events
GET    /api/app/monitor-groups        — list groups
POST   /api/app/monitor-groups        — create group
PUT    /api/app/monitor-groups/:id    — update group
DELETE /api/app/monitor-groups/:id    — delete group (ungroups monitors first)
```

Unauthenticated routes:

```
GET  /api/app/push/:pushToken         — push monitor heartbeat
POST /api/app/push/:pushToken         — push monitor heartbeat
```

## Snapshot And Dashboard Layer

Two hub files support snapshot collection and dashboard aggregation:

- `internal/hub/snapshots.go` — core snapshot logic:
  - `upsertHostSnapshot()` writes a snapshot to the `host_snapshots` PocketBase collection
  - `collectAllSnapshots(ctx)` iterates over `Hub.agentConns` and collects snapshots from all connected agents; returns `(refreshed, failed)` counts
  - `refreshSnapshots()` HTTP handler for `POST /api/app/refresh-snapshots` — calls `collectAllSnapshots` with the request context
  - `startSnapshotTicker(ctx, interval)` background goroutine started at hub boot; calls `collectAllSnapshots` on each tick
- `internal/hub/dashboard.go` — `getDashboard()` handler for `GET /api/app/dashboard`; reads from the `host_snapshots` and `agents` collections and returns an aggregated JSON payload to the frontend

The dashboard patch-status donut uses a strict priority order: `reboot_required`, `security_updates`, `stale_updates` (>30 days since last upgrade), `compliant`, then `unknown` when update data exists but the last upgrade time is not known.

### Periodic Snapshot Ticker

At startup, `StartHub()` launches `startSnapshotTicker` as a background goroutine. The interval is read from the `SNAPSHOT_INTERVAL` env var (default: `15m`, minimum: `1m`). The goroutine is cancelled when the hub terminates via `OnTerminate`.

**Why 15 minutes?** Snapshot collection runs `apt-get` or `dnf` subprocesses on each agent. These commands are CPU-intensive (they run the full package solver or parse DNF metadata) and may be network-dependent on RedHat systems. Running them every minute would generate measurable load on resource-constrained agents, especially with many packages. Package and repository state changes on the order of hours or days, not minutes.

Agent liveness (up/down) is tracked independently via WebSocket Ping every 30 seconds (`agentPingInterval`) and is not affected by this interval. When a future lightweight metrics collection action is added (CPU, RAM), it will use a separate, more frequent ticker — not this one.

The ticker coexists with the manual `POST /api/app/refresh-snapshots` endpoint — both call the same `collectAllSnapshots` function.

The `host_snapshots` collection is created by migration `2_create_host_snapshots.go`. It has a relation field to `agents`, a JSON `data` field for the snapshot blob, and a unique index on the agent relation so there is at most one snapshot record per agent.

## Notification Dispatcher

Notifications are sent by a `*notifications.Dispatcher` (`internal/hub/notifications/dispatcher.go`) held as a field on `Hub`. It is instantiated in `NewHub` and started as a goroutine in `StartHub`.

### Dispatch points

- **Monitor state transition** (`internal/hub/monitors.go` `saveResult`): after writing the new status with `SaveNoValidate`, if `effectiveStatus != previousStatus && previousStatus != monitorStatusUnknown`, calls `h.notifier.Dispatch(...)`.
- **Agent status transition** (`internal/hub/agent_connect.go` `setAgentStatus`): reads the previous status before overwriting; if changed, calls `h.notifier.Dispatch(...)` after `SaveNoValidate`.

**No `OnRecordAfterUpdate` hooks are used for notifications** — doing so on `monitors` creates an infinite save loop (see conventions doc).

### Internal architecture

```
Dispatcher.Dispatch(Event)        → non-blocking channel send (drops if buffer full)
worker goroutine × 2              → Dispatcher.process(ctx, evt)
process                           → load enabled rules from DB, match, route
sendToChannel                     → load channel record, select provider, retry × 3 (1s, 4s, 16s)
saveLog (SaveNoValidate)          → notification_logs record
```

Rules still carry a `min_severity` field in storage, but the current frontend no longer exposes it because it was redundant with explicit event selection for the current event set. Rules saved from the UI are normalized to `info`.

`saveLog` stores the full `payload_preview` string so the admin history UI can inspect the delivery preview without backend truncation.
It also stores `created_by` and `channel_kind` on each log entry so the frontend can subscribe only to the current user's relevant deliveries and show virtual `in-app` notifications as local toasts.

### Providers

| Kind | File | Config keys |
|---|---|---|
| `email` | `providers/email.go` | `to`, `cc`, `bcc` |
| `webhook` | `providers/webhook.go` | `url`, `method`, `headers` |
| `slack` | `providers/slack.go` | `url`, `channel`, `username` |
| `teams` | `providers/teams.go` | `url` |
| `gchat` | `providers/gchat.go` | `url` |
| `ntfy` | `providers/ntfy.go` | `url`, `token`, `priority` |
| `gotify` | `providers/gotify.go` | `url`, `token`, `priority` |
| `in-app` | `providers/in_app.go` | none |

Providers are registered in `Dispatcher.New()` via `providers.Register()`. The `in-app` provider is virtual: it doesn't call an external service, it only writes a successful `notification_logs` entry that the frontend can render as a toast.

### API routes (admin only)

All routes require the `requireAdminRole` middleware.

```
GET    /api/app/notifications/channels
POST   /api/app/notifications/channels
PATCH  /api/app/notifications/channels/{id}
DELETE /api/app/notifications/channels/{id}
POST   /api/app/notifications/channels/{id}/test

GET    /api/app/notifications/rules
POST   /api/app/notifications/rules
PATCH  /api/app/notifications/rules/{id}
DELETE /api/app/notifications/rules/{id}

GET    /api/app/notifications/logs?rule_id=&resource_id=&status=&event_kind=&since=&until=&page=&limit=
```

## Data Retention And Manual Purge

The hub is also responsible for lifecycle cleanup of the two append-only high-growth collections:

- `monitor_events`
- `notification_logs`

### Stored settings

Global purge settings live in the `data_retention_settings` collection as a single `key=global` record.

Fields:

- `monitor_events_retention_days`
- `notification_logs_retention_days`
- `monitor_events_manual_default_days`
- `notification_logs_manual_default_days`
- `offline_agents_manual_default_days`

### Automatic retention

`StartHub()` registers application cron jobs through a shared scheduled-jobs registry backed by PocketBase cron.

Current automatic behavior:

- delete `monitor_events` older than the configured retention window
- delete `notification_logs` older than the configured retention window

Current non-behavior:

- no automatic deletion of agents
- no automatic age-based deletion of `host_snapshots`

This is intentional because `host_snapshots` is already latest-only. Deleting hosts would remove current state, not old historical rows.

### Scheduled jobs

Global scheduled job state is stored in the `scheduled_jobs` collection. Each job stores:

- `key`
- `schedule`
- `last_run_at`
- `last_success_at`
- `last_status`
- `last_error`
- `last_result`
- `last_duration_ms`

The current retention cleanup is one registered job (`vigilAutoRetention`) in this shared registry.

Admin job routes:

```
GET  /api/app/jobs
POST /api/app/jobs/{key}/run
```

`POST /api/app/jobs/{key}/run` executes the job immediately and returns the updated persisted job state.

### Manual purge API routes (admin only)

```
GET   /api/app/purge/settings
PATCH /api/app/purge/settings
POST  /api/app/purge/run
```

`POST /api/app/purge/run` supports these scopes:

- `monitor_events`
- `notification_logs`
- `offline_agents`

And these modes:

- `older_than_days`
- `all`

For `offline_agents`, both modes only target agents where `status = 'offline'`. The destructive `all` mode never deletes connected agents.

The logs endpoint returns a paginated object with `{items, page, limit, has_more}` sorted by `-sent_at`.

Secrets in `notification_channels.config` are redacted to `"**REDACTED**"` in all read responses. When a client PATCHes a channel and sends back `"**REDACTED**"` for a sensitive field, the existing stored value is preserved.

### Frontend realtime toast flow

The authenticated frontend subscribes to `notification_logs` in realtime with a filter shaped like:

`created_by = "<current_user_id>" && (status = "failed" || channel_kind = "in-app" || (status = "sent" && (event_kind = "monitor.down" || event_kind = "monitor.up" || event_kind = "agent.offline" || event_kind = "agent.online")))`

This means:

- failed deliveries for the current user's rules surface as destructive toasts
- successful deliveries on the virtual `in-app` channel surface as normal in-app toasts
- successful `monitor.down` and `agent.offline` deliveries also surface as immediate red UI toasts, even when the actual notification is sent through an external provider such as webhook or email
- successful `monitor.up` and `agent.online` deliveries surface as green recovery toasts in the UI

## High-Signal Files For Backend Work

- `internal/cmd/hub/hub.go`
- `internal/hub/hub.go`
- `internal/hub/api.go`
- `internal/hub/collections.go`
- `internal/hub/agent_connect.go`
- `internal/hub/snapshots.go`
- `internal/hub/dashboard.go`
- `internal/hub/monitors.go`
- `internal/hub/monitors_api.go`
- `internal/hub/server.go`
- `internal/hub/server_development.go`
- `internal/hub/server_production.go`
- `internal/hub/ws/handlers.go`
- `internal/hub/ws/ws.go`
- `internal/hub/ws/request_manager.go`

## Safe Change Checklist

Before finishing a hub backend change, check whether it affects:

1. migrations or collection shape
2. auth and route protection
3. frontend app-info injection
4. agent lifecycle code
5. build-mode-specific serving behavior
6. tests that require the `testing` build tag
