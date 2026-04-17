# Project Overview

## What Nexus Is

Nexus is a reusable application boilerplate for building web applications with a central hub and lightweight remote agents.

It is not a one-off product with domain-specific business logic. The repository is structured so a derived project can rename, brand, and extend it without rewriting the core architecture.

The current implementation combines:

- a PocketBase-based hub for data, auth, custom API routes, and web serving
- an outbound agent process that connects back to the hub over WebSocket
- a React/Vite frontend embedded into the hub in production and proxied in development
- install, packaging, and deployment helpers under `supplemental/`

## Core Mental Model

Nexus has two primary runtime components.

### Hub

The hub is the central application server. It owns:

- the PocketBase database and auth model
- custom API routes under `/api/app/*`
- the WebSocket endpoint used by agents
- the frontend entrypoint and static asset serving
- enrollment-token management and agent lifecycle tracking

### Agent

The agent is a lightweight process that runs on another machine and connects outbound to the hub. It owns:

- remote identity via a stable fingerprint
- token-based authentication to the hub
- cryptographic verification of hub identity using the hub public key
- a handler registry for hub-initiated actions

The direction of control matters: the hub pulls information from agents. Agents do not proactively push data.

## Why The Boilerplate Stays Generic

Rename-sensitive metadata is centralized in `app.go`:

- `DisplayName`
- `AppName`
- `HubBinary`
- `AgentBinary`
- `HubEnvPrefix`
- `AgentEnvPrefix`
- default data directory names
- default release owner and repo values

This makes it possible to derive a real product from Nexus without hunting through the whole repository for every user-facing and technical identifier.

The same design choice appears elsewhere:

- the Go module path is still placeholder-oriented in `go.mod`
- frontend package metadata is generic
- Docker, systemd, packaging, and Helm assets under `supplemental/` use generic names

## Current Implementation Scope

The repository already includes real working code, but it is intentionally narrow in user-facing scope.

### Backend

The backend already implements:

- PocketBase startup and migrations
- custom app routes in `internal/hub/api.go`
- collection auth configuration in `internal/hub/collections.go`
- hub key generation and public-key exposure
- agent enrollment token management
- agent connection verification and lifecycle tracking

### Agent

The agent already implements:

- CLI startup and utility commands
- environment-variable based configuration
- data directory discovery
- fingerprint persistence
- WebSocket connection management
- built-in handlers for hub verification, liveness, and agent info

### Frontend

The frontend is intentionally small but real. It currently includes:

- login and first-run account creation flows
- OAuth, OTP, and MFA-related handling via PocketBase auth methods
- a home route
- a settings area
- agent enrollment token and agent record management UI
- localization, theme switching, and user settings handling

The frontend is not yet a broad product surface. It is a shell that demonstrates how derived projects can integrate UI, auth, and settings on top of the hub.

### Operations

The repository also includes:

- Docker Compose examples
- Linux and FreeBSD install scripts
- Debian packaging assets
- a Helm chart
- release packaging configuration in `.goreleaser.yml`

## Documentation Roles

Different top-level files serve different purposes.

### `README.md`

Use `README.md` as the short, public-facing overview of the boilerplate.

### `AGENTS.md`

Use `AGENTS.md` as high-signal instructions for coding agents and contributors who need architecture and conventions quickly.

### `docs/`

Use `docs/` as the maintainable, task-oriented knowledge base for the project itself. This is where detailed implementation guidance should live.

## How To Read This Repository

If you are new to the codebase, read it in this order:

1. `app.go`
2. `internal/cmd/hub/hub.go`
3. `internal/hub/hub.go`
4. `internal/hub/agent_connect.go`
5. `internal/common/common-ws.go`
6. `agent/agent.go`
7. `agent/client.go`
8. `internal/site/src/main.tsx`

That path gives you the central metadata, hub startup, agent connection model, shared protocol, agent runtime, and frontend entrypoint in a logical order.
