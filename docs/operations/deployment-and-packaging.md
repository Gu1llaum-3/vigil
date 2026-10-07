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
- **Agents** — installed **natively** on each monitored host, which must be **Linux** (`amd64`, `arm64`, `armv7`): with `install-agent.sh` or the Debian package. There is no agent container image; the agent is a lightweight process that integrates with the host's service manager. No Windows or macOS agent is built (the collectors are Linux-only), so there are no PowerShell, scoop, winget, or Homebrew installers.
  - Note: those installers were removed because they pointed at channels that were never published, and an unclaimed winget ID could have been registered by a third party. Bringing them back requires real builds, collectors, and channels whose namespaces the project owns.
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

Path: `.github/workflows/release.yml`, triggered by a version tag. Its jobs run in sequence:

1. `ci` — calls `.github/workflows/ci.yml` (Go vet + tests on Linux, the same tests under the race detector, frontend checks) on the tagged commit
2. `goreleaser` — `needs: ci`; publishes the Go binaries, archives, `.deb` packages and signed checksums
3. `docker` — `needs: goreleaser`; builds and pushes the hub image to GHCR

So a tag whose commit fails CI publishes nothing, and an image is never pushed without the matching release (or the reverse, as could happen when the image had its own workflow). The workflow is read-only by default; `goreleaser` gets `contents: write` and `id-token: write` (cosign), `docker` gets `packages: write`.

Release behavior:

- stable tags like `v1.2.3` are published as normal releases
- tags containing a hyphen such as `v1.2.3-beta.1` or `v1.2.3-dev.1` are treated as prereleases
- GitHub prereleases stay separate and are never promoted to `latest`
- rerunning the same tag replaces existing release artifacts instead of failing on duplicate asset names
- the `docker` job does **not** build the frontend on the runner — the Dockerfile's `web-builder` stage does (and `internal/site/dist` is in `.dockerignore`); it uses the GitHub Actions BuildKit cache (`type=gha`) across the three platforms (`linux/amd64`, `linux/arm64`, `linux/arm/v7`). A cache written on a tag is only readable by that same tag (and nothing builds the image on `main`), so it mostly speeds up re-runs of one release; each new tag builds cold
- `release.yml` installs Go from `go-version-file: go.mod`, so the `go` directive in `go.mod` **is** the toolchain used for release binaries — bump it to pick up Go security patch releases

> **Note:** there is intentionally no agent Compose or combined hub+agent Compose. Agents are installed natively (see *Service Management And Install Scripts*).

### Hub Dockerfile

Path:

- `internal/dockerfile_hub`

Purpose:

- build the frontend bundle and hub binary in a multi-stage Docker build
- produce a container image that serves the embedded web UI and PocketBase runtime
- install the system `ping` binary used by the hub `ping` monitor type
- pin the Go builder image to the patched Go toolchain version required by `go.mod`
- stamp the release version into the binary: the `VERSION` build arg (passed by the release workflow's `docker` job as the git tag) is injected with the same `-X github.com/Gu1llaum-3/vigil.Version=…` flag as `.goreleaser.yml`, leading `v` stripped. Without it the binary keeps the in-source default (`0.0.0-dev`), which shows a dev version in the UI / `GET /api/app/info`, makes `CHECK_UPDATES` always report an update, and makes the *Add agent* install command fall back to `main` instead of pinning a release — so a local `docker build` reports `0.0.0-dev` unless you pass `--build-arg VERSION=vX.Y.Z`
- all three base images (`node`, `golang`, `alpine`) are pinned by tag **and** digest; Dependabot's `docker` ecosystem (`.github/dependabot.yml`, directory `/internal`) bumps both

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
- the checksums file is **always** fetched from the canonical `github.com` host, even when `--mirror` routes the (larger) binary through a proxy. This prevents a malicious mirror from serving a backdoored binary together with a matching checksum — the integrity reference never comes from the same untrusted host as the artifact. If `github.com` is fully unreachable the install aborts with guidance unless the operator passes `--insecure-mirror`, which trusts the mirror's checksum and prints a loud reduced-integrity warning. (Advanced users can additionally verify the cosign signature on `checksums.txt` out-of-band — see below.)
- `install-hub.sh` installs the `vigil` binary and a `vigil` system user; `install-agent.sh` installs the `vigil-agent` binary, owned by root, and runs it as a dedicated `vigil-agent` system user under a sandboxed systemd unit (same directives as the `.deb` unit). Docker socket access (container inventory) is opt-in with `--docker` and revoked with `--no-docker`, because docker group membership is root-equivalent; the *Add agent* dialog offers the default command and a `--docker` one. Upgrades from installs that used the legacy `app` user are migrated in place (see `docs/conventions-and-gotchas.md`). The `.deb` uses the same `vigil-agent` user, with `/etc/vigil-agent.conf` root `0600`; its Docker access follows the `vigil-agent/docker_access` debconf answer (`dpkg-reconfigure vigil-agent` grants or revokes it and restarts the agent once `HUB_URL` and `KEY` are configured), and upgrades from packages that ran the agent as `vigil` are migrated in place
- secrets (`KEY`/`TOKEN`/`HUB_URL`) are never inlined into generated service definitions — systemd uses an `EnvironmentFile=`, and the OpenRC/procd/FreeBSD paths source a root-only, shell-escaped env file (see `docs/conventions-and-gotchas.md`)

Version selection:

- without `-v`, both scripts install GitHub's latest **stable** release (`/releases/latest`) and abort with guidance if there is none; prereleases (every `-beta` tag) are only installed when passed explicitly with `-v`
- the *Add agent* dialog's install command is **pinned to the hub's own version**: it downloads `install-agent.sh` from the `v<hub version>` tag (not `main`) and passes `-v v<hub version>` (with `curl -f`, so a missing tag aborts instead of running GitHub's 404 page), so a new agent always matches the hub, even when `/releases/latest` lags behind (it pointed at v0.1.1 during the 0.2.x betas). The command is built by `internal/site/src/lib/agent-install.ts`
- an unstamped dev hub reports `0.0.0-dev` (the in-source default in `app.go`), which matches no tag; its install command falls back to the script on `main` and the script's default version

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
- the `checksums.txt` file is signed with cosign v3 keyless (OIDC) by the release workflow, producing a single Sigstore bundle `*_checksums.txt.sigstore.json` (releases up to v0.2.14-beta shipped separate `*.sig` + `*.pem` instead); the `signs:` block documents the `cosign verify-blob --bundle` recipe. Cosign v3 signs through a signing config that only writes the bundle, so the old `--output-signature`/`--output-certificate` args would make the release fail — keep `--bundle`
- the hub image build publishes provenance and an SBOM
- goreleaser's `before` hook no longer runs `go mod tidy`, so the release never rewrites `go.mod`/`go.sum`: it builds exactly the committed files, whose tidiness CI checks (`go mod tidy -diff`). The hook runs `go mod verify` instead, a cheap check (the go command verifies every downloaded module against `go.sum` anyway)

Licensing:

- `LICENSE` carries the MIT notices of Vigil, Beszel (the project is derived from it) and PocketBase (`internal/ghupdate` is derived from its `ghupdate` package). All three use the identical MIT text, so one permission notice covers them
- every artifact ships them: goreleaser archives include `LICENSE*` through goreleaser's default `files` (do not add an `archives[].files` override without listing `LICENSE`), the `.deb` installs the DEP-5 `supplemental/debian/copyright` (full license text, with a `Files: internal/ghupdate/*` stanza), and the hub image copies `LICENSE` to `/usr/share/licenses/vigil/LICENSE`
- the install scripts and the self-updater only extract the binary from the archive; the notices ship in the downloaded archive itself
- `license_test.go` (repo root) fails if a notice disappears from `LICENSE` or the DEP-5 file, if the Dockerfile stops copying `LICENSE` or `.dockerignore` excludes it, or if `.goreleaser.yml` drops the copyright file from the `.deb` or overrides the archive files without `LICENSE`

If you rename the project or change binary names, this file must stay in sync with `app.go` and any install scripts.

## Hub Self-Update Flow

The hub update command is implemented in:

- `internal/hub/update.go`
- `internal/ghupdate/*`

The self-update flow does the following:

1. fetch latest release metadata
2. resolve the matching archive for the current OS and architecture (`<binary>_<os>_<arch>.tar.gz`, `.zip` on Windows — the names `.goreleaser.yml` publishes, see `archiveSuffix`)
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
