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

Vigil uses PocketBase as the application core rather than as a separate dependency hidden behind a service layer.

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
- `GET /api/app/hosts-overview` — lightweight per-host monitoring overview combining agent identity, latest snapshot, and latest host metrics; implemented in `internal/hub/host_metrics.go`. Each host also carries `metric_severity` (`{cpu,memory,disk}` → `"normal"`/`"warning"`/`"critical"`), the **instantaneous** bar severity of the current value against the resolved thresholds, computed by `metricAlertEvaluator.instantSeverity`. Each band (warning, critical) is resolved **independently** as the first positive of per-agent override → global → built-in default (80/90), so a disabled/zeroed "mute" override or a global row that sets only one band still falls through to a sensible default rather than leaving the bar uncolored. It deliberately **ignores** the alert's `enabled`/hysteresis/duration/mute state — the bar reflects the machine, not the alert — and the built-in defaults mean a vanilla install (no `metric_alerts` rows) still colors a hot CPU red. The disk bar is colored from the root-filesystem percent it displays (`disk_used_percent`), which can differ from the disk *alert* (evaluated on the busiest mount, `disk_max_used_percent`). The frontend hosts table maps it to the `MetricBar` tone (warning→amber, critical→red).
- `GET /api/app/hosts/:id` — dedicated host detail payload for one host; implemented in `internal/hub/host_metrics.go`
- `GET /api/app/hosts/:id/metrics` — historical host metrics for charts; implemented in `internal/hub/host_metrics.go`
- `GET /api/app/fleet-metrics?range=` — **all** fleet metrics (`cpu`/`memory`/`disk`/`load`) across **all** hosts in one response, keyed by metric (`map[metric][]FleetMetricSeries`, one time series per host), for the fleet Metrics page; implemented in `internal/hub/host_metrics.go` (`getFleetMetrics` → `loadFleetMetricsSeries`). Aggregates `host_metric_samples` **in SQL** — `GROUP BY agent` + a `strftime('%s', collected_at)`-derived time bucket, `AVG(...)` per metric per bucket — so a long range yields ~`fleetSeriesTargetPoints` (500) points per host instead of one row per raw sample (~10k over `7d`), keeping the page fast at scale. Known limitation: `load` is the raw 5-min load (not per-core like the host-detail chart).
- `GET /api/app/hosts/:id/container-metrics` — historical running-container metrics for host detail charts; implemented in `internal/hub/container_metrics.go`
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
- collects an initial host metrics sample with a short timeout and persists both the latest sample and the append-only history
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

## Host Metrics Pipeline

Host monitoring metrics are intentionally separate from inventory snapshots.

Files:

- `internal/hub/host_metrics.go` — periodic scheduler, persistence helpers, and dedicated metrics/host APIs
- `internal/hub/ws/handlers.go` — `GetHostMetrics()` WebSocket call
- `internal/migrations/21_create_host_metrics.go` — `host_metric_samples` and `host_metric_current` collections

Current behavior:

- `METRICS_INTERVAL` controls the polling cadence for lightweight host metrics
- default interval is `1m`
- minimum interval is `30s`
- each successful poll inserts one `host_metric_samples` record and upserts one `host_metric_current` record
- `host_metric_samples` retention is handled by the scheduled job `vigilHostMetricRetention`, which deletes samples older than 7 days by default

This split keeps high-frequency monitoring off the heavier `GetHostSnapshot` path and avoids reusing the dashboard aggregate for charts or row-level resource status.

## Container Metrics Pipeline

Running-container monitoring metrics are also kept separate from both inventory snapshots and host-level metrics.

Files:

- `internal/hub/container_metrics.go` — persistence helpers, retention, and the dedicated host container metrics API
- `internal/hub/ws/handlers.go` — `GetContainerMetrics()` WebSocket call
- `internal/migrations/22_create_container_metrics.go` — `container_metric_samples` collection

Current behavior:

- the same `METRICS_INTERVAL` cadence used for host metrics also polls running-container metrics
- each successful poll inserts one `container_metric_samples` record containing the full set of running-container samples for that host and timestamp
- retention is handled by the scheduled job `vigilContainerMetricRetention`, which deletes samples older than 7 days by default

