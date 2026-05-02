# Auth And Data Model

## Why This Document Exists

In Nexus, authentication, user setup, agent enrollment, and persisted settings are spread across:

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

### `agent_enrollment_tokens`

- stores user-owned enrollment tokens
- can be used to self-register new agents
- supports temporary and persistent enrollment patterns

The collection is tied to the user that created the enrollment token.

### `monitor_groups`

- created by migration `3_create_monitors.go`
- groups monitors for display organization
- fields: `name`, `weight` (sort order)
- monitors reference a group via relation field; deleting a group ungroups its monitors (relation set to empty, not cascaded)

### `monitors`

- created by migration `3_create_monitors.go`
- one record per configured uptime monitor
- common fields: `name`, `type` (select: `http`/`tcp`/`dns`/`push`), `group` (relation→monitor_groups), `active`, `interval` (seconds), `timeout` (seconds)
- type-specific fields: `url`, `http_method`, `http_accepted_codes` (json), `keyword`, `keyword_invert`, `hostname`, `port`, `dns_host`, `dns_type`, `dns_server`, `push_token`
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
- fields: `event_kind`, `category` (`monitors`/`agents`/`container_images`), `severity`, `resource_type`, `resource_id`, `resource_name`, `title`, `message`, `payload`, `occurred_at`
- list/view rules: authenticated users; create/update/delete forbidden from the API (written only by backend)
- read state is per-user and stored in `user_settings.settings.system_notifications_last_read_at_by_category`; bell visibility is controlled by `system_notifications_enabled_events`

### `data_retention_settings`

- created by migration `8_create_data_retention_settings.go`
- singleton-like admin collection used for global lifecycle settings (`key = global`)
- fields:
  - `monitor_events_retention_days`
  - `notification_logs_retention_days`
  - `monitor_events_manual_default_days`
  - `notification_logs_manual_default_days`
  - `offline_agents_manual_default_days`
- used by the retention cleanup logic and the admin purge settings UI
- only monitoring events and notification logs currently have automatic age-based retention; hosts cleanup is manual-only and targets offline agents

### `scheduled_jobs`

- created by migration `10_create_scheduled_jobs.go`
- admin-only collection used to persist runtime state for registered scheduled jobs
- fields: `key`, `schedule`, `last_run_at`, `last_success_at`, `last_status`, `last_error`, `last_result`, `last_duration_ms`
- the hub keeps job definitions in code, while this collection stores the latest execution state shown in the admin UI

### `container_image_audits`

- created by migration `13_create_container_image_audits.go`
- latest-only audit result per `(agent, container_id)` for public container images discovered in Docker snapshots
- fields: `agent` (relation→agents, cascadeDelete=true), `container_id`, `container_name`, `image_ref`, `registry`, `repository`, `tag`, `local_image_id`, `local_digest`, `policy`, `status`, `latest_tag`, `latest_digest`, `checked_at`, `error`, `details` (json)
- `status` is one of `up_to_date`, `update_available`, `unknown`, `unsupported`, `check_failed`
- `policy` is one of `digest_latest`, `semver_major`, `semver_minor`, `unsupported`
- tag selection currently works like this: `latest` -> `digest_latest`; one-part numeric tags like `15` -> latest `15.x.x`; two-part tags like `15.2` -> latest `15.2.x`; three-part tags like `15.2.3` -> latest `15.2.x`
- `details` stores the richer audit view used by the dashboard UI, including the primary `line_status`, `line_latest_tag`, `same_major_latest_tag`, `overall_latest_tag`, and whether a newer major exists
- `last_notified_signature` and `last_notified_at` persist which newer version set has already been announced for this container so update notifications are not re-sent on every audit run
- the hub writes this collection from the scheduled image-audit job; agents never write it directly

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

Behavior notes:

- `DISABLE_PASSWORD_AUTH=true` disables normal email/password login
- `USER_CREATION=true` allows OAuth user self-registration
- `MFA_OTP=true` or `superusers` enables OTP requirements
- `AUTO_LOGIN` enables trusted automatic login for a specific email
- `TRUSTED_AUTH_HEADER` trusts an upstream auth header containing a user email

## Agent Authentication Model

Agent auth is separate from user auth.

There are three important concepts.

### 1. Enrollment Token

An enrollment token allows a not-yet-registered agent to self-register.

Current properties:

- created from the hub side
- associated with the user who created it
- can be temporary or persistent
- can be reused to register multiple agents, each of which gets its own persisted agent record after first successful connection

### 2. Agent Token

Once an agent record exists, its per-agent token becomes the credential used for reconnection.

The agent sends this token during the WebSocket connection request.

### 3. Hub Identity Verification

The hub proves its identity by signing the agent token with the hub private key.

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
- if no key is configured, hub identity verification is skipped
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
