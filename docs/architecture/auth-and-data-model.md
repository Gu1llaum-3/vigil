# Auth And Data Model

## Why This Document Exists

In Vigil, authentication, user setup, agent enrollment, and persisted settings are spread across:

- PocketBase collections and migrations
- custom hub routes
- frontend auth flows
- agent connection verification

This document ties those pieces together so contributors can change auth and data behavior without missing a dependent subsystem.

## Collections Overview

The primary collections are defined in the snapshot migration under `internal/migrations/0_collections_snapshot_*.go`.

### `users`

- PocketBase auth collection
- stores application users
- includes a `role` field used by the project auth model

Expected roles:

- `user`
- `admin`
- `readonly`

### `user_settings`

- one record per user
- stores a JSON `settings` payload
- used by the frontend to persist app-level preferences

Current settings include values such as:

- preferred language
- layout width
- hour format
- system notification preferences (`system_notifications_enabled_events`, legacy `system_notifications_enabled_categories`) and per-category read cursors (`system_notifications_last_read_at_by_category`)

### `agents`

- one record per connected or enrollable agent
- stores token, fingerprint, status, capabilities, and metadata
- is updated by the hub after handshake and lifecycle events

Important fields include:

- `token`
- `fingerprint`
- `status`
- `capabilities`
- `metadata`
- `tags` — JSON array of free-text strings (migration `32_add_agent_tags.go`) for grouping/searching/filtering hosts. No managed catalog; arbitrary per-host strings. A normal writable field: non-readonly users edit it directly via the collection API (the agents `updateRule` already permits them), and it is included in the hosts-overview/detail payloads (`HostOverviewRecord.tags`). Agents may declare initial tags via the `TAGS` env var (reported in `GetAgentInfo`); the hub applies them **only when it first creates the agent record** (`setInitialAgentTags` in `agent_connect.go`), so reconnects and the UI remain authoritative.

**Tenancy / authorization (by design):** Vigil is single-tenant — every authenticated user
sees the whole fleet (`listRule`/`viewRule` only require authentication), and any non-readonly
user (`user` **or** `admin`) can update or delete any agent record. There is intentionally **no
per-owner scoping** on the `agents` collection: `created_by` records the enrollment-token owner,
which is not a reliable "owner" signal because enrollment tokens are deliberately shareable
(multiple agents per token). Treat the `user` role as a **fleet operator**, not a tenant.

The one sensitive field, `token`, is **not** writable or readable through the generic collection
API: migration `27_hide_agent_token.go` marks it `hidden`, and PocketBase strips hidden fields
from both input and output for non-superusers. Tokens are therefore only obtained via
`GET /api/app/agent-tokens` and rotated via `POST /api/app/agents/{id}/rotate-token` (both gated
to non-readonly users). If you need stricter isolation (e.g. agents managed only by admins),
tighten the collection `updateRule`/`deleteRule` to `@request.auth.role = "admin"` — this is a
deliberate hardening choice, not the current default.

### `agent_enrollment_tokens`

- stores user-owned enrollment tokens
- can be used to self-register new agents
- supports temporary and persistent enrollment patterns

The collection is tied to the user that created the enrollment token.

### `user_api_keys`

- created by migration `31_create_user_api_keys.go`
- long-lived bearer tokens (`vk_…`) for non-browser clients (scripts, the MCP server) to call `/api/app/*` as the owning user
- only the **SHA-256 hash** of the token is stored (`token_hash`, hidden); the plaintext is returned **once** at creation
- fields: `name`, `created_by` (relation→users), `prefix` (display head), `scope` (`read` / `read-write`), `last_used_at`, `expires_at`
- the `authenticateApiKey` middleware (`internal/hub/api_keys.go`) only authenticates the **Vigil app API** (`/api/app/*` and `/api/mcp`) — a key is never honored on the generic PocketBase API (`/api/collections`, `/api/realtime`, admin), so it can't act as a full session on raw collections. It resolves the bearer token (scheme stripped case-insensitively) to the user, enforces scope (a `read` key may only use safe HTTP methods, and the GET routes that return credentials — `/agent-tokens`, `/agent-enrollment-token` — also bind `rejectReadOnlyApiKey`, so a read key reads data, never secrets that enroll or impersonate agents; push tokens are still visible, see ticket T83; on `/api/mcp` scope is enforced per-tool — read keys are served only read-only tools, read-write keys also get write tools), and hands `RequireAuth` a freshly minted JWT
- `expires_at` is validated on creation (RFC3339, must be in the future) so a typo can't silently produce a non-expiring key; key management (`/api/app/api-keys`) is refused when the request itself authenticated via a key, so a key cannot mint more keys

