# Common Issues

## Tests Are Not Running Or The Editor Says "No Packages Found"

### Symptoms

- `go test ./...` appears to miss tests
- editor or LSP shows warnings for test files
- you cannot reproduce CI-like behavior locally

### Cause

Go tests in this repository use the `testing` build tag.

### Fix

Use:

```bash
go test -tags=testing ./...
```

or:

```bash
make test
```

### Related Files

- `internal/tests/hub.go`
- all `*_test.go` files with `//go:build testing`

## Logins Or API Calls Fail With `429 Too Many Requests`

### Symptoms

- the login form reports "Too many attempts" after a couple of attempts (API clients get HTTP `429`)
- several users, or agents, are throttled at the same time even though each one sends little
- the request logs (PocketBase dashboard → Logs) show `triggered rate limit rule`

### Cause

The hub enables PocketBase's rate limiter at startup (`*:auth` allows 2 attempts per 3 seconds per client IP, `/api/` 300 requests per 10 seconds). Every client that reaches the hub with the same IP shares those buckets:

| Deployment | IP the hub sees | Fix |
|---|---|---|
| LAN, clients connect directly | one per machine | nothing to do |
| Hub reached from one public IP (e.g. AWS whitelisted to the office IP, everyone behind its NAT) | the office IP, for everyone | `RATE_LIMIT_EXCLUDED_IPS=<office IP or CIDR>` (the firewall already keeps everyone else out) |
| Behind a reverse proxy / load balancer (Caddy, nginx, AWS ALB…) | the proxy's IP | `TRUSTED_PROXY_IPS` + `TRUSTED_PROXY_HEADERS` (see below) |
 Behind a reverse proxy without `TRUSTED_PROXY_IPS` + `TRUSTED_PROXY_HEADERS`, every client has the proxy's IP and shares one bucket. Client-IP headers set in the PocketBase dashboard are also ignored while `TRUSTED_PROXY_IPS` is empty (a startup warning says so).

### Fix

- set `TRUSTED_PROXY_IPS` to the proxy's IP/CIDR and `TRUSTED_PROXY_HEADERS` to the header it sets (`X-Real-IP`, `CF-Connecting-IP`, …), then restart
- tune a rule in the PocketBase dashboard (Settings → Rate limits); edited rules survive restarts, deleted Vigil rules come back
- all users come from one address (office NAT): set `RATE_LIMIT_EXCLUDED_IPS` to it
- as a last resort, `RATE_LIMITS=false` disables the limiter

### Related Files

- `internal/hub/rate_limits.go`
- `internal/hub/api.go` (`registerMiddlewares`)

## The Agent Connects But Fails Verification

### Symptoms

- handshake fails during connection
- agent logs show invalid signature or verification problems
- the agent exits at startup with `no hub public key configured` (no `KEY`, `KEY_FILE` or `--key`, or the key file is empty)
- hub never completes agent registration or online transition

### Common Causes

- wrong `KEY` or `KEY_FILE`
- stale hub public key after changing hub data dir or keypair
- token or connection target points to a different hub than expected

### Fix

1. fetch the current hub public key from `/api/app/info`
2. update the agent key configuration
3. restart the agent
4. verify the agent is talking to the intended hub URL

### Related Files

- `internal/hub/hub.go`
- `internal/hub/api.go`
- `agent/client.go`
- `agent/keys.go`

## The Agent Registers As A New Machine Unexpectedly

### Symptoms

- an existing agent appears as a new agent record
- the hub no longer associates reconnects with the expected agent entry

### Common Causes

- agent fingerprint file was deleted
- agent data directory changed
- containerized agent lost its persistent storage

### Fix

1. inspect the configured or detected agent data directory
2. check whether the fingerprint file still exists
3. restore or preserve persistent storage if the original identity should remain

### Related Files

- `agent/data_dir.go`
- `agent/fingerprint.go`

## Token Rotation Did Not Reset Agent Identity

### Symptoms

- token changed, but the hub still treats the agent as the same machine

### Cause

Token identity and fingerprint identity are different.

### Fix

- rotate the token if you only want to change auth credentials
- reset the fingerprint only if you intentionally want a new stable agent identity

Do not treat these operations as interchangeable.

## The Agent Appears Offline Briefly Or Status Lags Behind Connection Events

### Symptoms

- socket disconnect happens, but status does not update immediately
- reconnect succeeds, but UI state appears to lag

### Cause

The hub delays offline marking briefly to allow fast reconnects.

### Fix

- treat short delays as expected behavior first
- inspect `DownChan` handling if the delay seems excessive

### Related Files

- `internal/hub/ws/ws.go`
- `internal/hub/agent_connect.go`

## Development Frontend Looks Broken Even Though The Hub Is Running

### Symptoms

- hub is running locally, but frontend assets fail to load
- UI appears blank or stale in development mode

### Common Causes

- Vite dev server is not running
- hub is running in development mode but cannot proxy to `localhost:5173`
- frontend was changed but not rebuilt for embedded mode

### Fix

For development mode:

```bash
make dev-server
make dev-hub
```

For production-style local validation:

```bash
make build-hub
```

### Related Files

- `internal/hub/server_development.go`
- `internal/hub/server_production.go`
- `internal/site/embed.go`

