# Workflow And Testing

## Tooling

### Required

- Go 1.27.1 or newer (PocketBase ≥ 0.40 requires Go 1.27; see `docs/conventions-and-gotchas.md` → JSON v2)
- Node.js 24.x and pnpm (version pinned by `packageManager` in `internal/site/package.json`; `mise install` provides it)

### Optional But Useful

- `mise` for pinned Go/Node/lefthook toolchains
- `lefthook` for git hooks
- `entr` for hot-reload during development
- `golangci-lint` v2 for Go linting (`make lint`; configuration in `.golangci.yml`)

The repository pins its local toolchain in `.mise.toml`.

After cloning, run `mise install` to match the repo versions.

If you want the Git hooks enabled, run `lefthook install` once.

The hooks (`lefthook.yml`) are a local convenience; CI runs the same checks and more, so they are optional:

- pre-commit: `gofmt -w` on the staged Go files, which are then re-staged so the commit holds the formatted version (`stage_fixed`). With a partially staged file, lefthook sets the unstaged changes aside and restores them afterwards; if they touch lines gofmt reformatted, the restore fails ("Couldn't restore unstaged files") and they are left in the `lefthook auto backup` stash, and Biome (`pnpm check`) when frontend files are staged
- pre-push: the Go tests (creating the empty `internal/site/dist/index.html` the hub needs to compile, like CI) and the frontend type check (`pnpm typecheck`) when frontend files are pushed

Note: `lefthook run <hook>` (e.g. to try a hook by hand) installs the hooks into `.git/hooks` if they are missing; `lefthook uninstall` removes them again.

