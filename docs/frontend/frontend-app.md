# Frontend App

## Scope Of The Frontend

The frontend in Nexus is a small but real application shell embedded into the hub.

It currently covers:

- login and first-run account creation
- OAuth and OTP-related auth UX
- a live dashboard home route (host KPIs, hosts table, containers table, charts)
- a settings area
- agent enrollment-token and agent-management UI
- theme and language preferences

It is not yet a large product surface. Treat it as the reference implementation for how a derived project should connect UI, auth, settings, and hub APIs.

## Entry Point

The frontend entrypoint is `internal/site/src/main.tsx`.

This file is responsible for:

- booting React
- loading the router
- wiring theme and UI providers
- handling initial auth refresh behavior
- activating the selected locale

When the frontend fails to bootstrap correctly, this is the first place to inspect.

## Routing

Routing is centered on `internal/site/src/components/router.tsx`.

The router handles:

- route definitions
- base path support
- navigation helpers
- route matching for login, home, and settings pages

Current route surface is intentionally compact:

- login-related screens
- home (replaced by the live dashboard)
- settings routes

Important route helpers are re-exported and used across the app, especially by the navbar and settings flows.

## Base Path And App Injection

The frontend depends on a global `APP` object injected by the hub.

Important values include:

- display name
- base path
- hub URL
- app metadata used during login and navigation

This means frontend code should not assume deployment at `/` without checking the shared routing helpers.

Relevant files:

- `internal/hub/server.go`
- `internal/hub/server_production.go`
- `internal/hub/server_development.go`
- `internal/site/src/lib/utils.ts`
- `internal/site/src/components/router.tsx`

## PocketBase Client Integration

The PocketBase JS client is set up in `internal/site/src/lib/api.ts`.

This file is the main bridge between the frontend and backend. It handles patterns such as:

- auth refresh
- access to the current auth store
- role checks such as admin or readonly behavior
- logout
- calls to custom app endpoints

If a frontend change touches auth, current user state, or app-level requests, inspect `api.ts` first.

## Auth Flow

The login UI is organized under `internal/site/src/components/login/`.

Important files include:

- `login.tsx`
- `auth-form.tsx`
- `forgot-pass-form.tsx`
- `otp-forms.tsx`

### First-Run Flow

The login page calls `/api/app/first-run` to determine whether the app should show account creation instead of the normal sign-in UI.

### Standard Auth

PocketBase auth methods are loaded dynamically so the UI can adapt to enabled auth strategies.

### OAuth

OAuth behavior depends on PocketBase auth methods plus hub-side configuration. Popup behavior can also be influenced by environment settings.

### OTP And MFA

The frontend includes OTP request and entry flows that align with the hub and PocketBase MFA configuration.

## Layout And Navigation

Primary app navigation currently lives in `internal/site/src/components/navbar.tsx`.

This component demonstrates several frontend conventions used in the project:

- route-aware links via the router helpers
- admin-only UI branches
- lazy loading for heavier UI pieces
- a navbar-driven "Add agent" dialog for quick installation setup
- direct links into PocketBase admin views for some advanced operations

The monitors navbar icon also shows a live red badge when one or more monitors are currently `down`. It fetches `/api/app/monitors` and subscribes to the `monitors` PocketBase collection with the same 1-second debounce pattern used by the monitors page, so the badge updates dynamically without a full page refresh.

A sibling navbar icon (`BoxesIcon`) links to the dedicated `/images` page and shows an amber badge with the count of containers whose image audit currently reports `update_available`. It uses the same realtime + debounced fetch pattern via `pb.collection("container_image_audits").subscribe`.

The navbar notification bell is visible to every authenticated user. It fetches unread state from `GET /api/app/system-notifications/unread`, subscribes to the `system_notifications` collection, and clears via `POST /api/app/system-notifications/read-all`. This is separate from admin notification delivery logs and does not require email/webhook/in-app channels to be configured.

That last point is important: the custom frontend does not replace every PocketBase admin view yet.

## Monitors Route

