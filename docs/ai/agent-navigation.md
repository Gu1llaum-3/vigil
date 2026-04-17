# Agent Navigation

This document is for AI agents that need fast, task-oriented routing through the Nexus codebase.

Start with `docs/README.md` if you need broader orientation. Use this file when you already have a concrete task.

## Fast Task Routing

### Add Or Change A Hub API Endpoint

Start here:

- `internal/hub/api.go`
- `internal/hub/hub.go`
- `internal/users/users.go` if the endpoint touches user bootstrap or user defaults

Read next:

- `docs/backend/hub-backend.md`
- `docs/architecture/auth-and-data-model.md`

Remember:

- custom app routes live under `/api/app/*`
- auth and role middleware are defined in `internal/hub/api.go`

### Change Auth, Roles, Users, Tokens, Or Settings

Start here:

- `internal/hub/api.go`
- `internal/hub/collections.go`
- `internal/users/users.go`
- `internal/migrations/0_collections_snapshot_*.go`
- `internal/migrations/initial-settings.go`

Frontend touchpoints:

- `internal/site/src/components/login/*`
- `internal/site/src/components/routes/settings/*`
- `internal/site/src/lib/api.ts`

Read next:

- `docs/architecture/auth-and-data-model.md`

### Add A New Hub-To-Agent WebSocket Action

Start here:

- `internal/common/common-ws.go`
- `agent/handlers.go`
- `agent/collectors/` (if the action involves system data collection)
- `internal/hub/ws/handlers.go`
- `internal/hub/ws/request_manager.go`

Read next:

- `docs/architecture/hub-agent-architecture.md`
- `docs/agent/agent-runtime.md`

Mandatory rules:

- append the action constant, never reorder existing ones
- register the handler in `NewHandlerRegistry()`
- add a hub-side caller on `*ws.WsConn`
- use an explicit timeout from the hub side
- if collection logic is OS-specific, gate it with `//go:build linux` in `agent/collectors/` and provide a stub

### Change The Agent Handshake Or Connection Lifecycle

Start here:

- `agent/client.go`
- `agent/connection_manager.go`
- `internal/hub/agent_connect.go`
- `internal/hub/ws/ws.go`

Read next:

- `docs/architecture/hub-agent-architecture.md`

Watch for:

- `hubVerified` gating
- token and public-key verification behavior
- delayed disconnect signaling via `DownChan`

### Change Fingerprinting Or Agent Persistence

Start here:

- `agent/fingerprint.go`
- `agent/data_dir.go`
- `internal/cmd/agent/agent.go`

Read next:

- `docs/agent/agent-runtime.md`

### Change The Frontend App Shell

Start here:

- `internal/site/src/main.tsx`
- `internal/site/src/components/router.tsx`
- `internal/site/src/lib/api.ts`
- `internal/site/src/lib/stores.ts`

Read next:

- `docs/frontend/frontend-app.md`

Watch for:

- `APP.BASE_PATH`
- injected `APP` metadata
- PocketBase auth store behavior

### Change Login, OAuth, OTP, Or First-Run UX

Start here:

- `internal/site/src/components/login/login.tsx`
- `internal/site/src/components/login/auth-form.tsx`
- `internal/hub/api.go`
- `internal/users/users.go`

Read next:

- `docs/frontend/frontend-app.md`
- `docs/architecture/auth-and-data-model.md`

### Work On The Dashboard Home Page

Start here:

- `internal/site/src/components/routes/dashboard/`
- `internal/site/src/lib/dashboard-types.ts`
- `internal/hub/dashboard.go`
- `internal/hub/snapshots.go`
- `internal/hub/api.go` (route registration)

Read next:

- `docs/frontend/frontend-app.md`
- `docs/backend/hub-backend.md`

Watch for:

- `chart.js` and `react-chartjs-2` imports — use only inside the dashboard route
- `GET /api/app/dashboard` response shape matches `dashboard-types.ts`
- `POST /api/app/refresh-snapshots` requires non-readonly auth

### Change Migrations Or Snapshot Collection

Start here for snapshot-related migration work:

- `internal/migrations/2_create_host_snapshots.go`
- `internal/hub/snapshots.go`
- `internal/hub/agent_connect.go`

### Change Settings Or Agent Management UI

Start here:

- `internal/site/src/components/routes/settings/layout.tsx`
- `internal/site/src/components/routes/settings/general.tsx`
- `internal/site/src/components/routes/settings/agents.tsx`
- `internal/site/src/types.d.ts`