### `notification_mutes`

- created by migration `33_create_notification_mutes.go`
- per-resource notification silence list; suppresses **both** the in-app bell and external channels for one resource
- fields: `resource_type` (`monitor` / `agent` / `container_image`), `resource_id` (**plain text, no FK relation**), `muted_until` (date; empty = indefinite), `created_by`, `note`
- `resource_id` is plain text rather than a relation so the same table covers containers, which have no first-class record. For `monitor`/`agent` it is the record id; for `container_image` it is `<agentID>|<containerName>` — the stable container **name** (matching `container_audit_overrides`), *not* the ephemeral container id the audit event carries. That way a mute survives the container being recreated on the redeploy it was meant to silence; the hub resolves the event's container id to its name (via the event `Details`) when checking suppression
- the absence of an FK also avoids the migration lexical-ordering trap (a relation to `monitors`/`agents` in a `33_` file would run before `3_create_monitors.go`); an orphan mute row left after the resource is deleted is harmless
- unique index on `(resource_type, resource_id)` makes muting an upsert (the client recovers from a lost create race by updating the winner's row)
- enforced at the `h.emitNotification` chokepoint via `isNotificationSuppressed` (`internal/hub/notification_mute.go`); a mute on an `agent` also covers that host's `container_image` events. Suppression **fails open** (logs and delivers) on a DB lookup error — an alerting path must not silently drop notifications

### `maintenance`

- created by migration `34_create_maintenance.go`
- planned maintenance windows that suppress notifications for covered resources while active and drive the global "maintenance in progress" banner
- fields: `title`, `description`, `enabled`, `severity` (`info`/`warning`/`critical`), `strategy` (`single`/`recurring`), `start_at`/`end_at` (single), `start_time`/`end_time`/`weekdays`/`active_from`/`active_to`/`timezone` (recurring), `scope` (JSON), `created_by`
- `single` windows use absolute start/end instants; `recurring` windows match a local time-of-day range in `timezone` (via `time.LoadLocation`), an optional `weekdays` set (JSON `[0..6]`, 0=Sunday; empty = every day), and optional `active_from`/`active_to` calendar-date bounds. The recurring computation handles windows that cross midnight (the early-morning portion belongs to the previous day's start weekday)
- `scope` mirrors `notification_rules.filter`: `{}` = global, `{monitor_ids:[…],agent_ids:[…]}` = targeted. A `container_image` event is covered by its parent agent id. No FK (plain JSON ids) — stale ids after deletion are harmless
- writes are admin-only: create/update/delete rules are `null`; CRUD is gated by `requireAdminRole` on `/api/app/maintenance-windows` (handlers use `app.Save`, which bypasses collection rules). **Read** (list/view) is open to any authenticated user so the banner can subscribe to the collection over realtime and react instantly to another admin's create/edit/delete. The banner still renders from the separate authenticated `GET /api/app/maintenance/active` (returns only `{id,title,description,severity,ends_at}`) and uses the collection subscription purely as a change signal — so opening read does not change what the banner displays, only that authenticated users can now query/subscribe the collection
- active-window evaluation is lazy (`activeMaintenances(now)`, no cron); the frontend banner polls. Suppression is enforced at the `h.emitNotification` chokepoint (`underMaintenance`) alongside mutes and, like mutes, **fails open** on a DB error. v1 suppresses **all** in-window events (the "MAINTENANCE→DOWN still alerts" nuance is a future refinement)
- rules: list/view = authenticated, create/update/delete = non-readonly — the frontend manages mutes via `pb.collection("notification_mutes")` directly (no custom API)

### `host_metric_samples`

- created by migration `21_create_host_metrics.go`
- append-only host monitoring history
- one record per collected host metrics sample
- fields: `agent` (relation→agents, cascadeDelete=true), `cpu_percent`, `memory_total_bytes`, `memory_used_bytes`, `memory_used_percent`, `disk_total_bytes`, `disk_used_bytes`, `disk_used_percent`, `network_rx_bps`, `network_tx_bps`, `collected_at`
- indexed on `(agent, collected_at)` for per-host chart queries
- written only by the hub after polling connected agents over WebSocket

### `host_metric_current`

- created by migration `21_create_host_metrics.go`
- latest-only per-host metrics cache for list/detail views
- same metrics fields as `host_metric_samples`
- unique index on `agent`
- written only by the hub after each successful metrics poll
- migration `24_add_metric_alert_columns.go` adds `load1`/`load5`/`load15`/`disk_max_used_percent` to both `host_metric_samples` and `host_metric_current`
- migration `26_add_alert_tiers.go` adds an `alert_tiers` JSON column to `host_metric_current` only: it persists the metric-alert edge-trigger state (`metric → fired tier`) per agent so a hub restart does not re-fire already-active alerts (restored at boot by `loadState()`)
- migration `35_add_host_metric_disk_mounts.go` adds a `disk_mounts` JSON column to `host_metric_current` only: the latest per-mount `[]{mountpoint,used_percent}` breakdown from `HostMetricsResponse.DiskMounts`, used for the per-filesystem view and the hub-side per-host include/exclude + "worst monitored mount" selection. Only overwritten when the agent reports mounts (a legacy/empty poll never clobbers it); empty on agents older than the field
- migration `37_add_host_metric_disk_max_mount.go` adds a `disk_max_mount` text column to `host_metric_current` only: the name of the worst *monitored* mount (after the include/exclude rule), for the fleet disk bar tooltip. `disk_max_used_percent` on both samples and current is now the worst *monitored* mount's percent (folded in by `persistHostMetrics`), not the agent's blind busiest mount — they differ only when a `disk_monitor_rules` policy excludes mounts

### `disk_monitor_rules`

Per-host policy for which filesystems the disk bar + disk alert consider. One row per `agent` (relation to `agents`, unique index). Fields: `mode` (`all` default / `include` / `exclude`) and `mounts` (JSON list of mountpoints). Admin-only: rules are `null`, access gated by `requireAdminRole` on `/api/app/disk-monitor-rules` (list/upsert-by-agent/delete). Cached in memory (`diskRuleCache`, kept fresh via `disk_monitor_rules` hooks + a boot warm) so `monitoredWorstDisk` — the worst used% among the monitored mounts — costs no DB query on the per-poll hot path. Created by migration `36_create_disk_monitor_rules.go`.

### `metric_alerts`

- created by migration `25_create_metric_alerts.go`
- host metric-threshold alert definitions; one row per `(agent, metric)` (unique index)
- `agent` empty = **global default**; `agent` set = **per-agent override** (override always wins at evaluation, including when disabled — a disabled override **mutes** that metric for that host and does not fall back to the global; re-inherit by deleting the row)
- fields: `metric` (select `cpu`/`memory`/`disk`/`loadavg`), `enabled`, `warning_value`, `critical_value`, `hysteresis`, `duration_seconds` (sustained-"for" entry delay, migration `29`; `0` = immediate)
- `cpu`/`memory`/`disk` thresholds are percentages; **`loadavg` is load-per-core on the 5-minute load** (`Load5` ÷ CPU core count hub-side, so one global threshold is meaningful on any host size; `1.0` = fully utilized; the 5-min window avoids 1-min spikes). Migration `28_reset_loadavg_alerts.go` cleared old absolute loadavg rows.
- the API rejects `hysteresis ≥ threshold` (otherwise the dead band would extend below 0 and a fired alert could never recover)
- admin-only: collection rules are `null`; access gated by `requireAdminRole` on `/api/app/metric-alerts`
- evaluated by `internal/hub/metric_alerts.go` (edge-trigger + hysteresis; in-memory state mirrored to `host_metric_current.alert_tiers`); emits `host.metric_exceeded` / `host.metric_normal` events routed through `notification_rules`

### `monitor_groups`

- created by migration `3_create_monitors.go`
- groups monitors for display organization
- fields: `name`, `weight` (sort order)
- monitors reference a group via relation field; deleting a group ungroups its monitors (relation set to empty, not cascaded)

### `monitors`

- created by migration `3_create_monitors.go`
- one record per configured uptime monitor
- common fields: `name`, `type` (select: `http`/`ping`/`tcp`/`dns`/`push`), `group` (relation→monitor_groups), `active`, `interval` (seconds), `timeout` (seconds)
- type-specific fields: `url`, `http_method`, `http_accepted_codes` (json list of integers, read with `UnmarshalJSONField` — `Get` returns `types.JSONRaw`), `keyword`, `keyword_invert`, `hostname`, `port`, `dns_host`, `dns_type`, `dns_server`, `push_token`
- behavior field (migration `8_add_monitor_inverted.go`, prefixed `8_` for lexical migration ordering — see conventions-and-gotchas.md): `inverted` (bool — flip up↔down so a reachable target is the alert condition)
- status fields updated by the scheduler: `status` (-1=unknown, 0=down, 1=up), `last_checked_at`, `last_latency_ms`, `last_msg`, `last_push_at`
- write operations (`status`, `last_checked_at`, `last_latency_ms`, `last_msg`) use `SaveNoValidate` to avoid triggering scheduler hooks — see hub-backend.md

### `monitor_events`

- created by migration `3_create_monitors.go`
- append-only check history for each monitor
- fields: `monitor` (relation→monitors, cascadeDelete=true), `status`, `latency_ms`, `msg`, `checked_at`
- indexed on `(monitor, checked_at)` for efficient per-monitor history queries
- the API returns the last 50 events ordered by `-checked_at`

### `notification_channels`

- created by migration `5_create_notifications.go` and extended by `6_notification_in_app.go`
- one record per configured notification destination
- fields: `name` (text, required, unique), `kind` (select: `email`/`webhook`/`slack`/`teams`/`gchat`/`ntfy`/`gotify`/`in-app`), `enabled` (bool), `config` (json — provider-specific config, sensitive fields redacted in API responses), `created_by` (relation→users)
- list/view rules: authenticated users; create/update/delete: admin only
- the `in-app` kind is a virtual channel with no external config; it exists only to write delivery logs that the frontend can turn into local toast notifications
- sensitive config keys are redacted to `"**REDACTED**"` in all API responses; sending `"**REDACTED**"` back in PATCH preserves the stored value

### `notification_rules`

- created by migration `5_create_notifications.go`
- one record per notification routing rule
- fields: `name`, `enabled` (bool), `events` (json array of event kinds), `filter` (json — optional resource filter), `channels` (multi-relation→notification_channels, corrected by migration `7_notification_rule_channels_multi.go` so more than one channel can be stored reliably), `min_severity` (select: `info`/`warning`/`critical`, currently kept for backend compatibility but no longer exposed in the UI), `throttle_seconds` (number, default 0 = no throttle), `created_by` (relation→users)
- list/view rules: authenticated users; create/update/delete: admin only
- rules are matched by the dispatcher against each event's kind and severity; with the current event model the UI normalizes `min_severity` to `info` and relies on explicit event selection instead; `throttle_seconds` suppresses repeat notifications for the same rule+resource+event kind

### `notification_logs`

- created by migration `5_create_notifications.go` and extended by `6_notification_in_app.go`
- append-only delivery log written by the dispatcher via `SaveNoValidate`
- fields: `rule` (relation→notification_rules, cascadeDelete=true), `channel` (relation→notification_channels, cascadeDelete=true), `created_by` (relation→users), `channel_kind` (text), `event_kind` (text), `resource_id`, `resource_name`, `resource_type`, `status` (select: `sent`/`failed`/`throttled`), `error` (text), `payload_preview` (text), `sent_at`
- indexed on `(rule, sent_at)`, `(resource_id, sent_at)`, and `(created_by, sent_at)`
- the extra `created_by` and `channel_kind` fields exist so the frontend can subscribe in realtime only to the current user's relevant notification logs and distinguish virtual `in-app` deliveries from external providers
- list/view rules: admin only; create/update/delete forbidden from the API (written only by backend)

### `system_notifications`

- created by migration `19_create_system_notifications.go`
- append-only internal event feed for the navbar bell and `/notifications` page; independent from external notification delivery rules/channels
- fields: `event_kind`, `category` (`monitors`/`agents`/`container_images`/`host_metrics` — the last added to the select by `v1_0001`), `severity`, `resource_type`, `resource_id`, `resource_name`, `title`, `message`, `payload`, `occurred_at`
- list/view rules: authenticated users; create/update/delete forbidden from the API (written only by backend)
- read state is per-user and stored in `user_settings.settings.system_notifications_last_read_at_by_category`; bell visibility is controlled by `system_notifications_enabled_events`
- indexed on `(category, occurred_at)` and `(event_kind, occurred_at)`, plus `occurred_at` (added by migration `v1_0001_system_notifications_retention.go`); purged by the daily retention job after `data_retention_settings.system_notifications_retention_days` (90 by default)

### `data_retention_settings`

- created by migration `8_create_data_retention_settings.go`
- singleton-like admin collection used for global lifecycle settings (`key = global`)
- fields:
  - `monitor_events_retention_days`
  - `notification_logs_retention_days`
  - `system_notifications_retention_days` (migration `v1_0001_system_notifications_retention.go`; empty on older hubs = default 90)
  - `monitor_events_manual_default_days`
  - `notification_logs_manual_default_days`
  - `offline_agents_manual_default_days`
- used by the retention cleanup logic and the admin purge settings UI
- monitoring events, notification logs and the in-app notification feed (`system_notifications`) have automatic age-based retention; hosts cleanup is manual-only and targets offline agents, aged by `agents.last_seen` (written at every handshake and when the agent goes offline)

### `scheduled_jobs`

- created by migration `10_create_scheduled_jobs.go`
- admin-only collection used to persist runtime state for registered scheduled jobs
- fields: `key`, `schedule`, `last_run_at`, `last_success_at`, `last_status`, `last_error`, `last_result`, `last_duration_ms`
- the hub keeps job definitions in code, while this collection stores the latest execution state shown in the admin UI

### `container_image_audits`

- created by migration `13_create_container_image_audits.go`
- latest-only audit result per `(agent, container_id)` for public container images discovered in Docker snapshots
- fields: `agent` (relation→agents, cascadeDelete=true), `container_id`, `container_name`, `image_ref`, `registry`, `repository`, `tag`, `local_image_id`, `local_digest`, `policy`, `status`, `latest_tag`, `latest_digest`, `checked_at`, `error`, `details` (json)
- `status` is one of `up_to_date`, `update_available`, `unknown`, `unsupported`, `check_failed`, `disabled`
- `policy` is one of `digest_latest`, `semver_major`, `semver_minor`, `unsupported`
- tag selection currently works like this: `latest` -> `digest_latest`; one-part numeric tags like `15` -> latest `15.x.x`; two-part tags like `15.2` -> latest `15.2.x`; three-part tags like `15.2.3` -> latest `15.2.x`. Tags are parsed via Masterminds/semver in `internal/hub/image_tag_parser.go`, with a whitelisted variant suffix list (`-alpine`, `-bookworm`, `-jdk-slim`, `-fpm-alpine`, etc.) and a regex for prerelease detection (`rc`/`alpha`/`beta`/`dev`/`pre`/`snapshot`/`milestone`). Prereleases are excluded from candidates unless the current tag is itself a prerelease, and pure numeric tags without dots are filtered as build IDs when the current tag has dots.
- `details` stores the richer audit view used by the dashboard UI, including the primary `line_status`, `line_latest_tag`, `same_major_latest_tag`, `overall_latest_tag`, whether a newer major exists, and `error_kind` when the check failed (`auth_failed`, `not_found`, `timeout`, `network`, `registry_error`, `unknown`)
- `last_notified_signature` and `last_notified_at` persist which newer version set has already been announced for this container so update notifications are not re-sent on every audit run
- the hub writes this collection from the scheduled image-audit job; agents never write it directly. The audit cycle uses a bounded worker pool and a per-cycle `cachingRegistryClient` that memoizes `ListTags` per repository and retries transient failures.

## First-Run User Flow

The first-run behavior is exposed through the hub and consumed by the frontend login flow.

Relevant files:

- `internal/hub/api.go`
- `internal/users/users.go`
- `internal/migrations/initial-settings.go`
- `internal/site/src/components/login/login.tsx`
- `internal/site/src/components/login/auth-form.tsx`

The flow is:

1. frontend calls `/api/app/first-run`
2. hub checks whether any users exist
3. if there are no users, the login page switches into account creation mode
4. the first created user becomes the initial admin path into the system

The migration bootstrap also supports initial credentials via env in `initial-settings.go`.

## User Roles And Authorization

Role logic is layered on top of PocketBase auth.

### Authentication

PocketBase handles the base auth mechanisms:

- email/password
- OAuth providers
- OTP and MFA features when enabled

### Authorization

The project adds role-aware behavior on top:

- admin-only routes and UI actions
- readonly restrictions
- user-scoped settings and enrollment token ownership

Key files:

- `internal/hub/api.go`
- `internal/hub/collections.go`
- `internal/site/src/lib/api.ts`

## User Settings Lifecycle

User settings are persisted separately from the main `users` record.

### Backend Behavior

The hub ensures the settings record exists and can be read through app routes.

### Frontend Behavior

The frontend:

- loads user settings after auth refresh
- stores them in nanostores
- updates them from the settings routes

Relevant files:

- `internal/site/src/lib/stores.ts`
- `internal/site/src/components/routes/settings/layout.tsx`
- `internal/site/src/components/routes/settings/general.tsx`

This separation lets the auth identity stay stable while the settings payload evolves independently.

## User Auth Features Controlled By Environment

Hub-side auth behavior is influenced by env values read through `internal/hub/utils/utils.go`.

Important variables:

- `APP_URL`
- `DISABLE_PASSWORD_AUTH`
- `USER_CREATION`
- `MFA_OTP`
- `AUTO_LOGIN`
- `TRUSTED_AUTH_HEADER`
- `TRUSTED_PROXY_IPS`
- `TRUSTED_PROXY_HEADERS`
- `RATE_LIMITS`
- `RATE_LIMIT_EXCLUDED_IPS`

Behavior notes:

- `DISABLE_PASSWORD_AUTH=true` disables normal email/password login
- `USER_CREATION=true` allows OAuth user self-registration
- `MFA_OTP=true` or `superusers` enables OTP requirements
- `AUTO_LOGIN` enables trusted automatic login for a specific email
- `TRUSTED_AUTH_HEADER` trusts an upstream auth header containing a user email — but
  **only** when the request's real TCP peer (`RemoteAddr`, registered in
  `internal/hub/api.go` `registerMiddlewares`) matches the `TRUSTED_PROXY_IPS` allowlist.
  The header value is fully attacker-controlled, so this peer check is what prevents a
  direct caller from impersonating any user. `X-Forwarded-For` is intentionally **not**
  used for the gate because it is itself spoofable.
- `TRUSTED_PROXY_IPS` is the comma/whitespace-separated allowlist of reverse-proxy IPs and
  CIDRs (IPv4/IPv6; a bare IP becomes `/32` or `/128`). **Fail-safe:** if it is empty or
  unset while `TRUSTED_AUTH_HEADER` is configured, the header is ignored entirely and a
  warning is logged at startup — a misconfiguration can never open an auth bypass. The
  parsing/matching helpers (`parseTrustedProxies`, `remoteIPAllowed`) are unit-tested.
- `TRUSTED_PROXY_HEADERS` (comma-separated, e.g. `X-Real-IP`) is copied into PocketBase's
  `settings.TrustedProxy.Headers` at startup (`applyTrustedProxySettings` in
  `internal/hub/rate_limits.go`, `UseLeftmostIP` forced off); when unset the stored value is
  kept. PocketBase trusts those headers from **any** peer, so the hub binds
  `bindTrustedProxyHeaderGuard` ahead of every PocketBase middleware (priority below CORS, so
  before the superuser IP whitelist and the rate limiter): it deletes them from requests
  whose `RemoteAddr` is not in `TRUSTED_PROXY_IPS` (empty allowlist → always deleted, with a
  startup warning). `e.RealIP()` — the rate-limit key — is therefore the TCP peer unless a
  trusted proxy vouches for the client. Headers set in the PocketBase dashboard follow the
  same rule: without `TRUSTED_PROXY_IPS` they are now ignored.
- Rate limiting: `applyRateLimitSettings` enables PocketBase's limiter on every start
  (PocketBase v0.40 ships it disabled) unless `RATE_LIMITS=false`, and appends the rules of
  `vigilRateLimitRules` whose label is missing — `*:auth` 2/3s (every auth collection,
  superusers included; covers password, OTP and OAuth2), `*:requestOTP` and
  `*:requestPasswordReset` 3/60s, `/api/app/agent-connect` 60/10s (generous: agents behind
  one NAT share a bucket and retry every few seconds). PocketBase's own defaults (`*:create`
  20/5s, `/api/batch` 3/1s, `/api/` 300/10s) stay. An existing rule with the same label is
  never overwritten (whatever its audience — an audience-less duplicate would fail PocketBase's
  prefix uniqueness check and stop the hub), so dashboard tuning survives restarts; a deleted
  Vigil rule is re-added, and the enabled flag follows the env. The limiter keys on the full
  client IP (an IPv6 /64 holder gets many keys) and there is no per-account lockout. Authenticated superusers bypass the limiter (PocketBase behavior). Over the limit the
  API answers `429`.