This mirrors the Beszel-style split between inventory and monitoring while keeping the write path minimal for V1: one row per host poll instead of one row per container.

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
- `moveMonitor` — updates only the `group` field via `SaveNoValidate`; it does not restart the scheduler
- `deleteMonitor` — goroutine stopped via `OnRecordAfterDeleteSuccess` hook (the only remaining hook for monitors)

**Critical: do NOT add `OnRecordAfterCreateSuccess` or `OnRecordAfterUpdateSuccess` hooks for the monitors collection.** The scheduler calls `SaveNoValidate` to write check results, which triggers PocketBase update events. A hook that calls `startMonitor` from those events creates an infinite loop: save → hook → startMonitor → doCheck → save → hook → …

### Monitor Group Deletion

`deleteMonitorGroup` first moves any monitors in that group back to `group = ""` with `SaveNoValidate`, then deletes the group record. The frontend now warns before deleting a non-empty group so the ungrouping is explicit.

### Result Persistence

`saveResult(monitor, status, latencyMs, msg)`:

1. inserts a `monitor_events` record via `SaveNoValidate`
2. updates the monitor record fields (`status`, `failure_count`, `last_checked_at`, `last_latency_ms`, `last_msg`) via `SaveNoValidate`

Monitors also have a `failure_threshold` field. The default is `3`, `0` means instant down, and the scheduler flips the monitor to `down` after that many consecutive failures.