### Change Frontend Localization

Start here:

- `internal/site/lingui.config.ts`
- `internal/site/src/lib/i18n.ts`
- `internal/site/src/lib/languages.ts`
- `internal/site/src/locales/`

Run after changes:

- `npm run --prefix ./internal/site sync`
  or
- `npm run --prefix ./internal/site sync_and_purge`

### Change Migrations Or Collection Shape

Start here:

- `internal/migrations/0_collections_snapshot_*.go`
- `internal/migrations/initial-settings.go`
- all files that reference the affected collection name

Use searches like:

- `FindCollectionByNameOrId("...")`
- `OnRecordCreate("...")`
- `CountRecords("...")`
- `FindFirstRecordByFilter("...")`

### Work On Deployment, Packaging, Or Release Flow

Start here:

- `.goreleaser.yml`
- `supplemental/docker/`
- `supplemental/guides/systemd.md`
- `supplemental/scripts/`
- `supplemental/kubernetes/`
- `internal/ghupdate/*`

Read next:

- `docs/operations/deployment-and-packaging.md`

## High-Signal Files By Subsystem

### Global Metadata

- `app.go`
- `go.mod`

### Hub Backend

- `internal/cmd/hub/hub.go`
- `internal/hub/hub.go`
- `internal/hub/api.go`
- `internal/hub/collections.go`
- `internal/hub/agent_connect.go`

### Shared Protocol

- `internal/common/common-ws.go`

### Hub WebSocket Layer

- `internal/hub/ws/handlers.go`
- `internal/hub/ws/ws.go`
- `internal/hub/ws/request_manager.go`

### Agent

- `internal/cmd/agent/agent.go`
- `agent/agent.go`
- `agent/client.go`
- `agent/connection_manager.go`
- `agent/handlers.go`
- `agent/fingerprint.go`
- `agent/data_dir.go`
- `agent/health/health.go`
- `agent/collectors/` (snapshot orchestrator and Linux-only system collectors)

### Frontend

- `internal/site/src/main.tsx`
- `internal/site/src/components/router.tsx`
- `internal/site/src/lib/api.ts`
- `internal/site/src/lib/stores.ts`
- `internal/site/src/lib/dashboard-types.ts`
- `internal/site/src/components/login/*`
- `internal/site/src/components/routes/dashboard/*`
- `internal/site/src/components/routes/settings/*`
- `internal/site/src/types.d.ts`

### Migrations And Data Model

- `internal/migrations/0_collections_snapshot_0_19_0_dev_1.go`
- `internal/migrations/initial-settings.go`
- `internal/migrations/2_create_host_snapshots.go` (host_snapshots collection)

### Tests

- `internal/tests/hub.go`
- `internal/hub/*_test.go`
- `internal/hub/ws/*_test.go`
- `agent/*_test.go`

## Search Hints

Useful search terms in this repo:

- `agent-connect`
- `CheckFingerprint`
- `GetAgentInfo`
- `GetHostSnapshot`
- `SaveNoValidate`
- `user_settings`
- `agent_enrollment_tokens`
- `host_snapshots`
- `upsertHostSnapshot`
- `agentConns`
- `FindCollectionByNameOrId`
- `APP_URL`
- `AUTO_LOGIN`
- `TRUSTED_AUTH_HEADER`
- `create-user`
- `auth-refresh`
- `OAUTH_DISABLE_POPUP`
- `DockerAvailable`

## Rules Agents Should Remember

- Use `go test -tags=testing ./...` for real Go test execution.
- For internal agent status or similar internal record updates, prefer `SaveNoValidate` where the project already does.
- WebSocket actions are append-only.
- The hub should use explicit timeouts for hub-to-agent requests.
- Env lookup supports both prefixed and unprefixed names.
  - hub uses `APP_HUB_*` first
  - agent uses `APP_AGENT_*` first
- Production frontend serving is embedded; development frontend serving is proxied from Vite.
- The active transport is WebSocket even though some wording and abstractions still mention older transport ideas.

## Good Reading Order For Unclear Tasks

If the task is broad and you do not yet know where to edit, read in this order:

1. `app.go`
2. `docs/project-overview.md`
3. `docs/architecture/hub-agent-architecture.md`
4. `internal/cmd/hub/hub.go`
5. `internal/cmd/agent/agent.go`
6. `internal/site/src/main.tsx`

That usually gives enough context to route the rest of the task correctly.
