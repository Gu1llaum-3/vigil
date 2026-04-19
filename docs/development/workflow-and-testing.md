# Workflow And Testing

## Tooling

### Required

- Go
- Node.js with `npm`, or `bun` as an alternative frontend package runner

### Optional But Useful

- `entr` for hot-reload during development
- `golangci-lint` for Go linting

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
  - runs the agent in a development loop
- `make dev`
  - runs frontend, hub, and agent development processes together

### Maintenance Targets

- `make test`
  - runs Go tests with the `testing` build tag
- `make tidy`
  - runs `go mod tidy`
- `make lint`
  - runs `golangci-lint`
- `make clean`
  - cleans Go build output and removes `build/`

## Frontend Commands

Frontend commands live in `internal/site/package.json`.

Run them from the repository root via the `Makefile`, or directly in `internal/site`.

Important commands:

- `npm run dev`
- `npm run build`
- `npm run sync`
- `npm run sync_and_purge`
- `npm run lint`
- `npm run check`
- `npm run check:fix`

The build command also performs Lingui extraction and compilation.

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

## Test Helpers

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

1. `npm run --prefix ./internal/site check`
2. `npm run --prefix ./internal/site build`
3. if backend integration changed, `go test -tags=testing ./...`

### Docker Hub Verification

Recommended verification:

1. `docker compose -f supplemental/docker/hub/docker-compose.dev.yml up --build`
2. open the hub on `http://localhost:8090`
3. verify the data volume persists across container restarts

Important:

- `supplemental/docker/hub/docker-compose.dev.yml` still runs the hub in production-style embedded-frontend mode; it does not proxy to Vite like `make dev-hub`
- the Docker image now rebuilds `internal/site/dist` from `internal/site/src` during image build, so frontend source changes are picked up even if your local `dist/` is stale

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
make dev-agent
```

This is useful when you are only debugging agent startup, fingerprinting, or hub connection behavior.

## Repo-Specific Workflow Gotchas

- `make build-hub` depends on the frontend build unless `SKIP_WEB=true` is set.
- `make dev-hub` creates a placeholder `internal/site/dist/index.html` because the development server path still expects the directory to exist.
- `make dev-agent` still uses the placeholder module path from `go.mod`, so derived projects should keep it in sync when renaming.
- frontend localization artifacts are generated and compiled as part of the normal frontend build flow.