- `RATE_LIMIT_EXCLUDED_IPS` (IPs/CIDRs) sets `settings.RateLimits.ExcludedIPs`: PocketBase skips the
  limiter for those client IPs (`e.RealIP()`, so after the trusted-proxy guard). Meant for a hub
  that every user reaches from one address (office NAT, hub whitelisted to the office IP): without
  it they share the `*:auth` and `/api/` buckets. Invalid entries are dropped (PocketBase would
  otherwise reject the settings and stop the hub); unset keeps the stored value.

## Agent Authentication Model

Agent auth is separate from user auth.

There are three important concepts.

### 1. Enrollment Token

An enrollment token allows a not-yet-registered agent to self-register.

Current properties:

- minted by the hub (`security.RandomString(40)`) — a client-supplied value is never stored
- managed through `/api/app/agent-enrollment-token` (non-readonly users, not superusers): `GET` is read-only and returns `{token, active, permanent}` (the permanent DB token, else the ephemeral in-memory one, else empty); `POST {"enable", "permanent", "regenerate"}` changes it. Enabling or switching permanence keeps the current value (install commands already copied keep working); `regenerate` replaces and so revokes it; `enable: false` revokes both copies. The POST requires `Content-Type: application/json` (no cross-site HTML form can drive it, even with `AUTO_LOGIN`/`TRUSTED_AUTH_HEADER`), changes are serialized (`enrollmentTokenMu`) and every in-memory copy of the user is dropped, so a regenerate really revokes. The credential never travels in a URL (access logs). A permanent token chosen by a client before this change stays until it is regenerated
- associated with the user who created it
- can be temporary or persistent
- can be reused to register multiple agents, each of which gets its own persisted agent record after first successful connection

