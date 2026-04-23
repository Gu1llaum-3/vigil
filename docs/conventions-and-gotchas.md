# Conventions And Gotchas

## Why This Document Exists

Nexus has several project-specific rules that are easy to miss if you only read one subsystem in isolation.

These rules are important because they protect wire compatibility, auth behavior, lifecycle correctness, and contributor workflow.

## Use `SaveNoValidate` For Internal Agent Lifecycle Updates

When hub code performs internal agent status updates, prefer `SaveNoValidate` over `Save`.

Why:

- these updates are operational bookkeeping
- full validation hooks are not the right mechanism for connection lifecycle state changes
- using `Save` can introduce unintended coupling to validation rules

If you are editing agent status update paths, follow the existing hub pattern.

## Use Explicit Timeouts For Hub-To-Agent Calls

The hub request manager provides default timeout behavior, but hub-side calls should still use explicit `context.WithTimeout` when invoking agent actions.

Why:

- the timeout intent stays visible at the call site
- it avoids hidden behavioral differences when request manager defaults change
- it keeps protocol-sensitive code easier to audit

High-signal files:

- `internal/hub/ws/handlers.go`
- `internal/hub/agent_connect.go`

## `GetHostSnapshot` Uses A 60-Second Timeout

The initial snapshot collection on agent connect uses a 60-second context timeout, not the short default.

Why:

- collecting packages, repositories, and Docker data can take several seconds on a loaded or slow host
- using the default 5-second timeout would cause most snapshot collections to fail silently

If you add collection steps to the snapshot, keep this timeout in mind and extend it if necessary. Do not silently drop the timeout or rely on the request manager default for this call.

Container image freshness checks are intentionally not part of `GetHostSnapshot`. They run later from a hub-side scheduled job against the latest stored Docker inventory so registry/network variability does not slow down normal host snapshot collection.

## Agent Collectors Are Linux-Only

The `agent/collectors/` package contains files built with `//go:build linux`.

A non-Linux stub is provided so the agent binary compiles on macOS and Windows, but it returns empty or zero-value data. Do not add platform-sensitive logic to the shared handler in `agent/handlers.go` — keep it inside the collectors package behind build tags.

## WebSocket Actions Are Append-Only

Action constants in `internal/common/common-ws.go` are encoded on the wire.

Rules:

- append new actions only at the end
- never reorder existing constants
- never renumber existing values manually

Breaking this rule can silently break protocol compatibility between hub and agent.

## Treat WebSocket As The Real Transport

The real transport path in this repository is WebSocket.

Do not infer active SSH transport support just because:

- some abstractions still mention transport layers
- some wording in docs or locale strings mentions SSH fallback

Current truth:

- the connection transport is WebSocket
- SSH keys are used for identity verification during the WebSocket handshake

If you see wording that implies more than that, treat it as legacy or inconsistent wording until confirmed in active code paths.

## Prefixed Env Lookup Is Built In

Both hub and agent support prefixed and unprefixed env names.

### Hub

Lookup order:

1. `APP_HUB_<KEY>`
2. `<KEY>`

### Agent

Lookup order:

1. `APP_AGENT_<KEY>`
2. `<KEY>`

This matters when debugging config because a prefixed variable can override an unprefixed one unexpectedly.

## Development Frontend And Production Frontend Behave Differently

Production:

- the frontend is embedded into the hub binary

Development:

- the hub proxies to the Vite dev server

Gotcha:

- if the Vite server is not running, frontend behavior in development may fail even though the hub process is healthy

Relevant files:

- `internal/hub/server_production.go`
- `internal/hub/server_development.go`
- `internal/site/embed.go`

## Tests Require The `testing` Build Tag

This repository’s Go tests are build-tagged.

Use:

```bash
go test -tags=testing ./...
```

or:

```bash
make test
```

Gotcha:

- plain `go test ./...` may not execute the test suite you think it does
- editors may show confusing “No packages found” warnings for test files

## `hubVerified` Is A Real Security Boundary

Agent handlers should not be treated as generally callable before hub verification completes.

Current rule:

- `CheckFingerprint` is the special verification path
- all other handlers depend on the verified hub state

If you add a new action or adjust handshake logic, keep this boundary intact.

## Missing Key Material Changes Security Posture

If no hub public key is provided to the agent, signature verification is skipped.

That may be acceptable for local development, but it is not equivalent to verified production operation.

Do not document or treat the no-key path as the preferred default.

## Fingerprint Identity And Token Identity Are Different

Two common mistakes:

- assuming token rotation resets identity
- assuming fingerprint reset rotates auth credentials

In this repository:

- token controls agent authentication
- fingerprint controls stable agent identity

Work on one does not automatically imply changes to the other.

## Offline Timing Is Deliberately Delayed

The hub does not mark an agent offline instantly on socket close.

There is a small delay around `DownChan` handling to give reconnects a chance to happen first.

Gotcha:

- if you are debugging brief disconnects, the UI and persisted status may lag behind the transport event slightly by design

## Keep Frontend Assumptions Base-Path Safe

The frontend uses router helpers and injected app metadata to handle base paths.

Gotcha:

- hardcoded absolute paths can break non-root deployments

When adding navigation or links, use the established helpers instead of building URLs manually.

## Current Frontend Is A Shell, Not A Full Product Surface

Do not over-document or over-engineer the frontend as if it were already a large application.

Current reality:

- login flows are real
- settings and agent management are real
- many advanced admin features still link into PocketBase admin pages

That should shape both implementation and documentation decisions.

## Renaming Is Centralized But Not Fully Automatic

`app.go` is the main source of rename-sensitive metadata, but renaming a derived project still requires reviewing:

- `go.mod`
- frontend package metadata
- deployment scripts
- release config
- localized user-facing strings

Do not assume `app.go` alone completes the rename.

## No PocketBase Hooks For Monitor Scheduler Lifecycle

Do not add `OnRecordAfterCreateSuccess` or `OnRecordAfterUpdateSuccess` hooks for the `monitors` collection.

Why:

- the monitor scheduler calls `SaveNoValidate` on every check result, which fires PocketBase update events
- a hook that calls `startMonitor` from those events creates an infinite loop: save → hook → startMonitor → doCheck → save → hook → …
- the only remaining hook for monitors is `OnRecordAfterDeleteSuccess`, which stops the goroutine

Goroutine lifecycle is managed only from the API handlers (`createMonitor`, `updateMonitor`, `deleteMonitor`).

## Dispatch Notifications Via Direct Call, Not Hooks

The notification dispatcher (`h.notifier.Dispatch`) is called directly from business logic, not from PocketBase record hooks.

Why:

- hooks on the `monitors` collection trigger on every scheduler save (see above), making them unsafe for notification dispatch
- direct calls keep the dispatch point obvious at the call site and avoid invisible side effects from hook chaining

Current dispatch points:

- `internal/hub/monitors.go` `saveResult` — after status transition
- `internal/hub/agent_connect.go` `setAgentStatus` — after agent status change
- `internal/hub/image_audits.go` `upsertContainerImageAudit` — after the scheduled image audit detects a newly changed set of newer available versions for a container

When adding new notification triggers, follow this pattern and do not use `OnRecordAfterUpdate` hooks on high-frequency collections.

## Good Default Verification Habit

For most non-trivial changes, the safe baseline is:

1. `go test -tags=testing ./...`
2. build the affected binary or frontend
3. manually sanity-check the changed flow if it is user- or protocol-facing

That habit will catch most repo-specific mistakes earlier than code review.