The monitors page lives at `internal/site/src/components/routes/monitors.tsx`.

### Route Registration

Added to the router in `internal/site/src/components/router.tsx`:

```ts
monitors: "/monitors"
monitor: "/monitors/:id"
```

The page is lazy-loaded in `main.tsx`:

```tsx
const MonitorsPage = lazy(() => import("@/components/routes/monitors.tsx"))
```

**Suspense boundary requirement**: lazy-loaded page chunks must be wrapped in a `<Suspense>` boundary. The `<App />` component in `main.tsx` is wrapped in `<Suspense>` for this reason. Without it, the first navigation to a lazy chunk causes a blank screen while the module loads.

### Page Structure

- `MonitorsPage` (memo) — top-level component; fetches `/api/app/monitors` and `/api/app/monitor-groups` on mount; renders group sections
- `MonitorGroupSection` — collapsible group card (collapsed by default, persisted per browser) with edit/delete controls, an "Add monitor here" action, an up/total summary next to the group title, and a table of monitors inside the same bordered container when expanded; monitors without a group are rendered as a dedicated top section labeled "No group"
- `MonitorRow` — per-monitor row: status badge, name + last message, type badge, target (clickable for HTTP(S) monitors; push URLs keep their copy button), latency, rolling 24h average latency, 24h uptime, 30d uptime, mini status bars for the last checks, age, action dropdown with a `Move to` submenu
- `MonitorDetailPage` — dedicated monitor page with larger summary cards, 1h/3h/6h/24h range selector, a latency chart with red down bands, and a clickable target URL for HTTP(S) monitors
- `MonitorDialog` — create/edit form with type-conditional fields plus the failure threshold setting (`0` = instant down, default `3`)
- `GroupDialog` — simple group name form
- `StatusBadge` — green (UP), red (DOWN), outline (Pending — no last_checked_at yet)
- `TypeBadge` — monospace uppercase label (http/ping/tcp/dns/push)

### Push Monitors

When a monitor has type `push`, the UI exposes a generated `push_url`.

- copy the URL from the table row with the clipboard button
- call it from a cron job, timer, or script using `GET` or `POST` against `/api/app/push/:pushToken`
- the monitor becomes `Up` when a heartbeat arrives before `interval + 30s`

For a manual sanity check, run the URL once with `curl` and refresh the page.

### Realtime Updates

The monitors page subscribes to the `monitors` PocketBase collection with a **1-second debounce**:

```ts
const unsubscribeMonitors = await pb.collection("monitors").subscribe("*", () => {
  clearTimeout(debounceRef.current)
  debounceRef.current = window.setTimeout(fetchAll, 1000)
})
```

The debounce is critical: the hub scheduler updates monitor records frequently via `SaveNoValidate`. Without debouncing, each check result would fire a re-fetch, resulting in continuous GET requests.

The monitors page also includes `Expand all` and `Collapse all` buttons to manage long lists of groups more quickly.

Each monitor row can move the monitor to another group from its action menu, and each group menu includes `Add monitor here`.

### Type Definitions

Monitor-related TypeScript types live in `internal/site/src/lib/monitor-types.ts`:

- `MonitorType` — `"http" | "ping" | "tcp" | "dns" | "push"`
- `MonitorStatus` — `-1 | 0 | 1`
- `MonitorRecord`, `MonitorGroupResponse`, `MonitorGroupRecord`, `MonitorEventRecord`
- `MonitorFormData`, `defaultMonitorForm`

For the `ping` type, the monitors UI reuses the existing `hostname` field and lets the backend measure ICMP latency from the hub.
The phase 1 advanced options exposed in the form are `count`, per-request timeout, and IP family selection (`Auto`, `IPv4`, `IPv6`).

Rolling monitor metrics render as `N/A` only when no events exist in the window. As soon as at least one event is recorded within the 24h or 30d window, the corresponding metric is shown.

## Images Route

A dedicated container-image-audit page lives at `/images` (`internal/site/src/components/routes/images.tsx`). It complements the inline image-audit column on the dashboard by giving the data its own surface.