### 2. Agent Token

Once an agent record exists, its per-agent token becomes the credential used for reconnection.

The agent sends this token during the WebSocket connection request.

An agent enrolled with the enrollment token used to keep it as its per-agent token: every host enrolled with the same token shared one credential, and the only thing telling them apart was the fingerprint they report — a hash of the hostname, which anyone can compute. So the hub now issues each capable agent a token of its own.

**Issuance.** After `GetAgentInfo`, if the agent advertises `capabilities.agent_token` and its token is not one the hub minted for it (`agents.token_issued` false — migration `38_agent_identity.go`) or may be shared (it connected with an enrollment token, or several records carry it), `issueAgentToken` mints `security.RandomString(40)` and sends `SetAgentToken` (accepted only from a hub verified on that connection). The agent stores it first, keeping the token that authenticated that connection as a fallback until the new one has authenticated a verified connection; the hub then saves it and sets `token_issued`. If the acknowledgement is lost, the hub restarts or the save fails, the record keeps its old token and `token_issued` stays false: the agent's new token is refused, it falls back to the old one, and the next connection issues again. Existing agents migrate at their first connection after upgrading.

**Hosts awaiting approval.** An enrollment-token connection whose fingerprint belongs to a record with an issued token (`recordHeldByTokenAgent`) is a reinstall of that host — or someone holding the enrollment token posing as it. The hub cannot tell, so it neither refuses (which would lock a reinstalled host out) nor re-attaches (which would hand over the host): `awaitApproval` keeps one record per claimed host with `status = awaiting_approval` and `duplicate_of` = the claimed host. Its connection is kept and `GetAgentInfo` stored so the admin can see what it says it is, but nothing is collected or issued, and it is left out of the dashboard, hosts overview and fleet metrics (Settings → Agents lists it). An `agent.duplicate_fingerprint` notification (warning, keyed on the claimed host for throttling, **not** suppressed by maintenance windows) is emitted once. Admin decisions (`internal/hub/agent_approval.go`, `requireAdminRole`):

