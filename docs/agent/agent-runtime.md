# Agent Runtime

## Role Of The Agent

The Nexus agent is a lightweight remote process that connects outbound to the hub and responds to hub-initiated actions.

It is responsible for:

- loading connection configuration
- keeping a stable identity through fingerprint persistence
- verifying the hub's identity during connection
- maintaining the WebSocket session
- dispatching actions to registered handlers

The main code lives under `agent/` with the CLI entrypoint in `internal/cmd/agent/agent.go`.

## CLI Entry Point

The agent CLI is defined in `internal/cmd/agent/agent.go`.

This command handles:

- normal agent startup
- connection configuration from flags and env
- hub public-key loading
- utility subcommands such as health and fingerprint reset behavior

If you need to change startup flags or command-line UX, start there.

## Runtime Construction

The runtime is built around the `Agent` type in `agent/agent.go`.

Important responsibilities include:

- logger setup
- key storage for hub verification
- connection manager ownership

`Agent.Start(keys)` is the handoff point from CLI setup into the long-running connection loop.

## Environment Resolution

Agent env lookup is implemented in `agent/utils/utils.go`.

Lookup order is:

1. `APP_AGENT_<KEY>`
2. `<KEY>`

Important variables:

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
- missing key material means hub signature verification is skipped
- `LOG_LEVEL` configures runtime logging verbosity

## Data Directory Selection

Data directory discovery lives in `agent/data_dir.go`.

Selection order is:

1. explicit CLI-provided data dirs, if any
2. `DATA_DIR` env override
3. OS-specific defaults

Defaults include:

- `/var/lib/<agent-data-dir>` on Unix-like systems
- a user config fallback under the home directory
- Windows app-data paths when applicable

The agent checks for:

- directory existence
- writability
- ability to create the directory if needed

This directory is important because it stores the agent fingerprint.

## Fingerprint Lifecycle

Fingerprint behavior is implemented in `agent/fingerprint.go`.

The fingerprint model is:

- if a fingerprint file exists, reuse it
- otherwise generate a fingerprint from the hostname
- persist the generated fingerprint to the data directory

The agent also reports its current OS hostname through `GetAgentInfo` metadata so the hub can use a human-readable display name in the UI.

This gives the agent a stable identity across restarts while still allowing reset when needed.

Important functions:

- `GetFingerprint`
- `SaveFingerprint`
- `DeleteFingerprint`

Resetting the fingerprint is an identity-level change and is different from rotating the token.

## Health Behavior

Health behavior lives in `agent/health/health.go`.

The agent health model is file-based:

- a timestamp file is written in shared memory or temp storage
- the health check succeeds only if that file has been updated recently

Current behavior:

- Linux prefers `/dev/shm`
- other systems fall back to the OS temp directory
- the agent is considered unhealthy if the file is older than roughly 90 seconds

This is useful for service managers or deployment tooling that want a lightweight liveness check.

## Connection Lifecycle

The connection lifecycle is split between:

- `agent/connection_manager.go`
- `agent/client.go`

### Connection Manager

The connection manager is the long-running state machine.

Current states:

- `Disconnected`
- `WebSocketConnected`

Responsibilities:

- retrying connection attempts
- transitioning state when a connection succeeds or fails
- reacting to disconnect events
- updating health status during normal operation

### WebSocket Client

The WebSocket client is responsible for:

- reading `HUB_URL`
- reading the token from env or file
- building the `/api/app/agent-connect` URL
- opening the WebSocket session
- receiving requests and sending responses

## Hub Verification

Hub verification is implemented in `agent/client.go` and depends on loaded public keys from `agent/keys.go`.

Important behavior:

- the hub sends a signature challenge over WebSocket
- the agent verifies the signature against the configured public key set
- only after success does the agent set `hubVerified = true`

All handlers other than the fingerprint challenge require hub verification first.

If no keys are configured, the verification loop effectively has no keys to check and the agent operates without cryptographic hub verification.

That may be acceptable for development, but it should not be treated as the secure default.

## Handler Registry

The agent handler system lives in `agent/handlers.go`.

Main pieces:

- handler interface
- handler context
- registry construction in `NewHandlerRegistry()`
- per-action handler implementations

Built-in actions currently cover:

- hub fingerprint verification
- agent info reporting
- liveness ping
- host snapshot collection (`GetHostSnapshot`)

`GetAgentInfoHandler` now reports `"docker": collectors.DockerAvailable()` in the capabilities map so the hub knows whether Docker data will be present in snapshots.

The handler context provides access to:

- request ID
- request payload
- connection state
- response helpers

## Collectors Package

System data collection for snapshots lives under `agent/collectors/`.

Each collector is a focused function that gathers one domain of host data:

- `system.go` — OS info, CPU, memory, uptime
- `storage.go` — mounted filesystems and usage
- `packages_debian.go` — installed packages and pending updates (APT)
- `packages_redhat.go` — installed packages and pending updates (DNF/YUM)
- `repositories_debian.go` — APT repository sources
- `repositories_redhat.go` — DNF/YUM repository sources
- `reboot.go` — reboot-required detection
- `docker.go` — running container inventory

All collectors in this package use the `//go:build linux` build tag and are Linux-only. A non-Linux stub (`collectors_stub.go` or equivalent) provides no-op implementations so the agent compiles on other platforms without errors.

`snapshot.go` in the same package is the orchestrator: it calls each collector and assembles the full `HostSnapshotResponse`.

`DockerAvailable()` is exported from this package and used by `GetAgentInfoHandler` to populate the `"docker"` capability flag.

## How To Add A New Handler

Typical workflow:

1. append a new action constant in `internal/common/common-ws.go`
2. define any shared payload types there if needed
3. implement the handler in `agent/handlers.go`
4. register it in `NewHandlerRegistry()`
5. add the hub-side caller in `internal/hub/ws/handlers.go`
6. add or update tests

Do not reorder action constants.

## Response Helpers

Response helpers live in `agent/response.go` and are used by handlers to keep response framing consistent.

When writing new handlers, follow the existing response patterns instead of assembling ad hoc payloads in multiple styles.

## Useful Supporting Files

- `agent/keys.go`
  - parses hub public keys for signature verification
- `agent/response.go`
  - response helper construction
- `agent/utils/utils.go`
  - env and filesystem helpers

## High-Signal Files For Agent Work

- `internal/cmd/agent/agent.go`
- `agent/agent.go`
- `agent/client.go`
- `agent/connection_manager.go`
- `agent/handlers.go`
- `agent/fingerprint.go`
- `agent/data_dir.go`
- `agent/health/health.go`
- `agent/keys.go`
- `agent/collectors/` (snapshot orchestration and Linux-only system collectors)

## Safe Change Checklist

Before finishing an agent change, check whether it affects:

1. env handling and prefixed env lookup
2. fingerprint persistence
3. handshake and `hubVerified` gating
4. request or response compatibility with `internal/common/common-ws.go`
5. health behavior used by deployment tooling
6. tests that exercise connection lifecycle or handler behavior
