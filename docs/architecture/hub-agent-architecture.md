# Hub-Agent Architecture

## Runtime Shape

Vigil is built around two long-running components:

- the hub, which owns the database, auth model, custom API, frontend serving, and agent lifecycle tracking
- the agent, which runs remotely and opens an outbound WebSocket connection back to the hub

This is an outbound-only agent model. The hub initiates requests after the connection is established.

## Main Responsibilities

### Hub Responsibilities

The hub is responsible for:

- starting PocketBase and applying migrations
- configuring auth and collection rules
- exposing custom routes under `/api/app/*`
- accepting agent WebSocket connections at `/api/app/agent-connect`
- verifying agent identity and storing agent records
- requesting agent info and monitoring connection health
- serving the frontend in development and production

Key files:

- `internal/cmd/hub/hub.go`
- `internal/hub/hub.go`
- `internal/hub/api.go`
- `internal/hub/agent_connect.go`
- `internal/hub/ws/*`

### Agent Responsibilities

The agent is responsible for:

- loading connection configuration from env or flags
- selecting a writable data directory
- persisting a stable fingerprint
- opening and maintaining the WebSocket connection
- verifying the hub's identity challenge
- dispatching hub requests to registered handlers

Key files:

- `internal/cmd/agent/agent.go`
- `agent/agent.go`
- `agent/connection_manager.go`
- `agent/client.go`
- `agent/handlers.go`
- `agent/fingerprint.go`

## Startup Paths

### Hub Startup

The hub startup path is:

1. `internal/cmd/hub/hub.go`
2. `getBaseApp()` creates the PocketBase app and registers migration support
3. `hub.NewHub(baseApp)` wraps the PocketBase app in the project `Hub` type
4. `StartHub()` registers middleware, routes, hooks, and frontend serving
5. PocketBase starts serving HTTP

The `Hub` type is where project-specific behavior is attached to PocketBase.

### Agent Startup

The agent startup path is:

1. `internal/cmd/agent/agent.go`
2. flags are parsed and env may be populated from CLI options
3. hub public keys are loaded from the `--key` flag, then `KEY`, then `KEY_FILE`; the agent exits if none yields a key
4. `agent.NewAgent()` initializes the runtime
5. `Agent.Start(keys)` hands control to the connection manager
6. the connection manager opens and maintains the WebSocket connection

## Protocol Overview

The shared request and response types live in `internal/common/common-ws.go`.

Important facts:

- actions are encoded as `uint8`
- action values are assigned by `iota`
- action order is wire-sensitive and must remain append-only
- the transport payload format is CBOR

The current built-in actions are:

- `GetAgentInfo` (0)
- `CheckFingerprint` (1)
- `Ping` (2)
- `GetHostSnapshot` (3) — requests a full system snapshot from the agent (OS, resources, storage, packages, repositories, reboot state, Docker)
- `GetHostMetrics` (4) — requests lightweight periodic host monitoring metrics (CPU, memory, root disk usage, network throughput)
- `GetContainerMetrics` (5) — requests lightweight periodic running-container metrics (CPU, memory usage, network throughput)

### Request Shape

Hub requests are encoded as:

- `Action`
- optional `Data`
- optional request `Id`

### Response Shape

Agent responses are encoded as:

- optional request `Id`
- optional `Error`
- raw CBOR `Data`

The request ID is what allows the hub request manager to match replies to in-flight requests.

## Handshake And Verification Lifecycle

The connection flow is split between `agent/client.go`, `agent/connection_manager.go`, and `internal/hub/agent_connect.go`.

### Step 1: Agent Connects

The agent creates a WebSocket client using:

- `HUB_URL`
- `TOKEN` or `TOKEN_FILE`
- headers `X-Token` and `X-App`

The URL is transformed into the hub endpoint `/api/app/agent-connect`.

### Step 2: Hub Validates Connection Attempt

The hub:

- validates agent headers
- checks whether the token matches an existing agent or an enrollment token
- upgrades the HTTP request to WebSocket

### Step 3: Hub Challenges Agent

After upgrade, the hub signs the agent token with its private ED25519 key and sends a `CheckFingerprint` request.

The agent verifies that signature using the configured hub public key or keys.

If verification succeeds:

- the agent marks that connection as verified (`verifiedConn`; per connection, so a reconnect must verify again)
- the agent responds with its stable fingerprint

> **Known limitation — static challenge (deferred hardening).** The hub signs the agent's
> *token* itself (`challenge := []byte(token)` in `internal/hub/ws/handlers.go GetFingerprint`),
> not a fresh per-connection nonce. Because ED25519 is deterministic and enrollment tokens are
> intentionally shared across agents, the resulting signature is constant for a given (hub key,
> token) pair, so it is replayable: anyone who already knows the token and has observed one valid
> signature (a compromised sibling agent on the same token, or a one-time capture via a
> `HUB_TLS_INSECURE` MITM) can replay it to impersonate the hub to other agents on that token.
> Impact is bounded — the agent's handler set is read-only (snapshot/metrics), so an impersonator
> can pull inventory but cannot drive arbitrary commands. The proper fix (agent-generated nonce
> per connection, hub signs `nonce || token || fingerprint`, with version-skew handling for the
> agent auto-update window) is deferred to a dedicated branch. **Mitigation today:** never run
> agents with `HUB_TLS_INSECURE=true` outside development — it is the realistic path to capturing
> a replayable signature; always verify the hub TLS certificate (system trust or `HUB_CA_FILE`).