- `POST /api/app/agents/{id}/merge` — it is the claimed host, reinstalled: a new token for the claimed record is pushed through the pending connection (it must be connected), saved on the claimed record, and the pending record is deleted. History is kept. Refused (409) while the claimed host is itself connected — the strongest sign that the pending one is someone else — unless `?force=1` (the UI asks twice)
- `POST /api/app/agents/{id}/approve` — a different machine (shared hostname): it is issued its own token right away through its live connection (it must be connected, like for a merge, so the approved record never sits on the shared token) and becomes a host of its own
- `POST /api/app/agents/{id}/reject` — someone else: the pending record is deleted and the connection closed. It can connect again with the enrollment token (and wait again): regenerate the enrollment token

The identity fields (`status`, `duplicate_of`, `token_issued`, and the fingerprint of a pending host) cannot be changed through the collection API by non-superusers (`protectAgentIdentityFields`, an `OnRecordUpdateRequest` hook; `token` is hidden, so PocketBase ignores it in their bodies), and a pending host's token cannot be rotated (409). The `agent.duplicate_fingerprint` notification ignores maintenance windows and mutes. `duplicate_of` cascades: deleting the claimed host deletes its pending claimants. Resetting the fingerprint of a host that has its own token is admin-only (it would lift the guard).

Known limits: (1) all holders of the enrollment token presenting the same fingerprint share one pending record — the live connection is the latest one, so before merging make sure only your reinstalled host is connecting (regenerate the enrollment token first if in doubt); (2) deleting the claimed host (non-readonly users can) lifts the guard for its fingerprint, so a claimant reconnecting afterwards enrolls as a plain host; (3) pending records never go offline, so the offline purge never removes them — reject them.

