# Agent Runtime

## Role Of The Agent

The Vigil agent is a lightweight remote process that connects outbound to the hub and responds to hub-initiated actions.

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
- `TAGS`

Behavior notes:

- `TOKEN_FILE` is an alternative to `TOKEN`
- `KEY_FILE` is an alternative to `KEY`
- a hub key is mandatory (`--key`, `KEY` or `KEY_FILE`): without one the agent exits at startup
- `LOG_LEVEL` configures runtime logging verbosity
- `TAGS` is a comma-separated list of free-text host tags reported in `GetAgentInfo` (`parseTags` trims/dedupes); the hub applies them only at first enrollment, so the UI remains the source of truth afterward

## Data Directory Selection

Data directory discovery lives in `agent/data_dir.go`.

Selection order is:

1. explicit CLI-provided data dirs, if any
2. `DATA_DIR` env override
3. OS-specific defaults

Defaults include:

- `/var/lib/<agent-data-dir>` on Unix-like systems
- a user config fallback under the home directory
- a Windows app-data branch in `agent/data_dir.go`, kept for compile-time portability only (no Windows agent is built)

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

The data directory also holds `agent-token` (`agent/agent_token.go`): the token the hub issued to this agent (`SetAgentToken`), stored as JSON `{hub, hub_key, token, previous, configured}` with mode 0600, fsynced, through a temporary file renamed into place (refused without a data directory; only letters and digits are accepted, like what the hub mints). It is used only for the hub that issued it: the canonical hub URL (`hubKeyFor`), or the SHA-256 fingerprint of the hub key that verified the issuing connection — so moving the hub to a new URL with the same key keeps it, but never over a weaker transport (`wss` → `ws`). `previous` is the token that authenticated the issuing connection (the hub accepts it until it has saved the new one; if it is the configured token, it is dropped once the new token authenticated a verified connection — `unconfirmed`, `confirmAgentToken`); `configured` is the SHA-256 of the configured `TOKEN` at issuance. Token choice (`tokenCandidates`): the stored token, then `previous`, then the configured `TOKEN` only if it changed since issuance (last: re-running the install command rewrites it); without a stored token, the configured one. The client moves to the next candidate after 3 consecutive 401 answers (`handleConnectError`), wrapping around — the configured token is never tried otherwise, so a host deleted on the hub stays revoked. `vigil-agent fingerprint reset` also deletes `agent-token`, since a new fingerprint is a new identity. The hub signs the token sent on each connection, so signature verification uses the token recorded in that connection's session.

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

Concurrency model: everything above runs on the single `Start` event loop goroutine. The WebSocket client (whose callbacks run on `gws` goroutines) only reports back by sending `WebSocketConnect` / `WebSocketDisconnect` on `eventChan`; it never touches the manager's state. After a disconnect the loop reconnects immediately, or — if the previous attempt is more recent than `connectAttemptSpacing` (5s) — arms a one-shot `retryC` timer that the same loop services; a failed attempt falls back to the 10s retry ticker. Do not reintroduce `go c.connect()` or other goroutines that mutate the manager: that is what produced the data races `go test -race` used to report. The state is stored atomically and read with `State()`, which is safe from any goroutine (the hub integration tests poll it).

### WebSocket Client

The WebSocket client is responsible for:

- reading `HUB_URL` and checking it (`checkHubURL`): `https://`/`wss://` connect over `wss`, `http://`/`ws://` over plaintext `ws` with a startup warning (the token and inventories travel in cleartext), any other scheme or a missing host is refused. An unusable configuration (no or invalid `HUB_URL`, no token) makes `Start` fail and the agent exit, instead of running idle while looking healthy to systemd (under the shipped units, `Restart=on-failure` then retries every 5s, so a fixed config or a late `TOKEN_FILE` recovers on its own); the error never echoes URL credentials (`u.Redacted()`)
- reading the token from env or file
- building the `/api/app/agent-connect` URL
- opening the WebSocket session
- receiving requests and sending responses

## Hub Verification

Hub verification is implemented in `agent/client.go` and depends on loaded public keys from `agent/keys.go`.

Important behavior:

- the hub sends a signature challenge over WebSocket
- the agent verifies the signature against the configured public key set
- only after success does the agent record that connection as verified (`verifiedConn`); a reconnect starts unverified, so every new connection must pass `CheckFingerprint` again

All handlers other than the fingerprint challenge require hub verification first.

A hub public key is mandatory. `loadPublicKeys` in `internal/cmd/agent/agent.go` reads `--key`, then `KEY`, then `KEY_FILE`, and the agent exits at startup with `no hub public key configured` when none of them yields a key (unset, empty, or comments only). `verifySignature` also fails closed on an empty key set, so there is no mode without hub verification, including for development: copy the key from the hub's *Add agent* dialog.

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
- host metrics collection (`GetHostMetrics`)
- running-container metrics collection (`GetContainerMetrics`)

