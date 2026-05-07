# Hub-Agent Architecture

## Runtime Shape

Nexus is built around two long-running components:

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
3. hub public keys are loaded from `KEY`, `KEY_FILE`, or the `--key` flag
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

- the agent marks `hubVerified = true`
- the agent responds with its stable fingerprint

### Step 4: Hub Matches Or Creates Agent Record

The hub then:

- matches an existing `agents` record by fingerprint or empty first-connection fingerprint
- or creates a new record when a valid enrollment token is being used

### Step 5: Hub Pulls Initial Info

Once identity is established, the hub requests `GetAgentInfo` and persists:

- version
- capabilities (including `"docker": true/false` when the Docker collector is available)
- metadata

### Step 5b: Hub Collects Initial Snapshot

Immediately after pulling agent info, the hub sends a `GetHostSnapshot` request with a 60-second timeout. The resulting snapshot is upserted into the `host_snapshots` collection via `upsertHostSnapshot()` in `internal/hub/snapshots.go`.

### Step 5c: Hub Collects Initial Metrics

After the initial snapshot, the hub sends a `GetHostMetrics` request with a short timeout and persists the result into:

- `host_metric_samples` for append-only chart history
- `host_metric_current` for the latest per-agent resource view used by the hosts overview UI

The live `*ws.WsConn` for each connected agent is tracked in `Hub.agentConns` (a `sync.Map` keyed by agent ID). It is stored on connect and deleted on disconnect so that on-demand snapshot refresh knows which agents are reachable.

### Step 6: Lifecycle Management

The hub starts periodic liveness checks by sending `Ping` requests through the WebSocket connection wrapper.

When the connection goes down, the hub eventually marks the agent offline.

## Connection State And Disconnection Behavior

The agent connection manager has two states:

- `Disconnected`
- `WebSocketConnected`

It retries connection attempts on a ticker while disconnected.

On the hub side, `WsConn.DownChan` is triggered after a short delay in `internal/hub/ws/ws.go`. That delay allows reconnection before the hub flips the agent to offline too aggressively.

An additional **30-second grace period** (`agentOfflineGracePeriod`) is applied in `manageAgentLifecycle` after `DownChan` fires: the hub waits before writing `status=offline` and only does so if the agent has not already reconnected (checked via `agentConns.Load`). Combined with the `ws.go` delay, the total window before an offline status is committed is ~35 seconds. This prevents spurious offline notifications and status flaps caused by service restarts or binary upgrades.

Ping failures bypass the grace period and mark the agent offline immediately, since a failed ping indicates a genuinely dead connection rather than a planned restart.

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