**Rotation.** `POST /api/app/agents/{id}/rotate-token` pushes the new token to a connected capable agent before saving it (`pushed: true`; if the agent does not confirm, nothing is saved). An offline agent must be given the new token in its `TOKEN` — a changed `TOKEN` is tried first.

**Agent side.** The agent uses its issued token, then the token it replaced (the enrollment token only until the new one is confirmed), then the configured `TOKEN` only if it changed since the issuance — last, since re-running the install command rewrites `TOKEN`. It moves between them after 3 consecutive 401 answers; a database error on the hub answers 503, not 401. A host deleted on the hub therefore stays revoked (re-enroll it with `vigil-agent fingerprint reset`, or a new `TOKEN`), and a hub outage never makes the fleet re-enroll (`docs/agent/agent-runtime.md`).

**Transition risk.** Until an agent upgrades, its record keeps the shared token and the old fingerprint matching: someone holding the enrollment token can still attach to it, and, if their client advertises the capability, be issued that record's token before the real host. The real host then comes back **awaiting approval** while the impostor holds the original record. Upgrade agents soon after the hub, then regenerate the enrollment token. Records still marked **shared** in Settings → Agents (no issued token: hosts not upgraded, decommissioned hosts, old duplicates) remain open to that attack: upgrade, rotate or delete them. Treat an `agent.duplicate_fingerprint` notification you did not expect as an incident (`docs/troubleshooting/common-issues.md`).