### Step 4: Hub Matches Or Creates Agent Record

The hub then:

- matches an existing `agents` record by fingerprint or empty first-connection fingerprint
- or creates a new record when a valid enrollment token is being used

### Step 5: Hub Pulls Initial Info

Once identity is established, the hub requests `GetAgentInfo` and persists:

- version
- capabilities (including `"docker": true/false` when the Docker collector is available, and `"agent_token": true` for agents that handle `SetAgentToken`)
- metadata

If the agent has the `agent_token` capability and its token is not one the hub issued for it (`token_issued` false) or may be shared (an enrollment token, or several records carry it), the hub then issues it a token of its own (`SetAgentToken`). A host that presented the fingerprint of a host with an issued token through the enrollment token stops here: it is **awaiting approval**, nothing is collected (see `docs/architecture/auth-and-data-model.md` → Agent Token).

### Step 5b: Hub Collects Initial Snapshot

Immediately after pulling agent info, the hub sends a `GetHostSnapshot` request with a 60-second timeout. The resulting snapshot is upserted into the `host_snapshots` collection via `upsertHostSnapshot()` in `internal/hub/snapshots.go`.

### Step 5c: Hub Collects Initial Metrics

After the initial snapshot, the hub sends a `GetHostMetrics` request with a short timeout and persists the result into:

- `host_metric_samples` for append-only chart history
- `host_metric_current` for the latest per-agent resource view used by the hosts overview UI

The hub also sends `GetContainerMetrics` with a short timeout and persists the result into `container_metric_samples` as append-only per-host polling snapshots. Each row stores the full set of running-container samples captured for that poll, which keeps the write path compact while still supporting multi-series container charts on the host detail page.

The live `*ws.WsConn` for each connected agent is tracked in `Hub.agentConns` (a `sync.Map` keyed by agent ID). It is stored on connect and deleted on disconnect so that on-demand snapshot refresh knows which agents are reachable.

### Step 6: Lifecycle Management

The hub starts periodic liveness checks by sending `Ping` requests through the WebSocket connection wrapper.

When the connection goes down, the hub eventually marks the agent offline.

## Connection State And Disconnection Behavior

The agent connection manager has two states:

- `Disconnected`
- `WebSocketConnected`

It retries connection attempts on a ticker while disconnected.

A connection that goes silent without closing (host powered off, kernel panic, network cut, a proxy that blackholes traffic) never produces a FIN or RST, and the hub's pings keep succeeding at the TCP level until the kernel gives up retransmitting — about 15 minutes on Linux. So the hub bounds silence instead. Each connection has a deadline (read and write, `defaultDeadline` = 70s in `ws.go`) that only moves forward, and only for two reasons (`RequestManager.receivedFrame` / `requestSent`):

- a frame arrives from the agent (`OnMessage`, `OnPong` — the agent answers every 30s ping, `agent/client.go` `OnPing`): deadline = now + 70s;
- the hub sends a request: the agent runs its handlers **on its read loop**, so during a slow snapshot (up to 45s) it sends nothing, not even pongs, yet it is alive. The deadline then covers the request's own deadline + 35s, capped at **140s after the agent was last heard from** — the cap keeps frequent polling (a short `METRICS_INTERVAL`, where a request is always in flight) from keeping a dead peer alive.

`WsConn.Ping` never touches the deadline. A peer that stops answering hits it 70–140s after its last frame (depending on the requests in flight), the read loop fails and the normal close path below runs. Verified with a `SIGSTOP`ped agent (its kernel keeps ACKing): connection closed after ~97s, offline ~35s later, back online on `SIGCONT`. Keep any agent handler well under 140s minus the ping interval: an agent stuck in a handler longer than that (e.g. a `statfs` on a hung NFS mount) is now closed and shown offline, and only reconnects once the handler returns — before, the hub's own pings kept such an agent shown as connected.

When a connection closes (`OnClose`), its `RequestManager` is closed at once: requests waiting for that agent fail immediately with `gws.ErrConnClosed` instead of running out their timeout (up to 60s for a snapshot), and new requests on it are refused.

On the hub side, `WsConn.DownChan` is triggered after a short delay in `internal/hub/ws/ws.go`. That delay allows reconnection before the hub flips the agent to offline too aggressively.

An additional **30-second grace period** (`agentOfflineGracePeriod`) is applied in `manageAgentLifecycle` after `DownChan` fires: the hub waits before writing `status=offline` and only does so if the agent has not already reconnected (checked via `agentConns.Load`). Combined with the `ws.go` delay, the total window before an offline status is committed is ~35 seconds. This prevents spurious offline notifications and status flaps caused by service restarts or binary upgrades.

