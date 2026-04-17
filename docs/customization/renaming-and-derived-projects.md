# Renaming And Derived Projects

## Why This Matters

Nexus is a reusable boilerplate. A derived project should be able to rename and rebrand it without leaving behind confusing defaults, broken package metadata, or mismatched service names.

This repository already centralizes a large part of that work in `app.go`, but renaming still affects multiple layers:

- Go module and imports
- binaries and service names
- env prefixes
- frontend branding and metadata
- deployment assets under `supplemental/`
- release and packaging configuration

## Recommended Rename Order

Apply renames in this order.

### 1. Update `app.go`

Start with `app.go` because it is the central metadata file.

Review and rename values such as:

- `DisplayName`
- `AppName`
- `HubBinary`
- `AgentBinary`
- `HubDataDirName`
- `AgentDataDirName`
- `HubEnvPrefix`
- `AgentEnvPrefix`
- `ReleaseOwner`
- `ReleaseRepo`

Why first:

- many backend, agent, and packaging defaults are derived from these constants
- it gives you one source of truth for the rest of the rename process

### 2. Update The Go Module Path

Edit `go.mod` and replace the placeholder module path with the real one for the derived project.

Then update imports across the repo that reference:

- `github.com/your-org/app`

This affects both Go code and tests.

### 3. Update Frontend Package Metadata

Review `internal/site/package.json` and related frontend metadata.

Check for:

- package name
- display name references
- homepage or repository references if present

### 4. Update User-Facing Frontend Branding

Review frontend components and generated metadata that display project identity.

High-signal places:

- `internal/site/src/components/logo.tsx`
- `internal/site/src/components/login/login.tsx`
- any places using `APP.DISPLAY_NAME`
- `internal/site/public/` assets if present

If the derived project has its own logo, icon set, or theme identity, this is the point to swap them.

### 5. Update Environment Prefixes And Operational Names

If you change env prefixes in `app.go`, audit all documentation and deployment assets that refer to env names.

This includes:

- shell examples
- install scripts
- systemd docs
- Docker Compose examples

Changing env prefixes is useful for a derived product, but it raises the risk of silent misconfiguration if the operational docs are not updated too.

### 6. Update Binary, Service, And Data Directory Names

Review any assets that refer to:

- hub binary name
- agent binary name
- service names
- data directory names

High-signal files:

- `supplemental/scripts/install-hub.sh`
- `supplemental/scripts/install-agent.sh`
- `supplemental/guides/systemd.md`
- `.goreleaser.yml`

### 7. Update Deployment And Packaging Assets

Review `supplemental/` for placeholders or generic naming.

Important targets:

- Docker Compose files under `supplemental/docker/`
- Kubernetes chart values under `supplemental/kubernetes/`
- Debian or packaging assets under `supplemental/`
- release metadata in `.goreleaser.yml`

These files are easy to miss because they are not part of the main runtime code path.

## Files To Review During Rename

### Core Metadata

- `app.go`
- `go.mod`
- `README.md`
- `AGENTS.md`

### Backend And Agent Imports

- all Go files importing `github.com/your-org/app`

### Frontend

- `internal/site/package.json`
- `internal/site/src/components/logo.tsx`
- `internal/site/src/components/login/login.tsx`
- any frontend references to display name or branding

### Deployment And Packaging

- `.goreleaser.yml`
- `supplemental/docker/**/*`
- `supplemental/scripts/*`
- `supplemental/guides/systemd.md`
- `supplemental/kubernetes/**/*`

## Frontend Branding Checklist

When turning Nexus into a product, decide whether to update:

- display name shown in login and document titles
- logo component
- favicon and PWA assets
- color palette and theme defaults
- marketing or support links if present later

The app currently already reads display information from the injected `APP` object, so use that as the preferred source of truth instead of hardcoding names in multiple components.

## Locale And Copy Regeneration

If you rename visible user-facing strings, review localization assets.

Relevant commands:

- `npm run --prefix ./internal/site sync`
- `npm run --prefix ./internal/site sync_and_purge`

Relevant files:

- `internal/site/lingui.config.ts`
- `internal/site/src/locales/`

Do not leave stale translated strings referring to the old boilerplate name.

## Derived-Project Defaults To Revisit

When creating a real product from Nexus, revisit these defaults instead of accepting them automatically:

- auth mode choices
- OAuth self-registration behavior
- enrollment token policy
- release owner and repo
- heartbeat behavior
- trusted-auth and auto-login behavior
- public app URL assumptions

These are product decisions, not just rename tasks.

## Validation Checklist After Renaming

Run this validation after the rename is complete.

1. search for the old project name and placeholder module path
2. run `go test -tags=testing ./...`
3. run `make build`
4. run `make build-web-ui`
5. verify login page title and branding
6. verify generated binaries and service names are correct
7. review Docker, script, and Helm assets for stale identifiers

## Suggested Search Terms

Useful searches after a rename:

- old product name
- `github.com/your-org/app`
- old env prefix
- old binary name
- old service name
- old data directory name

## What Should Stay Generic Upstream

If you are still maintaining Nexus as the upstream boilerplate, keep these generic in the upstream repository:

- module placeholders
- binary and display names
- deployment examples
- documentation examples

Only specialize them inside the derived project repository.