`agents.fingerprint` stays visible: it is derived from the hostname, which is shown as the host name anyway.

### 3. Hub Identity Verification

The hub proves its identity by signing the agent token together with a nonce the agent sent on that connection (`X-Nonce`, `common.HubChallenge`) with the hub private key, so a captured signature cannot be replayed. Agents older than the nonce send none and get a signature over the token alone; an upgraded agent accepts that static form only from a hub key that never signed a nonce (see `docs/architecture/hub-agent-architecture.md`, Step 3).

The agent verifies that signature with the configured public key from:

- `KEY`
- `KEY_FILE`
- CLI key input

This prevents an attacker from impersonating the hub even if the agent knows only the hub URL and token.

## Hub Keypair Lifecycle

The hub stores an ED25519 keypair in the data directory.

Behavior:

- generated automatically on first run if missing
- private key stays local to the hub
- public key is exposed via `GET /api/app/info`
- loaded once at startup and cached (`Hub.GetSSHKey`, under `keyMu`): replacing or removing the file takes effect only after a hub restart, and a file removed mid-run never turns into a new identity

Relevant files:

- `internal/hub/hub.go`
- `internal/hub/api.go`
- `agent/client.go`

## Agent Registration And Identity

The agent identity model has two layers.

### Token Identity

The token controls whether the connection attempt is authorized.

