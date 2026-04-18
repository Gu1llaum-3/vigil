# Vigil

Vigil is a hub/agent patch audit application built on [PocketBase](https://pocketbase.io/), with a WebSocket architecture, React frontend, and multi-language support.

It provides centralized monitoring and reporting of system patches, package updates, repositories, and compliance status across distributed infrastructure.

This project is based on [Beszel](https://github.com/henrygd/beszel) and draws inspiration from [Uptime Kuma](https://github.com/louislam/uptime-kuma) and [Patchmon](https://github.com/PatchMon/PatchMon).

## Architecture

The project consists of two main components: the **hub** and the **agent**.

- **Hub**: A web application built on [PocketBase](https://pocketbase.io/) that provides authentication, real-time data sync, and a React-based UI for monitoring patch status.
- **Agent**: Lightweight processes deployed on remote systems that connect outbound to the hub over secure WebSocket (SSH key authentication) and respond to requests.

## Features

- **PocketBase backend** — SQLite, collections, realtime subscriptions, OAuth2, MFA/OTP, automatic backups.
- **Secure WebSocket transport** — Ed25519 key fingerprinting, CBOR binary protocol, universal token self-registration.
- **React frontend** — nanostores, Tailwind CSS, shadcn/ui, i18n via Lingui (20+ languages).
- **Multi-user** — role-based access (admin / readonly). Users manage their own settings.
- **OAuth / OIDC** — supports many OAuth2 providers. Password auth can be disabled.
- **Patch audit** — collect and visualize package updates, security patches, repositories, and reboot requirements.

## Getting started

```bash
# Install dependencies and build the web UI
make build-web-ui

# Build hub and agent binaries
make build

# Or run in development mode
make dev
```

## Environment variables

### Hub

| Variable | Description |
|---|---|
| `VIGIL_HUB_*` / env name | See hub configuration |

### Agent

| Variable | Description |
|---|---|
| `VIGIL_AGENT_HUB_URL` | URL of the hub |
| `VIGIL_AGENT_TOKEN` | Authentication token |
| `VIGIL_AGENT_TOKEN_FILE` | Path to a file containing the token |

## Project structure

```
.
├── agent/                    # Agent binary — data collection and hub communication
│   ├── deltatracker/         # Incremental delta calculations for metrics
│   ├── health/               # Agent health checks
│   ├── tools/                # Build-time tools (e.g. smartctl fetcher)
│   └── utils/                # Shared agent utilities
├── internal/
│   ├── cmd/
│   │   ├── agent/            # Agent binary entrypoint
│   │   └── hub/              # Hub binary entrypoint
│   ├── common/               # Shared SSH/WebSocket protocol types
│   ├── ghupdate/             # GitHub releases update checker
│   ├── hub/                  # Hub core logic (API, collections, server, agent connection)
│   │   ├── expirymap/        # TTL map for ephemeral tokens
│   │   ├── heartbeat/        # Outbound heartbeat ping service
│   │   ├── transport/        # WebSocket transport abstraction
│   │   ├── utils/            # Hub environment variable helpers
│   │   └── ws/               # WebSocket connection management (CBOR, fingerprinting)
│   ├── migrations/           # PocketBase database migrations
│   ├── site/                 # Frontend application (Vite + React)
│   │   ├── public/           # Static assets (icons, manifest)
│   │   └── src/
│   │       ├── components/   # React components (login, routes, UI primitives)
│   │       └── lib/          # Shared utilities, nanostores, API client
│   ├── tests/                # Shared Go test helpers
│   └── users/                # User collection management
└── supplemental/             # Deployment helpers and examples
    ├── debian/               # Debian packaging scripts and systemd service unit
    ├── docker/               # Docker Compose examples (hub, agent, same-system)
    ├── guides/               # Installation guides (e.g. systemd)
    ├── kubernetes/           # Helm chart for hub deployment
    ├── licenses/             # Third-party license files
    └── scripts/              # Shell/PowerShell install scripts
```

## License

MIT