A failing check that has **not yet** reached the threshold (the monitor's effective status is still up) is recorded in `monitor_events.status` as **pending (`2`)**, Uptime-Kuma style, so the sparkline shows amber ("failing but not yet down") before the monitor flips down (red). Pending is only ever written to events — never to a monitor's own `status` (which stays `-1`/`0`/`1`) — and the notification path is unchanged (notifications still fire only on the effective up↔down transition). The monitor status constants are `monitorStatusUnknown=-1`, `monitorStatusDown=0`, `monitorStatusUp=1`, `monitorStatusPending=2`.

### Inverted Monitors

A monitor with `inverted = true` treats a *reachable* target as the alert condition (e.g. a maintenance page that should normally be unreachable). `doCheck` calls `invertMonitorResult(status, msg)` to flip `up`↔`down` **before** `saveResult`, so the failure-threshold and notification machinery operate on the effective status unchanged. An `unknown` status is left untouched, and the flip is **skipped for `push`** monitors (a missing heartbeat is a real outage, not a reachability signal — and the UI only offers the toggle for http/tcp/dns/ping). The raw check message is kept verbatim; the "inverted" context is surfaced in the UI (badge / Mode cell), not baked into the stored `last_msg`.

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

The rolling uptime and average latency values are computed over the available events in each window. Uptime is `up / (up + down)` — only `status IN (0,1)` events count toward the denominator, so **pending (`2`) and unknown (`-1`) events are excluded** (Uptime-Kuma model: retries under the failure threshold don't penalize uptime). They are exposed as soon as at least one up/down event exists in the window (`total > 0`), so the frontend shows real data immediately. `N/A` is only shown when there are no countable events at all for the window. `avg_latency_24h_ms` is never set for `push`-type monitors.

#### Monitors-list aggregate cache (`monitor_stats_cache.go`)

Computing those aggregates is expensive: `loadAllMonitorMetrics` runs a 30-day `GROUP BY` scan over the whole `monitor_events` table and `loadRecentChecks` does one indexed seek per monitor. The list endpoint (`GET /api/app/monitors`) is also hit frequently by the sidebar down-count and the home page, so recomputing on every request scanned the events table constantly.

The hub keeps an in-memory `monitorStatsCache` (on `*Hub`), refreshed in the background by a ticker (`MONITORS_STATS_INTERVAL`, default 30s, started in `StartHub` next to the snapshot/metrics tickers; it computes once eagerly at boot so the first request is warm). `buildMonitorsResponse` reads the cached aggregates + `recent_checks` instead of querying `monitor_events`, while still loading the small `monitors`/`monitor_groups` tables live — so **`status`/`last_checked_at`/`last_latency_ms`/`last_msg` stay instant** (the sidebar down-count remains accurate) and only the historical aggregates lag by up to one refresh interval. On a cold cache (before the first refresh) `buildMonitorsResponse` computes once synchronously so the response is never statless. A measured ~140× drop in per-request cost at 90k events (≈47ms → ≈0.3ms), widening with event volume.

**Gotcha:** the cached `*MonitorMetrics` is shared and read-only. The push-monitor latency nil-ing in `buildMonitorsResponse` copies the struct first (`cp := *metrics`) — mutating the cached pointer in place would corrupt the cache and race other readers. The monitor *detail* endpoint (`buildMonitorDetail`, one monitor) is unaffected and still computes directly.

The monitor detail page uses the same history data through:

- `GET /api/app/monitors/{id}` — single monitor payload with current status and rolling metrics
- `GET /api/app/monitors/{id}/events` — event history, with optional `since`, `until` (RFC3339), `range`, and `limit` query parameters for time-bounded charts. A `range` (`1h`/`3h`/`6h`/`24h`/`7d`) is resolved server-side (`monitorEventsWindowSince`) and **takes precedence over `since`**, making the server the single clock authority for the window — so the detail chart and the transitions list cover exactly the same period (the series endpoint derives `since` the same way) instead of drifting by client/server clock skew. With `transitions_only=true` it returns only the status-change events (up↔down), newest first — the Uptime-Kuma-style incident history. Change detection runs **in SQL** via a `LAG` window function (`loadMonitorTransitions`), so only transition rows (≤ `limit`, default 500) are materialized regardless of how many raw checks fall in the window; **pending (`2`) rows are filtered out *before* the `LAG`** so sub-threshold flapping doesn't create up→pending churn or push real outages out of the capped list. The detail page requests `limit=500` and notes truncation if the cap is reached.
- `GET /api/app/monitors/{id}/series?range=` — a **downsampled** latency/uptime series for the detail chart on long ranges (e.g. `7d`), where returning every raw check would be ~10k points. `loadMonitorSeries` aggregates **in SQL** (`GROUP BY` on a `strftime('%s')`-derived time bucket) into ~500 buckets — so only one row per bucket is materialized — each carrying the average latency over its up checks and a status following the worst check in the bucket (down if any was down → red band, else pending if any was pending → amber band, else up), so incidents stay visible on the downsampled chart. Returns `MonitorEventEntry`-shaped points so the frontend's existing `buildSeries` consumes it unchanged; the detail page uses raw events for ≤24h and this series for `7d`.

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

Docker inventory is still sourced from the latest `host_snapshots` record for each agent, but image freshness is now audited separately in the `container_image_audits` collection. `getDashboard()` merges the latest per-container audit result back into the flattened container list so the frontend stays on the same dashboard route.

### Periodic Snapshot Ticker

At startup, `StartHub()` launches `startSnapshotTicker` as a background goroutine. The interval is read from the `SNAPSHOT_INTERVAL` env var (default: `15m`, minimum: `1m`). The goroutine is cancelled when the hub terminates via `OnTerminate`.

**Why 15 minutes?** Snapshot collection runs `apt-get` or `dnf` subprocesses on each agent. These commands are CPU-intensive (they run the full package solver or parse DNF metadata) and may be network-dependent on RedHat systems. Running them every minute would generate measurable load on resource-constrained agents, especially with many packages. Package and repository state changes on the order of hours or days, not minutes.

Agent liveness (up/down) is tracked independently via WebSocket Ping every 30 seconds (`agentPingInterval`) and is not affected by this interval. When a future lightweight metrics collection action is added (CPU, RAM), it will use a separate, more frequent ticker — not this one.

The ticker coexists with the manual `POST /api/app/refresh-snapshots` endpoint — both call the same `collectAllSnapshots` function.

The `host_snapshots` collection is created by migration `2_create_host_snapshots.go`. It has a relation field to `agents`, a JSON `data` field for the snapshot blob, and a unique index on the agent relation so there is at most one snapshot record per agent.

## Notification Dispatcher

Notifications are sent by a `*notifications.Dispatcher` (`internal/hub/notifications/dispatcher.go`) held as a field on `Hub`. It is instantiated in `NewHub` and started as a goroutine in `StartHub`.

### Suppression chokepoint (`emitNotification`)

Every event flows through a single method, `h.emitNotification(evt)` (`internal/hub/notification_mute.go`), instead of calling the bell + dispatcher inline. It gates delivery on `h.isNotificationSuppressed(evt)` **before both** the in-app bell (`createSystemNotification`) and external channels (`notifier.Dispatch`), so a suppressed resource is silenced everywhere at once:

```go
func (h *Hub) emitNotification(evt notifications.Event) {
    if h.isNotificationSuppressed(evt) { return }
    _ = h.createSystemNotification(evt)   // bell
    h.notifier.Dispatch(evt)              // channels
}
```

`isNotificationSuppressed` currently checks per-resource **mutes** (`notification_mutes` collection): an active mute on `(resource_type, resource_id)` — `muted_until` empty (indefinite) or in the future. `container_image` events are handled specially (`containerImageMuted`): the mute is keyed by the stable container **name** (`<agentID>|<containerName>`, matching `container_audit_overrides`) rather than the ephemeral container id the event carries, so it survives the redeploy it was meant to silence — the name is read from the event `Details`. A mute on a host (`agent`) also covers that host's `container_image` events, so muting a noisy host silences its containers and metric alerts too. The lookup **fails open** (logs and delivers) on a DB error — suppression must never silently swallow an alert. Putting the check here — not in the dispatcher — is what makes a mute cut the bell *and* the channels together.

`isNotificationSuppressed` also ORs in **maintenance windows** (`underMaintenance` in `internal/hub/maintenance.go`): if any enabled `maintenance` record is active at `now` and its scope covers the event's resource, the event is suppressed. Window activeness is computed lazily (`isMaintenanceWindowActive`) — single windows use absolute `start_at`/`end_at`; recurring windows match a local time-of-day range in the window's `timezone` plus an optional weekday set and date bounds (handling midnight-crossing). Scope is global (`{}`) or `{monitor_ids,agent_ids}`, with `container_image` events covered by their parent agent. Like mutes, maintenance suppression fails open on a DB error. v1 suppresses **all** in-window events.

### Maintenance endpoints

- `GET/POST /api/app/maintenance-windows`, `PUT/DELETE /api/app/maintenance-windows/{id}` — admin CRUD (`requireAdminRole`), validated in `maintenance_api.go` (title required; single needs start<end; recurring needs valid HH:MM times that differ, a loadable timezone, weekdays in 0–6, and consistent date bounds).
- `GET /api/app/maintenance/active` — authenticated (all users); returns the active windows for the banner as `{id,title,description,severity,ends_at}` only. `ends_at` is the end instant of the current occurrence (`windowEndsAt`).

### Dispatch points

All four route through `h.emitNotification(evt)`:

- **Monitor state transition** (`internal/hub/monitors.go` `saveResult`): after writing the new status with `SaveNoValidate`, if `effectiveStatus != previousStatus && previousStatus != monitorStatusUnknown`, emits the event (bell + dispatch).
- **Agent status transition** (`internal/hub/agent_connect.go` `setAgentStatus`): reads the previous status before overwriting; if changed, writes a `system_notifications` entry and calls `h.notifier.Dispatch(...)` after `SaveNoValidate`.
- **Container image update discovery** (`internal/hub/image_audits.go` `upsertContainerImageAudit`): after each scheduled audit result is merged into `container_image_audits`, the hub computes a persisted notification signature from the newer compatible tags currently available for that container. It emits both the system notification and external dispatch from the same event payload only when that signature changes, so the same discovered version set is not re-notified on every run.
- **Host metric threshold breach/recovery** (`internal/hub/metric_alerts.go`, called directly from `persistHostMetrics`, before the `host_metric_current` upsert): on each metrics poll the evaluator compares CPU/RAM/disk/loadavg against the per-(agent, metric) thresholds in the `metric_alerts` collection (per-agent override → global default; a disabled override mutes the metric for that host). It emits `host.metric_exceeded` (severity `warning`/`critical`) on tier escalation and `host.metric_normal` on recovery, using an edge-trigger state with hysteresis so a value hovering at the threshold does not flap. The tier read-compute-write is atomic; the dead band is clamped so an alert can always recover; an exact-0 reading is ignored as a non-reading; the disk alert names the busiest mount. The fired tier is persisted in `host_metric_current.alert_tiers` (folded into the same write) and restored at boot, so a restart does not re-fire active alerts. Routed by `notification_rules` like any other event (filter by `agent_ids`, gate by `min_severity`). Admin CRUD at `/api/app/metric-alerts`.

**No `OnRecordAfterUpdate` hooks are used for notifications** — doing so on `monitors` creates an infinite save loop (see conventions doc). The metric-alert threshold *cache* does use `metric_alerts` hooks, which is safe because that collection is low-frequency and admin-edited.

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

### System notification center

The navbar bell and `/notifications` page use `system_notifications`, not `notification_logs`. This feed is written directly by the hub for monitor transitions, agent transitions, and container image audit results. It does not require any notification channel or rule to be configured. Container image system notifications reuse the same rendered event title/body as webhook/email delivery so the bell, history page, and external channels stay consistent.

Routes require an authenticated user but are not admin-only:

```
GET   /api/app/system-notifications?category=&severity=&event_kind=&status=&q=&page=&limit=
GET   /api/app/system-notifications/unread?limit=
POST  /api/app/system-notifications/read-all?category=
GET   /api/app/system-notifications/preferences
PATCH /api/app/system-notifications/preferences
```

Read state is per-user and per-category in `user_settings.settings.system_notifications_last_read_at_by_category`. Bell visibility is stored in `user_settings.settings.system_notifications_enabled_events` and legacy category visibility in `system_notifications_enabled_categories`. Re-enabling a disabled category sets its read cursor to the current time so old events do not flood the bell.

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

The currently registered jobs are:

- `vigilAutoRetention` — deletes old monitor and notification history according to retention settings
- `vigilContainerImageAudit` — audits Docker image tags used by the current Docker container inventory and persists the result in `container_image_audits`

The image-audit job is read-only: it does not ask agents to start, stop, or restart containers, and it does not mutate workloads on remote hosts.
It now runs twice per day at `03:00` and `15:00` UTC (`0 3,15 * * *`).

Registry coverage: any Docker Registry v2 endpoint reachable from the hub (Docker Hub, GHCR, Quay, GitLab, self-hosted Harbor, etc.).

Authentication is resolved through a multi-keychain in this order:

1. `registry_credentials` collection (managed in Settings → Registry credentials, admin only). Passwords are encrypted at rest with AES-256-GCM using a per-install key stored at `<datadir>/credentials.key` (mode 0600, generated on first hub start). The API never returns the cleartext password — it is replaced by `**REDACTED**` on every read; sending `**REDACTED**` (or omitting the field) on update preserves the stored secret. Uniqueness is enforced on the registry hostname.
2. `authn.DefaultKeychain`, which reads `$DOCKER_CONFIG/config.json` (or `~/.docker/config.json`) of the hub process. To pull from a private registry without the in-app store, run `docker login <registry>` on the hub host. If the hub runs in a container, mount the config file read-only or set `DOCKER_CONFIG` to a mounted directory.
3. Anonymous, when neither of the above has a match.

**Recommended for Docker Hub:** add a Docker Hub credential in Settings → Registry credentials (or `docker login` on the hub host). Anonymous Docker Hub access is rate-limited per IP; authenticating raises the limit substantially and reduces transient check failures on busy fleets.

### Reliability (per-host serialization, per-call timeout, transient-failure handling)

Registry calls are bounded and gentle to avoid the self-inflicted rate-limiting / `context deadline exceeded` that bursty parallel access caused (`internal/hub/image_audits_runner.go`):

- **Per-registry-host serialization (one mechanism)**: `runImageAuditPool` groups targets by registry host (`registryHost`, which collapses all Docker Hub repos to one host) and processes each host's group **sequentially** in its own goroutine, with `imageAuditParallelism` host-groups running concurrently. So one registry is never hit concurrently, and — crucially — a worker slot is never parked waiting on a busy host, so distinct registries (GHCR/lscr.io) genuinely run in parallel instead of queueing behind a Docker-Hub-heavy fleet. Consecutive containers on the same host are paced by `imageAuditPerHostDelay` (between containers, not between a container's internal calls). The serialization lives only here — the registry client carries no per-host lock.
- **Per-call timeout** (`imageAuditPerCallTimeout`, 30s) applies to each `ListTags`/`ResolvedDigest`/`HeadDigest` individually, so a slow paginated `ListTags` no longer starves the following calls. `imageAuditPerContainerTimeout` (90s) is just a safety net.
- **Retries are conservative**: `imageAuditMaxRetries` (2), backoff capped at `imageAuditMaxRetryDelay` (2s), and timeouts are **not** retried (a retry only burns the budget; the next cycle re-checks).
- **Error taxonomy distinguishes transient from definitive**: `classifyRegistryError` maps 5xx/429/network/timeout to transient kinds and **4xx-other (400/410/422/…) to `client_error`** — a definitive client error that is neither retried nor preserved (it surfaces immediately). Auth (401/403) and not-found (404) are likewise definitive.
- **Transient failures keep the last good result** (`decideAuditPersistence`): a timeout/network/registry error does not overwrite a healthy `up_to_date`/`update_available` status (note: `unknown` is **not** preserved — it has no data worth protecting). It increments `consecutive_failures` and records a soft `last_check_error`/`last_check_error_at`, escalating to `check_failed` only after `imageAuditFailureGraceCycles` (3) consecutive failures (or immediately if there was no prior good state, or for a definitive error). Success resets the counter. The frontend (`StaleCheckHint`, shared across the dashboard, images page and container detail) shows a discreet "last check errored" hint instead of a red badge while the prior result is preserved.

Tag selection rules are intentionally simple:

- `latest` uses `digest_latest` and compares the container's current local manifest digest (extracted from `RepoDigests`, the same value `docker pull` reports) with the remote manifest digest resolved for the same tag and platform — note that the local image ID (`docker image inspect .Id`) is the image config digest, which is never equal to a manifest digest and must not be used for this comparison
- one-part numeric tags such as `15` use `semver_major` and track the newest `15.x.x`
- two-part numeric tags such as `15.2` use `semver_minor` and track the newest `15.2.x`
- three-part numeric tags such as `15.2.3` also use `semver_minor` and track the newest `15.2.x`
- tags are parsed via Masterminds/semver (`internal/hub/image_tag_parser.go`) so prefixed forms (`v1.2.3`) and full semver with prereleases parse correctly; prereleases (`-rc1`, `-alpha.1`, `-beta-2`, etc.) are excluded from candidates unless the current tag is itself a prerelease
- numeric tags with a variant suffix (`15-alpine`, `1.25-bookworm`, `20.11.1-alpine3.19`, `8-jdk-slim`, `8.2-fpm-alpine`) follow the same major/minor rules but only match candidates that share the same whitelisted suffix (`knownVariantSuffixes`) — `1.2.3-alpine` is never proposed as an update of `1.2.3-bullseye`
- pure numeric tags without dots (e.g. `608111629`) are filtered out as build IDs when the current tag has dots, even though Masterminds technically accepts them as one-part versions

Per-container overrides: an admin can override the auto-deduced policy for any container via the dropdown menu in the dashboard's container table or by writing to `container_audit_overrides`. Supported overrides:

- `digest` — force `digest_latest`, only watches the digest of the current ref. Useful for rolling tags such as `:stable`, `:nightly`, or to opt out of semver candidate selection on a pinned tag.
- `patch` — force `semver_minor` (track only the same `major.minor` line).
- `minor` — force `semver_major` (track patches and minors within the same major).
- `disabled` — skip the audit entirely for that container; the record is upserted with `status: disabled` and no notification signature is set, so re-enabling later will trigger a fresh notification on the first detected update.

Each override row also carries optional `tag_include` and `tag_exclude` Go-flavored regexes (migration `20_add_tag_filters_to_container_audit_overrides.go`). When set, they narrow the candidate tag list returned by the registry (`applyTagRegexFilter`) before the semver comparison: priority is include first, then exclude. The container's current tag is always preserved so the audit baseline never becomes unreachable. Both regexes are validated server-side at write time. Use `tag_include="^v3\\."` to lock a `traefik` container to its v3 series even when v4 ships, or `tag_exclude="-rc\\d*$"` to drop release candidates if the upstream uses them outside semver prerelease syntax.

Overrides are loaded once per audit cycle and applied during `collectContainerImageAuditResults` before the registry call, so a `disabled` override never reaches the registry.

Audit cycles run with a host-grouped pool (`runImageAuditPool`, default `imageAuditParallelism = 4` concurrent registry **hosts**): targets are grouped by `registryHost`, each host's containers are processed **sequentially** in one goroutine (so a registry is never hit concurrently and no worker slot is parked behind a busy host), with `imageAuditParallelism` distinct hosts running in parallel. Consecutive containers on the same host are paced by `imageAuditPerHostDelay`. Each container has a generous `imageAuditPerContainerTimeout` (90s) safety net, and every individual registry call has its own `imageAuditPerCallTimeout` (30s) so a slow `ListTags` cannot starve the following `HeadDigest`. The registry client used during a cycle is a `cachingRegistryClient` that memoizes `ListTags` per repository (so 50 containers all using `nginx` issue a single `/v2/library/nginx/tags/list`) and waits for in-flight lookups instead of returning partial results; a per-caller context cancellation is **not** memoized (it would poison sibling containers). It applies up to `imageAuditMaxRetries` (2) attempts with capped exponential backoff on transient registry errors (network, 5xx, 408/425/429) — **not** on timeouts (retrying a timeout only burns the budget) and **not** on definitive errors (401/403, 404, other 4xx). Errors surfaced to the frontend keep the human message on `error` and add a stable kind on `details.error_kind` (`auth_failed`, `not_found`, `timeout`, `network`, `registry_error`, `client_error`, `unknown`) so the UI can suggest the right remediation.

Override changes also reflect immediately into the matching `container_image_audits` records (without waiting for the next cycle), via `applyOverrideToAuditRecords` invoked from the override API handlers. Setting `disabled` stamps `status=disabled` on the matching audit rows; reverting away from `disabled` resets `status=unknown` so the next cycle re-evaluates. Other policy switches leave the existing status alone — the cached result still reflects the *previous* policy until the next cycle (or "Check images now"). The frontend dashboard subscribes to the `container_image_audits` collection (debounced 1.5s) so these writes propagate without a manual refresh.

For semver-like tags, the backend now separates two concepts:

- the primary line status answers whether the container is current within its own line (`15` -> latest `15.x.x`, `15.2` or `15.2.3` -> latest `15.2.x`)
- the audit also records the latest tag in the same major and the latest overall tag so the UI can show that a newer major exists without marking the current patch line as outdated

Example: `ghcr.io/mealie-recipes/mealie:v2.2.5` can be `up_to_date` in its `v2.2.x` line while still surfacing `v3.15.2` as a newer major to plan for.

Notification dispatch for image updates intentionally follows a different rule than the raw audit `status` field. The hub notifies on newer compatible tags becoming available, even if the current tag is still up to date in its own line. For example, `1.2.5` may stay `up_to_date` in `1.2.x` while still notifying when `1.3.0` or `2.0.0` first appear.

The dispatched notification carries an event-specific severity (`imageAuditEventSeverity`): `warning` when a new major is detected or when the audit failed with `error_kind=auth_failed`, otherwise `info`. The `notifications.Event` struct now exposes a `Severity` override that takes precedence over `Kind.Severity()`; `dispatcher.go` and `system_notifications.go` both consume it through `Event.EffectiveSeverity()`.

The per-container notification state is stored directly on `container_image_audits`:

- `last_notified_signature` — normalized set of the higher versions already announced for this container
- `last_notified_at` — timestamp of the last emitted update-available notification for that signature

This makes image update notifications durable across hub restarts and avoids relying on the in-memory throttle cache for this use case.

For exact semver tags such as `1.2.5`, the audit also resolves the remote digest of that same tag. If the registry republishes `1.2.5` with a different image digest, Vigil marks it as a dedicated rebuilt-tag update instead of silently treating it as current.

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

The navbar notification bell uses `system_notifications` instead of `notification_logs`, so it is available to every authenticated user and remains independent from configured external notification channels.

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