### Fingerprint Identity

The fingerprint provides a stable agent identity across reconnects.

The fingerprint is:

- loaded from the agent data directory if present
- otherwise generated from the hostname
- then persisted for future reuse

This is separate from the human-readable agent name shown in the UI. The hub stores the latest hostname reported by the agent in `agents.name`, but hostname collisions are allowed. When multiple agents share the same hostname, the frontend disambiguates them visually with a short fingerprint suffix rather than forcing unique names in the database.

Relevant file:

- `agent/fingerprint.go`

This is why token reset and fingerprint reset are different operations.

## Environment Variable Model

Both hub and agent support prefixed and unprefixed env names.

### Hub Env Lookup

Defined in `internal/hub/utils/utils.go`.

Lookup order:

1. `APP_HUB_<KEY>`
2. `<KEY>`

### Agent Env Lookup

Defined in `agent/utils/utils.go`.

Lookup order:

1. `APP_AGENT_<KEY>`
2. `<KEY>`

This matters in derived products where env namespaces may be customized.

## Agent Environment Variables

Important agent variables include:

- `HUB_URL`
- `TOKEN`
- `TOKEN_FILE`
- `KEY`
- `KEY_FILE`
- `DATA_DIR`
- `LOG_LEVEL`

Behavior notes:

- `TOKEN_FILE` is an alternative to `TOKEN`
- `KEY_FILE` is an alternative to `KEY`
- a hub key is required: with no `KEY`/`KEY_FILE`/`--key`, the agent exits at startup
- `DATA_DIR` overrides automatic data-directory selection

## Data-Model Change Checklist

When changing auth or data behavior, check all of these areas:

1. migration snapshots
2. custom hub routes
3. collection auth settings
4. frontend types in `internal/site/src/types.d.ts`
5. frontend auth or settings flows
6. test helpers and integration tests

This repository’s auth behavior is not isolated in one place. Treat changes as cross-cutting until proven otherwise.