The `CI` workflow (`.github/workflows/ci.yml`, on pushes to `main` and `dev` and on pull requests, including Dependabot's) has four jobs:

- `go` (ubuntu-latest): `go mod verify`, `go mod tidy -diff` (fails when `go.mod`/`go.sum` are not tidy), `go vet -tags=testing ./...` and `go test -tags=testing -timeout 20m ./...`. It runs on Linux, so the `//go:build linux` collector tests run too, and creates an empty `internal/site/dist/index.html` first because the hub embeds that directory.
- `lint`: `gofmt` (fails on any unformatted tracked `.go` file), `golangci-lint` (`.golangci.yml`: the standard linters — errcheck, govet, ineffassign, staticcheck, unused — on the `testing` build tag) and `govulncheck` (fails only on vulnerabilities the code reaches). `make lint` runs the same three checks
- `race`: the same suite under the race detector (`go test -tags=testing -race -timeout 20m ./...`), in parallel with `go`
- `frontend`: Biome, `pnpm audit --prod --audit-level high` (dependencies shipped in the bundle; build-only tools such as `@lingui/cli` are not audited), the type check, the frontend build, and a run of `supplemental/scripts/third-party-notices.sh` (the release generates `THIRD_PARTY_NOTICES` with it; it also fails on a Go dependency with a forbidden, restricted or unknown license). `make notices` runs it locally

`ci.yml` is also a reusable workflow (`workflow_call`): the release workflow runs it on the tagged commit before publishing anything (see `docs/operations/deployment-and-packaging.md`). To reproduce the `go` job locally, run the same commands in a `golang` container on a clean copy (`git archive HEAD`), as a non-root user like the GitHub runner (and as root too: Docker builds run tests as root).

## Main Make Targets

The main workflow is driven by `Makefile`.

### Build Targets

- `make build`
  - builds both the agent and the hub
- `make build-agent`
  - builds only the agent binary
- `make build-hub`
  - builds the frontend and then builds the hub binary
- `make build-hub-dev`
  - builds the hub with the `development` build tag so the frontend is served from the Vite dev server
- `make build-web-ui`
  - installs frontend dependencies and builds the frontend bundle only

Output binaries are written under `build/`.

### Development Targets

- `make dev-server`
  - runs the Vite frontend dev server
- `make dev-hub`
  - runs the hub in development mode and proxies frontend requests to Vite
- `make dev-agent`
  - runs the agent in a development loop; export `HUB_URL`, `TOKEN` and `KEY` first (copied from the hub's *Add agent* dialog), otherwise the agent exits at startup
- `make dev`
  - runs frontend, hub, and agent development processes together

### Maintenance Targets

- `make test`
  - runs Go tests with the `testing` build tag
- `make tidy`
  - runs `go mod tidy`
- `make lint`
  - runs `gofmt -l`, `golangci-lint` and `govulncheck`, like the CI `lint` job
- `make clean`
  - cleans Go build output and removes `build/`

## Frontend Commands

Frontend commands live in `internal/site/package.json`.

Run them from the repository root via the `Makefile`, or directly in `internal/site`.

This repository standardizes on **pnpm** for frontend dependency management and script execution. The lockfile is `internal/site/pnpm-lock.yaml`; do not use `npm install` (it would create a `package-lock.json`, which is git-ignored). The pnpm version is pinned in one place, the `packageManager` field of `internal/site/package.json`: CI (`pnpm/action-setup`) and the Docker `web-builder` stage read it, and `.mise.toml` mirrors it for local use — bump both together.

pnpm settings live in `internal/site/pnpm-workspace.yaml`:

- `allowBuilds` — pnpm runs no dependency install scripts unless allowed. The only one requested, `@swc/core`'s postinstall, is denied on purpose: it is a fallback that installs `@swc/wasm` when the native binary (shipped as a per-platform optional dependency) cannot load. When a new dependency asks for a build script, pnpm fails the install with `ERR_PNPM_IGNORED_BUILDS`; decide with `pnpm approve-builds <pkg>` or `pnpm approve-builds '!<pkg>'` and commit the result.
- pnpm's default `minimumReleaseAge` (1 day) is kept: a version published less than 24h ago is not picked, the previous matching version is used instead. This guards against freshly published compromised releases; an update may therefore lag a release by a day.

Important commands:

- `pnpm dev`
- `pnpm build`
- `pnpm sync`
- `pnpm sync_and_purge`
- `pnpm lint`
- `pnpm check`
- `pnpm check:fix`
- `pnpm typecheck`

The build command compiles Lingui catalogs and runs Vite. It does **not** re-extract messages from sources — extraction would rewrite `.po` files, which is fine in dev but breaks CI release tooling that asserts a clean working tree (e.g. goreleaser). Run `pnpm sync` after adding or changing user-facing strings to refresh catalogs, then commit the diff alongside the source change.

The frontend `pnpm check` command intentionally excludes two categories of files from Biome:

- generated Lingui locale bundles under `internal/site/src/locales/**/*.ts`
- the Tailwind v4 stylesheet entrypoint `internal/site/src/index.css`

Those files are still validated indirectly by the normal frontend build, but they are not useful Biome targets because the locale bundles are generated artifacts and the Tailwind v4 at-rules used in `index.css` are not parsed cleanly by the current Biome version.

`pnpm build` never type-checks: Vite transpiles TypeScript with SWC and drops the types. `pnpm typecheck` (`lingui compile && tsc -b --noEmit`) is the only type check; it compiles the Lingui catalogs first because `src/lib/i18n.ts` imports the generated (git-ignored) `src/locales/*/*.ts` bundles. Use `tsc -b` (project references through `tsconfig.app.json` and `tsconfig.node.json`), not `tsc -p .`: the root `tsconfig.json` has `"files": []` and checks nothing on its own.

## Development Modes

### Production-Style Hub

`make build-hub` uses the embedded frontend output from `internal/site/dist`.

In this mode:

- the frontend is bundled into the Go binary via `internal/site/embed.go`
- `internal/hub/server_production.go` serves static assets and returns the injected HTML shell

### Development Hub

`make dev-hub` or `make build-hub-dev` uses the `development` build tag.

In this mode:

- the hub proxies frontend traffic to `localhost:5173`
- `internal/hub/server_development.go` modifies the proxied HTML to inject app info
- Vite is expected to be running separately via `make dev-server`

If the frontend looks broken in development, check that the Vite dev server is actually running.

## Testing Rules

### Always Use The Build Tag

Go tests in this repository are build-tagged with:

```go
//go:build testing
```

That means the correct command is:

```bash
go test -tags=testing ./...
```

or:

```bash
make test
```

Do not rely on plain `go test ./...` for normal verification here.

### Why This Matters

Without the build tag:

- some test files will be skipped
- editors may show misleading “No packages found” messages for test files
- your verification may appear to pass while not actually running the intended tests

### Tests Must Not Depend On The User Or The Host

Go tests run as root in CI containers and Docker builds, and as a regular user on laptops. Keep them independent of both:

- for a path that must not be creatable, use a child of a regular file (`uncreatableDir` in `agent/data_dir_test.go`); root can create `/invalid/path`
- skip permission-denial cases when `os.Geteuid() == 0` (root bypasses directory permissions)
- clear the env variables the code reads, in both forms: the `VIGIL_AGENT_`/`VIGIL_HUB_` prefixed name wins even when empty, and developers often export `TOKEN`, `KEY` or `DATA_DIR` for `make dev-agent`
- do not assume a fixed local port is free (a dev hub may listen on it): bind `127.0.0.1:0` and close it to get a refused address
- never let a test write to real system or home locations: point `HOME` at `t.TempDir()`, and skip a case that would create `/var/lib/...` as root
- use `require.Error` before reading `err.Error()`, so a missing error fails the test instead of panicking and hiding the rest of the package
- do not depend on the host's package state: package-manager tests use fake `apt-get`/`dnf` binaries with recorded outputs (`agent/collectors/testdata/`)

Check agent changes in both modes, e.g. `docker run --rm -v "$PWD":/src -w /src golang:1.27 go test -tags=testing ./agent/...` and the same with `--user 1000:1000 -e HOME=/tmp -e GOCACHE=/tmp/gocache -e GOPATH=/tmp/gopath`.

## Test Helpers

Hub tests start from a data dir migrated once per test binary: `internal/hub/main_test.go` calls `pbtemplate.Run` (`internal/tests/pbtemplate`) from `TestMain`, and both `tests.NewTestHub(t.TempDir())` and the in-package `createTestHub` clone it (`pbtemplate.DataDirFor`) instead of an empty dir. Replaying the ~40 migrations for every test used to dominate the suite (`internal/hub`: ~22 s → ~7 s, and ~420 s → ~45 s under `-race`). Two rules follow:

- a migration that reads environment variables while it runs (today `initial-settings.go`: `USER_EMAIL`/`USER_PASSWORD`) cannot be reflected by the shared dir: list its variables in `migrationEnvVars` (`internal/tests/pbtemplate`), so tests that set them migrate from scratch
- call `Cleanup()` on every test hub, or its cloned data dir stays in `$TMPDIR`
- a new test package that creates many hubs should get the same `TestMain`

Useful helpers include:

- `internal/tests/hub.go`
  - test hub creation
  - user creation
  - generic record creation
- `internal/hub/hub_test_helpers.go`
  - hub-specific testing accessors
- `agent/agent_test_helpers.go`
  - agent-specific testing accessors

The repository already includes hub, agent, protocol, heartbeat, and integration-oriented tests.

## Verification By Change Type

### Hub Or Backend Change

Recommended verification:

1. `go test -tags=testing ./...`
2. `make build-hub`

If you changed routing, hooks, or auth behavior, also test the relevant flows manually in development.

### Agent Or Protocol Change

Recommended verification:

1. `go test -tags=testing ./...`
2. `make build-agent`
3. `make build-hub`

If the handshake or request manager changed, prioritize the integration-style tests involving agent connection.

### Frontend Change

Recommended verification:

1. `pnpm --dir ./internal/site check`
2. `pnpm --dir ./internal/site typecheck`
3. `pnpm --dir ./internal/site build`
4. if backend integration changed, `go test -tags=testing ./...`

### Docker Hub Verification

Recommended verification:

1. `docker compose -f supplemental/docker/hub/docker-compose.dev.yml up --build`
2. open the hub on `http://localhost:8090`
3. verify the data volume persists across container restarts

Important:

- `supplemental/docker/hub/docker-compose.dev.yml` still runs the hub in production-style embedded-frontend mode; it does not proxy to Vite like `make dev-hub`
- the Docker image now rebuilds `internal/site/dist` from `internal/site/src` during image build, so frontend source changes are picked up even if your local `dist/` is stale
- if the Docker builder cannot resolve `storage.googleapis.com` during `go mod download`, retry with a direct Go module fetch path, for example: `GOPROXY=direct docker compose -f supplemental/docker/hub/docker-compose.dev.yml build --no-cache`; the Dockerfile now supports overriding `GOPROXY` and includes `git` in the Go builder stage for this fallback

### Release Verification

Recommended verification:

1. `goreleaser check`
2. `go test -tags=testing ./...`
3. create a test tag locally if you want to validate prerelease behavior

### Rename Or Boilerplate Metadata Change

Recommended verification:

1. `go test -tags=testing ./...`
2. `make build-web-ui`
3. `make build`

## Common Workflow Shortcuts

### Start The Full Local Stack

```bash
make dev
```

This is useful when you want:

- Vite frontend hot reload
- development hub serving
- a local agent process

### Start Only The Hub Frontend Pair

```bash
make dev-server
make dev-hub
```

### Run The Agent Only

```bash
HUB_URL=http://localhost:8090 TOKEN="..." KEY="..." make dev-agent
```

This is useful when you are only debugging agent startup, fingerprinting, or hub connection behavior.

## Repo-Specific Workflow Gotchas

- `make build-hub` depends on the frontend build unless `SKIP_WEB=true` is set.
- `make dev-hub` creates a placeholder `internal/site/dist/index.html` because the development server path still expects the directory to exist.
- `make dev-agent` still uses the placeholder module path from `go.mod`, so derived projects should keep it in sync when renaming.
- frontend localization artifacts are generated and compiled as part of the normal frontend build flow.
- `pnpm check` is expected to focus on maintainable source files; generated Lingui bundles and the Tailwind v4 root stylesheet are verified through `pnpm build` instead.