Ping failures bypass the grace period and mark the agent offline immediately, since a failed ping indicates a genuinely dead connection rather than a planned restart — but only when the failing connection is still the agent's current one (`CompareAndDelete` succeeded). After an agent restart, the old connection's ticker can fire once the new connection is already stored; that failure is ignored. Both offline paths go through `markAgentOfflineUnlessReconnected`, which re-checks `agentConns` inside the write transaction (`setAgentStatusIf`), so a reconnection already registered is never overwritten. A reconnection still between its handshake (`UpdateAgent` writes `connected`) and its registration can be: `verifyWsConn` registers the connection with `registerAgentConn`, which stores it and then re-asserts `connected` if it is still the current one, repairing that interleaving (at the cost of an offline/online notification pair). A replaced connection whose ping fails is closed.

That lifecycle goroutine does not survive a hub restart, but the `connected` status in the database does. So `StartHub` also starts a one-shot reconciler (`startAgentStatusReconciler`, through `goAgent`): `agentBootReconcileDelay` (60s) after boot — time for live agents to reconnect, they retry every 5–10s — `reconcileAgentStatuses` marks `offline`, with the usual agent-offline notification, every agent still recorded as `connected` whose `last_seen` predates the boot and that has no entry in `agentConns`. Every handshake writes `last_seen` before storing the connection, so an agent seen since boot (mid-handshake, or inside its own grace period) is left alone; the condition is re-checked in the write transaction (`setAgentStatusIf`). That covers a host that died while the hub was down or during the grace period. The status is not reset at boot on purpose: that would flash every host offline in the UI and send a "back online" notification per agent on every hub restart. A hub that stops before the delay never runs it.

`last_seen` is written at every handshake and, since `setAgentStatusIf`, whenever an agent goes `offline`: for an offline host it is the time the hub lost it. The manual offline-host purge ("offline for more than N days") relies on that — before, a host connected for longer than N days became purgeable the moment it went offline.

The per-connection goroutines (`verifyWsConn`, `manageAgentLifecycle`) are tied to the hub's lifetime: they are started through `Hub.goAgent` (tracked by a `WaitGroup`), and their agent requests use contexts derived from `Hub.agentCtx` instead of `context.Background()`. `Hub.stopAgentConnections()` — called from `OnTerminate` and by the test hub cleanup — refuses new agent connections (`503`; a connection already upgraded when the shutdown starts is closed, because `goAgent` refuses to start its goroutine once stopping), cancels in-flight agent requests, closes the live connections (and the lifecycle goroutine closes any connection registered after that) and waits for those goroutines, so none of them writes to the database after it is closed. A hub shutdown never marks agents `offline` (the lifecycle goroutine returns without writing, including during the grace period). Panics: `verifyWsConn` and `manageAgentLifecycle` recover their own (stack logged) and drop their connection, so the agent reconnects with a fresh one instead of staying registered with no lifecycle goroutine; `goAgent` recovers anything else started through it, and the upgrader runs with `gws.Recovery`, so a panic in a gws callback (`OnMessage`, `OnClose`…) ends that connection's read loop rather than the hub. The snapshot and metrics fan-out goroutines are not covered by these. Start any new per-connection goroutine with `goAgent` (and handle its `false` return) and derive its contexts from `agentCtx`. `WsConn.conn` is an atomic pointer because the read loop clears it on close while other goroutines ping or close the connection. The hub's other background goroutines (snapshot and metrics tickers, notification dispatcher, monitor checks) are cancelled on terminate but not yet awaited.

This behavior matters when debugging brief network interruptions.

## Current Transport Truth

The active transport is WebSocket.

The repository still contains a transport abstraction under `internal/hub/transport/`, but the real implementation path in use is the WebSocket path.

Treat any wording that suggests SSH fallback as legacy or inconsistent wording unless the code path clearly proves otherwise. In the current codebase:

- hub identity verification uses SSH keys over WebSocket
- the agent connection transport itself is WebSocket

## How To Add A New WebSocket Action

When adding a new hub-to-agent action:

1. append the new action constant in `internal/common/common-ws.go`
2. add any request or response payload types there if they are shared protocol types
3. implement a handler in `agent/handlers.go`
4. register the handler in `NewHandlerRegistry()`
5. add the hub-side method on `*ws.WsConn` in `internal/hub/ws/handlers.go`
6. use an explicit timeout from the hub when invoking it
7. add or update tests on both sides where appropriate

Never reorder existing action constants.

## High-Signal Files For Architecture Work

- `internal/common/common-ws.go`
- `internal/hub/agent_connect.go`
- `internal/hub/ws/handlers.go`
- `internal/hub/ws/ws.go`
- `internal/hub/ws/request_manager.go`
- `internal/hub/snapshots.go`
- `internal/hub/host_metrics.go`
- `internal/hub/dashboard.go`
- `agent/client.go`
- `agent/connection_manager.go`
- `agent/handlers.go`
- `agent/collectors/`