## First-Run Login Flow Is Confusing

### Symptoms

- app shows account creation when sign-in was expected
- app shows sign-in when initial admin creation was expected

### Common Causes

- no users exist yet
- test or local data dir changed
- a seeded user was created via migration env values

### Fix

1. check whether any `users` records exist
2. verify the active hub data directory
3. inspect first-run behavior in `/api/app/first-run`

### Related Files

- `internal/hub/api.go`
- `internal/users/users.go`
- `internal/migrations/initial-settings.go`
- `internal/site/src/components/login/login.tsx`

## OAuth Login Opens Poorly Or Fails In Popup Flow

### Symptoms

- popup-based auth behaves inconsistently
- browser blocks the OAuth popup

### Common Causes

- browser popup restrictions
- `OAUTH_DISABLE_POPUP` behavior or deployment assumptions
- provider misconfiguration in PocketBase admin

### Fix

1. verify provider configuration in PocketBase admin
2. check whether popup mode is disabled by env
3. test in a browser context that allows popups for the site

### Related Files

- `internal/hub/server.go`
- `internal/site/src/components/login/auth-form.tsx`

## Agent Connection Fails Because The Wrong Token Source Is Used

### Symptoms

- agent cannot authenticate even though a token seems configured

### Common Causes

- `TOKEN` and `TOKEN_FILE` disagree
- wrong env prefix is being used
- stale env file mounted by a service manager

### Fix

1. check the effective token source
2. verify prefixed vs unprefixed env names
3. inspect service environment files if using systemd, Docker, or rc scripts

### Related Files

- `agent/client.go`
- `agent/utils/utils.go`
- `supplemental/scripts/install-agent.sh`

## Build Or Runtime Behavior Still Uses Old Project Names After A Rename

### Symptoms

- binaries, env names, or UI labels still use the old boilerplate name

### Common Causes

- `app.go` was updated but deployment assets were not
- `go.mod` or imports were not fully renamed
- localized strings or package metadata still refer to old names

### Fix

Use the rename checklist in:

- `docs/customization/renaming-and-derived-projects.md`

Focus on:

- `app.go`
- `go.mod`
- `internal/site/package.json`
- `.goreleaser.yml`
- `supplemental/`

## Heartbeat Configuration Exists But No Pings Are Sent

### Symptoms

- heartbeat endpoint never receives traffic

### Common Causes

- `HEARTBEAT_URL` is unset or malformed
- method or interval assumptions are wrong
- hub instance did not start the heartbeat loop as expected

### Fix

1. verify `HEARTBEAT_URL`
2. confirm method and interval env values
3. use the heartbeat test path if available
4. check hub logs for heartbeat startup or request errors

### Related Files

- `internal/hub/heartbeat/heartbeat.go`
- `internal/hub/api.go`

## Ping Monitor Stays Down Even For Reachable Hosts

### Symptoms

- a `ping` monitor always reports `down`
- `last_msg` mentions missing `ping`, timeout, or a command error from the hub

### Common Causes

- the hub runtime does not have the `ping` executable on `PATH`
- the deployment environment blocks ICMP echo for the hub process
- a hardened container or Kubernetes policy removed the network privileges needed by the system `ping`
- the target host or network drops ICMP while still allowing TCP or HTTP traffic
- the monitor’s `count`, timeout, or IP family settings do not match the network path you are testing

### Fix

1. verify the hub host or container can run `ping <target>` manually
2. if running the official container image, confirm the deployment did not strip the packaged `ping` binary or over-restrict networking
3. inspect `last_msg` on the monitor record for the exact runtime failure reported by the hub
4. try adjusting `count`, per-request timeout, or `IPv4` / `IPv6` if the default path is not reachable
5. if ICMP is intentionally filtered in your environment, use a `tcp` or `http` monitor instead

### Related Files

- `internal/hub/monitors.go`
- `internal/dockerfile_hub`
- `docs/operations/deployment-and-packaging.md`

## When In Doubt

For most repository-specific issues, inspect these in order:

1. `app.go`
2. `docs/README.md`
3. `docs/conventions-and-gotchas.md`
4. the subsystem-specific doc for hub, agent, frontend, or auth
5. the high-signal files listed in `docs/ai/agent-navigation.md`

## Containers Are Missing For A Host (Docker Inventory Empty)

### Symptoms

- a host that runs Docker shows no containers, or its Docker state is `permission_denied`

### Cause

- the agent's service user has no access to the Docker socket. Since the least-privilege installer change, `install-agent.sh` only grants it with `--docker` (docker group membership is root-equivalent); the `.deb` asks through the `vigil-agent/docker_access` debconf question

### Fix

- re-run the install script with `--docker` (or copy *Copy install script with Docker monitoring* from the *Add agent* dialog), or `dpkg-reconfigure vigil-agent` for the `.deb`
- the agent picks up the new group on restart: both the script and `dpkg-reconfigure` restart it (for the `.deb`, once `HUB_URL` and `KEY` are set in `/etc/vigil-agent.conf`)

### Related Files

- `supplemental/scripts/install-agent.sh`
- `supplemental/debian/postinstall.sh`
- `agent/collectors/docker.go`
