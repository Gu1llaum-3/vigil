# Deployment And Packaging

## Why This Document Exists

Nexus already includes several operational assets, but they are spread across `supplemental/`, `.goreleaser.yml`, and runtime update code.

This document explains what is available, what each asset is for, and how the pieces relate to the actual runtime.

## Operational Asset Map

The main operational assets are:

- `supplemental/docker/`
- `supplemental/guides/systemd.md`
- `supplemental/scripts/`
- `supplemental/kubernetes/`
- `.goreleaser.yml`
- `internal/hub/update.go`
- `internal/ghupdate/*`

Not all of these are equally production-opinionated. Some are examples and starting points rather than a single blessed deployment path.

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

### Agent Compose

Path:

- `supplemental/docker/agent/docker-compose.yml`

Purpose:

- run the agent in a container with environment-driven configuration
- connect back to an existing hub

Typical use case:

- remote host deployment where Docker is preferred

### Same-System Compose

Path:

- `supplemental/docker/same-system/docker-compose.yml`

Purpose:

- run hub and agent on the same machine for local development or demos

Typical use case:

- quick smoke testing of the full architecture
- demos or evaluation setups

### Hub Dockerfile

Path:

- `internal/dockerfile_hub`

Purpose:

- build the frontend bundle and hub binary in a multi-stage Docker build
- produce a container image that serves the embedded web UI and PocketBase runtime

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

Important note:

- these scripts still use generic boilerplate naming and should be reviewed carefully in derived projects
- `supplemental/scripts/install-agent.sh` is currently aligned to the agent release artifacts published by `.goreleaser.yml`, which at the moment means Linux only: `amd64`, `arm64`, and `arm` (`armv7`)
- the agent install script does not currently configure a working self-update flow; treat `--auto-update` as a compatibility placeholder rather than a supported feature

### FreeBSD-Specific Service Support

Some supplemental assets still include FreeBSD-oriented service examples, but the shipped agent install script should be treated as Linux-only until the agent release matrix expands.

Treat those non-Linux paths as templates rather than verified install flows.

## Kubernetes Assets

Path:

- `supplemental/kubernetes/`

The current chart and values are a starting point for running the hub in Kubernetes.

Highlights from the provided values:

- configurable image repository and tag
- service and ingress options
- PVC support for hub persistence
- resource and autoscaling settings

Current limitation:

- these assets are generic and require environment-specific hardening and naming before production use

## Release Packaging

Release packaging is defined in `.goreleaser.yml`.

This file controls how the project is built and distributed across targets.

Typical concerns to review here:

- binary naming
- archive naming
- target OS and architecture matrix
- packaged supporting files

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

Important env variables:

- `HEARTBEAT_URL`
- `HEARTBEAT_INTERVAL`
- `HEARTBEAT_METHOD`

Operational use case:

- external uptime monitoring for the hub process

There is also a test-heartbeat path in the hub API, which is useful when validating configuration.

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

- Docker Compose files are useful starting points
- install scripts encode useful defaults and service structure
- Kubernetes assets are a generic base, not a final production chart
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
- `supplemental/docker/agent/docker-compose.yml`
- `supplemental/docker/same-system/docker-compose.yml`
- `supplemental/guides/systemd.md`
- `supplemental/scripts/install-hub.sh`
- `supplemental/scripts/install-agent.sh`
- `supplemental/kubernetes/**/*`
- `internal/hub/update.go`
- `internal/ghupdate/*`
- `internal/hub/heartbeat/heartbeat.go`