`GetAgentInfoHandler` reports `"docker": collectors.DockerAvailable()` in the capabilities map so the hub knows whether Docker data may be present in snapshots. This remains a read-only inventory capability: the agent does not expose Docker start/stop/restart or other active workload operations.

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
- `packages_debian.go` — installed packages and pending updates (APT). Pending updates come from one `apt-get -s upgrade`; each `Inst` line is parsed positionally (`Inst <name> [<installed>] (<candidate> <origins> [<arch>])`), and a package is a security update only when one of **its own** origins is a security archive: an archive ending in `-security` (`Debian-Security:12/stable-security`, `jammy-security`, Ubuntu ESM `jammy-apps-security`/`jammy-infra-security`) or the `Debian-Security` label (Debian 10 archives have no suffix). Packages kept back by `upgrade` are not listed
- `packages_redhat.go` — installed packages and pending updates (DNF only: yum-only hosts such as Amazon Linux 2 or CentOS 7 report no pending updates). `dnf check-update` exit code `100` means updates, `0` none, anything else (or a missing `dnf`) is a collector error; the installed `@<repo>` lines of the "Obsoleting Packages" section are skipped; security flags come from `dnf updateinfo list security` (dnf4 syntax; dnf5 on Fedora 41+ finds no advisories with it)
- a failed pending-updates query is logged (`pending updates query failed`, with the command's stderr; a missing package manager only at debug level), but the snapshot still carries no outdated packages for that host, so the hub shows it as having no pending updates: the snapshot has no "unknown" state for this yet
- the package parsers are tested against real `apt-get`/`dnf` outputs captured from container images and kept in `agent/collectors/testdata/` (files named `*-synthetic.txt` are hand-written edge cases), served by fake binaries put first in `PATH` (`fakebin_test.go`); when changing a parser, add the real output that motivated it there
- `repositories_debian.go` — APT repository sources, in both formats: one-line `sources.list` / `sources.list.d/*.list` and deb822 `sources.list.d/*.sources` (the default on Debian 12+ images, Debian 13 and Ubuntu 24.04). A deb822 stanza yields one entry per URI × suite; only binary (`deb`) repositories are reported, and `Enabled: no` stanzas are kept with `enabled: false`
- `repositories_redhat.go` — DNF/YUM repository sources
- `reboot.go` — reboot-required detection
- `docker.go` — read-only Docker inventory (container state, image refs, image IDs, repo digests, exit code for terminal states)
- `metrics.go` — lightweight host monitoring metrics (CPU, memory, root disk, network throughput, 1/5/15-min load average, and the max used% across all real mounted filesystems). The load and max-disk fields back the host metric-threshold alerts; they are append-only CBOR fields, so older hubs ignore them and the hub degrades gracefully for older agents that omit them. To keep this high-frequency path cheap, the mounted-filesystem list is cached for 5 minutes (same interval as network-interface re-detection), network/remote filesystems (NFS/CIFS/…) are excluded so a stalled remote mount can't block collection, and pseudo / read-only-image filesystems (squashfs snap images, overlay, tmpfs, iso9660/udf — see `isPseudoFs`/`skipFSTypes` in `fstypes.go`) are excluded so they don't raise false 100%-full disk alerts. Root usage is read once via that same partition loop (`DiskMaxMount` reports the busiest mount).
- `container_metrics.go` — lightweight running-container monitoring metrics gathered from `docker stats --no-stream`

All collectors in this package use the `//go:build linux` build tag and are Linux-only. A non-Linux stub (`collectors_stub.go` or equivalent) provides no-op implementations so the agent compiles on other platforms without errors.

`snapshot.go` in the same package is the orchestrator: it calls each collector and assembles the full `HostSnapshotResponse`.

**Subprocess timeout:** `CollectSnapshot()` creates a `context.WithTimeout(45s)` that is propagated to every collector that spawns a subprocess (`packages_debian.go`, `packages_redhat.go`, `reboot.go`, `docker.go`). If a command blocks (e.g. a slow or unreachable DNF repository), it is killed after 45 seconds and the partial result is returned. Collectors that only read files (`system.go`, `storage.go`, `repositories_*.go`) do not receive the context. The 45s budget sits below the hub's 60s WebSocket timeout for `GetHostSnapshot`, leaving 15s for the response to be transmitted.

`DockerAvailable()` is exported from this package and used by `GetAgentInfoHandler` to populate the `"docker"` capability flag. The collector now returns more specific snapshot states such as `not_configured`, `cli_missing`, `daemon_unreachable`, and `permission_denied` so the hub and UI can distinguish configuration errors from a healthy Docker inventory.

`ContainerInfo` carries an `ExitCode *int` populated from `docker inspect` (`State.ExitCode`) only for containers in a terminal state (`exited` or `dead`). The pointer lets the hub distinguish "not reported" (nil) from "reported as 0", which matters because a clean `exited (0)` (one-shot job finished successfully) is classified differently from a non-zero exit. The field is `omitempty` on the wire and safe to roll out independently on agents and hubs.

`CollectContainerMetrics()` is intentionally separate from `docker.go` inventory collection. It samples only lightweight runtime metrics for running containers and does not mutate or enrich the snapshot inventory path. The collector currently parses `docker stats --no-stream` output and derives per-container network throughput from successive polls, similar in spirit to Beszel's separate `container_stats` history.

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