```ts
images: "/images"
```

```tsx
const ImagesPage = lazy(() => import("@/components/routes/images.tsx"))
```

What the page renders:

- a header with the total audited container count and an admin-only `Check images now` button (calls `POST /api/app/jobs/vigilContainerImageAudit/run`)
- five counter cards: `New major versions`, `Updates available`, `Check failed`, `Up to date`, `Disabled`
- collapsible groups by bucket in priority order (major → update → failed → up_to_date → disabled → other), each row showing the container/host, current image ref, status badge, and short tag pills (line / same major / new major) with the audit timestamp
- a right-side `Sheet` drawer per row with full version, digest, and source details, plus admin actions `Pin to current tag` and `Disable audit`. Pinning posts an override with `policy=patch` and a `tag_include` regex anchored on the current tag.
- a free-text search input that filters across container, host, image ref, and tag fields
- distinct empty states for no configured agents, no detected containers, containers without generated audit results, and active filters with no matches

Data source: `GET /api/app/dashboard` (filtered to entries with a non-empty `image_audit`). The page subscribes to the `container_image_audits` PocketBase collection with the standard 1-second debounce so audit state updates propagate without a manual refresh; the drawer re-resolves its selected row against the latest data each render so the open detail view stays in sync.

Override edits made elsewhere (the dashboard `OverrideMenu` and its `AdvancedOverrideDialog`, the page's pin/disable buttons) all hit the same admin endpoint at `/api/app/container-audit-overrides`. The advanced dialog exposes the regex `tag_include` / `tag_exclude` fields with client-side validation (`new RegExp(...)` in a `try/catch`), in addition to the policy select and a free-form notes field.

## Settings Area

## Dashboard Components

The dashboard home page lives under `internal/site/src/components/routes/dashboard/`.

The route header also includes a manual `Refresh` action for immediate snapshot collection and now shows the most recent host snapshot timestamp next to that button so operators can see when fleet data last refreshed.

Components:

- `kpi-cards.tsx` — summary metric cards (host connectivity ratio, monitor up/total ratio, pending updates, etc.)
- `hosts-table.tsx` — per-host patch state table
- `hosts-filter-sheet.tsx` — Radix `Sheet`-based multi-facet filter panel for the hosts table (exports `HostsFilters`, `defaultHostsFilters`, `applyHostsFilters`, `countHostsFilters`, and the `HostsFilterSheet` component)
- `containers-table.tsx` — running Docker container inventory plus read-only image audit badges, with a clipboard shortcut on the image reference
- `containers-filter-sheet.tsx` — equivalent multi-facet filter panel for the containers table
- `charts.tsx` — bar/doughnut charts using `chart.js` and `react-chartjs-2`
- `empty-state.tsx` — shown when no snapshot data is available yet

The `Patch Status` donut and the host patch badge both follow the same priority order: `Reboot required`, `Security updates`, `Out of SLA (>30d)`, `Compliant`, and `Unknown / Pending`.
The `Unknown / Pending` state is used when update data exists but the agent could not determine the last upgrade time.

Shared dashboard type definitions are in `internal/site/src/lib/dashboard-types.ts`. These types map the JSON shape returned by `GET /api/app/dashboard`, including the optional per-container `image_audit` block merged from the backend `container_image_audits` collection.

Both tables filter via a side `Sheet` panel rather than chips. Each table exposes a `Filters` button (with an active-count badge) next to its search input; the panel combines a single-select facet (Connection / Status) with multi-select checkbox groups (Compliance + Features for hosts, Severity + Image audit for containers). Combination semantics are intersection between groups (AND) and union within a group (OR); an empty multi-select group means "no constraint". The whole filter state (`HostsFilters`, `ContainersFilters`) is lifted into `home.tsx` so KPI cards stay wired to the hosts filter via small adapters (`deriveKpiKey` / `kpiKeyToHostsFilters`) without forcing the cards to know the facet shape, and so that the `Containers` KPI card can preset the containers filter (Errors / Warnings / Running) when clicked.

Container severity is classified by a single helper in `internal/site/src/lib/container-status.ts` (`containerSeverity`), kept in sync with `containerSeverity()` in `internal/hub/dashboard.go`:

- **ok** — `running`
- **warning** — `restarting`
- **error** — `dead`, or `exited` with a non-zero exit code
- **neutral** — `exited (0)` (a one-shot job that finished cleanly), `paused`, `created`, unknown

The `Containers` KPI card on the dashboard reflects this: its border tint follows the highest severity present (red > amber > green) and a badge in the top-right corner shows the total count of problematic containers when `summary.containers_in_error + summary.containers_in_warning > 0`. The badge tooltip breaks down the counts and takes precedence over the image-updates indicator when both would otherwise be shown.

The image-audit cell now distinguishes:

- a primary line status such as `Up to date`, `Patch available`, or `Minor available`
- an optional secondary badge when a newer major exists

The tooltip expands that summary with `Current`, `Latest in line`, `Latest same major`, and `Latest overall` so a pinned tag can still be shown as current in its patch line while surfacing a future major upgrade path.

`chart.js` and `react-chartjs-2` are added dependencies. They are used only within the dashboard route and should not be imported in other parts of the application.

## Settings Area

Important files:

- `layout.tsx`
- `general.tsx`
- `agents.tsx`
- `notifications.tsx`
- `purge.tsx`

### `layout.tsx`

Acts as the shared settings shell and includes logic related to loading and saving settings.

### `general.tsx`

Handles user-facing preferences such as:

- language
- layout width
- time format

### `agents.tsx`

Handles agent-related administration such as:

- viewing agent records
- managing enrollment tokens
- token generation helpers used by the UI

The navbar also exposes a lightweight installation dialog that fetches the hub public key and the current enrollment token, then provides ready-to-copy Docker and binary installation commands for new agents.

The agents settings table prefers the persisted agent hostname (`agents.name`) over the record id. If more than one agent shares the same hostname, the UI appends a short fingerprint suffix for display-only disambiguation.

## Notifications Route

The system notifications page lives at `/notifications` in `internal/site/src/components/routes/notifications.tsx`.

It is user-facing rather than admin-only and consumes these custom API routes:

```
GET   /api/app/system-notifications?category=&severity=&status=&q=&page=&limit=
POST  /api/app/system-notifications/read-all?category=
GET   /api/app/system-notifications/preferences
PATCH /api/app/system-notifications/preferences
```

The page uses the same compact table pattern as the dashboard host/container tables. It lets users search events, filter by category, unread state, and severity, clear filters with removable pills, and mark visible categories as read.

### `jobs.tsx`

Admin-only settings page used to inspect and run registered scheduled jobs.

Current responsibilities:

- list active jobs from `GET /api/app/jobs`
- show each job schedule (displayed as `<cron> (UTC)` since all cron schedules are evaluated in UTC by PocketBase), last run, last success, last duration, and last error
- execution timestamps are stored in UTC and rendered in the viewer's local browser timezone with timezone abbreviation via `Intl.DateTimeFormat` with `timeZoneName: "short"`
- expose `Run Now` via `POST /api/app/jobs/{key}/run`
- render the last persisted result payload for debugging/admin visibility

Current built-in jobs shown in this page include the retention cleanup job and the public container image audit job.

### `purge.tsx`

Admin-only settings page used to manage automatic retention and manual cleanup.

Current responsibilities:

- configure automatic retention for `monitor_events`
- configure automatic retention for `notification_logs`
- run manual purge actions for:
  - probe history
  - notification history
  - offline hosts

Important behavior:

- `offline hosts` means agent records with `status='offline'`
- deleting offline hosts also removes their current `host_snapshots` through cascade delete
- the destructive `Delete all offline hosts` action never touches connected agents

## State Management

The frontend uses nanostores for shared app state.

Main store definitions live in `internal/site/src/lib/stores.ts`.

Important state includes:

- router state
- user settings
- text direction
- transient UI helpers like clipboard fallback state

The project uses stores for small shared state, not a large central application store.

## Types And Shared Shapes

Frontend-side application types are declared in `internal/site/src/types.d.ts`.

This file is important when backend collection shape or settings fields change because the frontend consumes backend records directly in several places.

If you change:

- settings fields
- agent record shape
- frontend-visible app metadata

update the related types here.

## Theme And UI

Theme-related behavior is bootstrapped from the frontend entrypoint and UI helpers.

Relevant files include:

- `internal/site/src/components/theme-provider.tsx`
- `internal/site/src/components/mode-toggle.tsx`
- `internal/site/src/index.css`

The project uses Tailwind CSS v4-style setup with componentized UI primitives.

## Localization

Localization is built with Lingui.

Important files:

- `internal/site/lingui.config.ts`
- `internal/site/src/lib/i18n.ts`
- `internal/site/src/lib/languages.ts`
- `internal/site/src/locales/`

The frontend:

- detects locale from local storage or browser preferences
- dynamically loads locale bundles
- switches document direction for RTL languages

Build and sync commands are defined in `internal/site/package.json`.

## Realtime And Data Refresh

The frontend uses a combination of HTTP fetches and PocketBase realtime subscriptions.

The dashboard home page (`components/routes/home.tsx`) subscribes to four collections:

- `agents` collection — updates `host.status` and recalculates `summary.connected_hosts` / `summary.offline_hosts` in real time when an agent connects or disconnects
- `host_snapshots` collection — triggers a debounced `fetchDashboard()` (1 s delay) whenever any snapshot is written; this is what delivers the periodic auto-refresh driven by the backend ticker
- `monitors` collection — debounced 1 s; keeps the monitor KPI card fresh
- `container_image_audits` collection — debounced 1.5 s; keeps the container table's image audit column fresh after "Check images now" or an audit-override change (the override API handlers stamp `status=disabled`/`unknown` directly so this subscription fires without waiting for the next audit cycle)

The debounces are intentional: the backend ticker updates all agents roughly simultaneously, and the audit job rewrites every audit record on each run — without debouncing, a fleet of N agents/audits would fire N re-fetches in quick succession.

The manual Refresh button (`POST /api/app/refresh-snapshots` + `GET /api/app/dashboard`) remains available for on-demand collection outside the ticker cycle.

The settings agents route is one of the best current examples of frontend/backend coordination in the UI layer.

## Frontend Development Modes

### Production Build

The frontend is built into `internal/site/dist` and embedded into the hub binary.

### Development Build

During development, the hub proxies to the Vite dev server. This is why frontend changes may appear missing if only the hub is running without `make dev-server`.

## Notifications Settings Route

The notifications settings page lives at `/settings/notifications` and is admin-only (the nav item uses `admin: true` so only admins see it). It configures the navbar bell event preferences, external notification delivery channels, routing rules, and delivery history. The user-facing navbar bell and `/notifications` page are backed by the separate `system_notifications` feed.

The implementation lives in `internal/site/src/components/routes/settings/notifications.tsx`.

The page is split into two tabs:

- `Configuration` — channel and rule management
- `History` — paginated delivery log explorer implemented in `internal/site/src/components/routes/settings/notifications/history.tsx`

### Channels Section

- Lists all configured notification channels from `GET /api/app/notifications/channels`
- Each row shows: name, kind badge, enabled toggle, dropdown menu (Send test / Edit / Delete)
- "Add channel" button opens a dialog with a name field, kind select, enabled toggle, and config fields rendered dynamically per kind
- Kind is locked after creation (you cannot change a channel's kind on edit; delete and re-create instead)
- Config fields per kind:
  - `email`: to, cc, bcc
  - `webhook`: url, method, headers (JSON textarea)
  - `slack`: url (redacted), channel, username
  - `teams` / `gchat`: url only (redacted)
  - `ntfy`: url, token (redacted), priority
  - `gotify`: url, token (redacted), priority
  - `in-app`: no external config; matching notifications are shown as local UI toasts for the rule owner
- Sensitive fields that come back as `**REDACTED**` from the API are shown as-is with a hint; sending them back unchanged preserves the stored secret

### Rules Section

- Lists all routing rules from `GET /api/app/notifications/rules`
- Each row shows: name, event badges, channel name badges (resolved from local channel list), enabled toggle, dropdown menu (Edit / Delete)
- "Add rule" dialog fields:
  - name
  - enabled toggle
  - events checkboxes (monitor.down, monitor.up, agent.offline, agent.online, container_image.update_available)
  - channels multi-select (scrollable list of existing channels with kind badges); a single rule can target several channels at once
  - older databases are normalized by migration `7_notification_rule_channels_multi.go` so the persisted relation really behaves as multi-select
  - throttle_seconds number input (0 = no throttle)
- The UI no longer exposes `min_severity` because it was redundant with explicit event selection for the current event model; rules created or edited from the UI are normalized to `info`
- No realtime subscription on these collections — they are low-frequency configuration data; the page reflects state at mount time

### API Calls

All calls go through `pb.send()` to the custom hub API endpoints (not PocketBase collections directly):

```
GET    /api/app/notifications/channels
POST   /api/app/notifications/channels
PATCH  /api/app/notifications/channels/{id}
DELETE /api/app/notifications/channels/{id}
POST   /api/app/notifications/channels/{id}/test   → {ok, preview?, error?}
GET    /api/app/notifications/rules
POST   /api/app/notifications/rules
PATCH  /api/app/notifications/rules/{id}
DELETE /api/app/notifications/rules/{id}

GET    /api/app/notifications/logs?rule_id=&status=&event_kind=&since=&until=&page=&limit=
```

### History Tab

- Fetches paginated logs from `GET /api/app/notifications/logs`
- Filter controls:
  - rule
  - status (`sent` / `failed` / `throttled`)
  - event kind, including `container_image.update_available`
  - date range (`since` / `until`)
- The table shows sent time, event kind, rule, channel, status, and resource id/type
- Each row opens a detail dialog with the stored `payload_preview` and full `error` text

## Notification Toasts

The authenticated app shell mounts `internal/site/src/components/notification-log-toasts.tsx` from `main.tsx`.

This component:

- subscribes to the `notification_logs` PocketBase collection in realtime
- filters to logs where `created_by` matches the current auth record
- surfaces `failed` logs as destructive toasts
- surfaces `sent` logs as normal toasts when `channel_kind = "in-app"`
- also surfaces `sent` logs for `monitor.down` and `agent.offline` as immediate red UI alerts, even if the underlying notification was delivered through webhook, email, Slack, etc.
- also surfaces `sent` logs for `monitor.up` and `agent.online` as green recovery alerts

Container image update notifications do not currently get a dedicated incident-style toast treatment. They follow the standard delivery flow for external notification logs, while the separate system notification feed exposes matching image-audit event details in the navbar bell and `/notifications` page.

The component deduplicates these alert toasts for a short window so a single event does not produce one toast per configured channel, and keeps the alert toasts visible longer than the default informational toast.

The toast titles and descriptions also go through the normal Lingui catalogs, so changing their copy requires the usual locale extract/compile workflow.

## High-Signal Files For Frontend Work

- `internal/site/src/main.tsx`
- `internal/site/src/components/router.tsx`
- `internal/site/src/lib/api.ts`
- `internal/site/src/lib/stores.ts`
- `internal/site/src/types.d.ts`
- `internal/site/src/lib/dashboard-types.ts`
- `internal/site/src/components/login/*`
- `internal/site/src/components/routes/dashboard/*`
- `internal/site/src/components/routes/settings/*`
- `internal/site/src/lib/i18n.ts`
- `internal/site/src/lib/languages.ts`

## Safe Change Checklist

Before finishing a frontend change, check whether it affects:

1. base path behavior
2. global `APP` metadata assumptions
3. PocketBase auth refresh or auth store usage
4. settings record shape and frontend types
5. locale extraction or compiled locale output
6. development mode versus embedded mode behavior
