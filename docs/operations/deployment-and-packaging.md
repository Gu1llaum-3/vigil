# Deployment And Packaging

## Why This Document Exists

Vigil already includes several operational assets, but they are spread across `supplemental/`, `.goreleaser.yml`, and runtime update code.

This document explains what is available, what each asset is for, and how the pieces relate to the actual runtime.

## Operational Asset Map

The main operational assets are:

- `supplemental/docker/` (hub only)
- `supplemental/guides/systemd.md`
- `supplemental/scripts/`
- `supplemental/debian/`
- `.goreleaser.yml`
- `internal/hub/update.go`
- `internal/ghupdate/*`

Not all of these are equally production-opinionated. Some are examples and starting points rather than a single blessed deployment path.

## Deployment Model

The supported deployment model is intentionally asymmetric:

- **Hub** — runs as a container (primary path, see the hub Compose below) or as a native service installed via `install-hub.sh`. Docker is the recommended way to run the hub, but not the only one.
- **Agents** — installed **natively** on each monitored host (shell / PowerShell / Debian package / Homebrew). There is no agent container image; the agent is a lightweight process that integrates with the host's service manager.
- **Kubernetes** — not supported. There is no Helm chart.

## Docker Compose Assets

The repository includes multiple Docker Compose examples.

### Hub Compose

Path:

- `supplemental/docker/hub/docker-compose.yml`

Purpose:

- run the hub as a containerized service
- persist the PocketBase data directory via a volume
- expose port `8090`

Typical use case:

- local evaluation
- simple single-service deployment

### Hub Dev Compose

Path:

- `supplemental/docker/hub/docker-compose.dev.yml`

Purpose:

- build the hub image locally from the repository
- run the built image with a persistent data volume

Typical use case:

- local Docker-based verification of the hub and web UI

### Release Workflows

Paths:

- `.github/workflows/release.yml`
- `.github/workflows/docker-images.yml`

Purpose:

- publish Go release artifacts for tagged versions
- build and push the hub Docker image to GHCR

Release behavior:

- stable tags like `v1.2.3` are published as normal releases
- tags containing a hyphen such as `v1.2.3-beta.1` or `v1.2.3-dev.1` are treated as prereleases
- GitHub prereleases stay separate and are never promoted to `latest`
- rerunning the same tag replaces existing release artifacts instead of failing on duplicate asset names

> **Note:** there is intentionally no agent Compose or combined hub+agent Compose. Agents are installed natively (see *Service Management And Install Scripts*).

### Hub Dockerfile

Path:

- `internal/dockerfile_hub`

Purpose:

- build the frontend bundle and hub binary in a multi-stage Docker build
- produce a container image that serves the embedded web UI and PocketBase runtime
- install the system `ping` binary used by the hub `ping` monitor type
- pin the Go builder image to the patched Go toolchain version required by `go.mod`

Operational note:

- the image runs as a non-root user (uid `10001`); the data dir `/vigil_data` is owned by that uid, so a host bind mount must be chowned to `10001:10001` (the shipped Compose uses a named volume to avoid this)
- it defines a `HEALTHCHECK` against PocketBase's `GET /api/health`
- the image includes `iputils` so the `ping` monitor works in the official hub container; `CAP_NET_RAW` is granted to the `ping` binary via `setcap` so it works under the non-root user (Docker's default capability set includes `NET_RAW`)
- ICMP still depends on the runtime environment allowing echo requests; network policy or capability restrictions can block it even when `ping` is installed
- hardened container or cluster policies can still block ICMP echo at runtime even when the binary is present

## Service Management And Install Scripts

### systemd Guide

Path:

- `supplemental/guides/systemd.md`

Purpose:

- document systemd unit setup and service-oriented deployment

Use this when:

- deploying to a Linux host without Docker
- managing the hub or agent as a normal service

### Install Scripts

Paths:

- `supplemental/scripts/install-hub.sh`
- `supplemental/scripts/install-agent.sh`

Purpose:

- install binaries
- create service-oriented runtime structure
- provide OS-specific service integration details

Integrity and secret handling:

- both scripts download the release archive named `vigil_${OS}_${ARCH}.tar.gz` and verify its SHA-256 against the published `vigil_${VERSION}_checksums.txt` before installing; a mismatch aborts the install
- `install-hub.sh` installs the `vigil` binary and a `vigil` system user; `install-agent.sh` installs the `vigil-agent` binary
- secrets (`KEY`/`TOKEN`/`HUB_URL`) are never inlined into generated service definitions — systemd uses an `EnvironmentFile=`, and the OpenRC/procd/FreeBSD paths source a root-only, shell-escaped env file (see `docs/conventions-and-gotchas.md`)

Important note:

- `supplemental/scripts/install-agent.sh` is aligned to the agent release artifacts published by `.goreleaser.yml`, which at the moment means Linux only: `amd64`, `arm64`, and `arm` (`armv7`)
- the agent install script does not currently configure a working self-update flow; treat `--auto-update` as a compatibility placeholder rather than a supported feature

### FreeBSD-Specific Service Support

Some supplemental assets still include FreeBSD-oriented service examples, but the shipped agent install script should be treated as Linux-only until the agent release matrix expands.

Treat those non-Linux paths as templates rather than verified install flows.

## Release Packaging

Release packaging is defined in `.goreleaser.yml`.

This file controls how the project is built and distributed across targets.

Typical concerns to review here:

- binary naming
- archive naming
- target OS and architecture matrix
- packaged supporting files

Integrity:

- `make_latest: true` so GitHub's `/releases/latest` resolves for stable tags (the install scripts depend on it); prereleases stay excluded via `prerelease: auto`
- the `checksums.txt` file is signed with cosign keyless (OIDC) by the release workflow, producing `*.sig` and `*.pem`; the `signs:` block documents the `cosign verify-blob` recipe
- the hub image build publishes provenance and an SBOM

If you rename the project or change binary names, this file must stay in sync with `app.go` and any install scripts.

## Hub Self-Update Flow

The hub update command is implemented in:

- `internal/hub/update.go`
- `internal/ghupdate/*`

The self-update flow does the following:

1. fetch latest release metadata
2. resolve the matching archive for the current OS and architecture
3. download the archive
4. extract the new executable
5. replace the running executable on disk
6. try to restart the service automatically when possible

Supported service restart attempts include:

- systemd
- OpenRC

If restart cannot be handled automatically, the user is asked to restart manually.

## Heartbeat Monitoring

Heartbeat support lives in `internal/hub/heartbeat/heartbeat.go`.

Purpose:

- send periodic outbound pings to an external monitoring endpoint

The heartbeat goroutine is started from `StartHub()` (in `internal/hub/hub.go`) on
serve, and only when `HEARTBEAT_URL` is set (`heartbeat.New` returns nil otherwise);
it stops with the app via the server context.

Important env variables:

- `HEARTBEAT_URL` — the push endpoint to ping
- `HEARTBEAT_INTERVAL` — seconds between pings (default 60)
- `HEARTBEAT_METHOD` — `POST` (default) or `GET`

Operational use case:

- external uptime monitoring for the hub process via a push monitor, e.g. an
  Uptime Kuma "Push" monitor (set `HEARTBEAT_URL` to the push URL and
  `HEARTBEAT_METHOD=GET`), Healthchecks.io, or BetterStack. If the hub stops, the
  pings stop and the external monitor alerts.

## Storage And Persistence Considerations

### Hub

The hub stores its PocketBase data in the app data directory.

Operational implications:

- use a persistent volume for containerized deployment
- include the data dir in backup strategy
- remember that the hub SSH keypair also lives in the data dir

### Agent

The agent stores its fingerprint in its own data directory.

Operational implications:

- wiping the data dir changes the stable agent identity
- token and fingerprint persistence should be considered separately when troubleshooting

## Examples Versus Supported Paths

Treat the assets in `supplemental/` as supported examples, not as a single canonical deployment standard.

That means:

- the hub Docker Compose file is a useful starting point
- install scripts encode useful defaults and service structure
- derived projects should review naming, security, storage, and secret handling before production rollout

## Recommended Deployment Review Checklist

Before using a deployment asset in a real environment, review:

1. public app URL and base URL assumptions
2. persistent storage for hub data
3. agent fingerprint persistence needs
4. hub public-key distribution to agents
5. binary and service names for derived products
6. environment variable naming and prefixes
7. reverse proxy, ingress, and TLS expectations

## High-Signal Files For Ops Work

- `.goreleaser.yml`
- `supplemental/docker/hub/docker-compose.yml`
- `supplemental/guides/systemd.md`
- `supplemental/scripts/install-hub.sh`
- `supplemental/scripts/install-agent.sh`
- `supplemental/debian/`
- `internal/hub/update.go`
- `internal/ghupdate/*`
- `internal/hub/heartbeat/heartbeat.go`
