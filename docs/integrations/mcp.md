# MCP Integration (connect an AI assistant)

Vigil ships an embedded **Model Context Protocol (MCP)** server so an AI assistant
(Claude Desktop, Claude Code, …) can query your fleet — hosts, monitors, uptime and
response-time reports — through a small set of **read-only** tools.

It is served as a Streamable HTTP endpoint on the hub itself; there is **nothing to
install**. You authenticate with a Vigil API key.

## 1. Create an API key

In the web UI: **Settings → API keys → New key**. Give it a name (e.g. `mcp`) and copy the
token (`vk_…`) — it is shown **only once**. Keys are **read-only** today: the assistant can
read everything you can see, but cannot change anything.

You can revoke a key at any time from the same page (the **Regenerate** button issues a fresh
token and invalidates the old one).

## 2. The endpoint

```
https://<your-hub>/api/mcp
```

The exact URL (honoring any base path) is shown in **Settings → API keys → Connect an AI
assistant**, with a ready-to-copy configuration.

## 3. Configure your MCP client

Add the server to your client's MCP configuration (`.mcp.json`, or the client's UI). It is a
remote **HTTP** server authenticated with a bearer token:

```json
{
  "mcpServers": {
    "vigil": {
      "type": "http",
      "url": "https://<your-hub>/api/mcp",
      "headers": { "Authorization": "Bearer vk_your_key_here" }
    }
  }
}
```

- **Claude Code**: add the block above to your project's `.mcp.json` (or run the equivalent
  `claude mcp add` command), then restart.
- **Claude Desktop**: add it under `mcpServers` in the app's MCP config and restart.

Once connected, ask things like *"summarize my Vigil fleet"*, *"which monitors are down?"*,
or *"what's the 30-day uptime and average response time of each monitor?"*.

## Tools

All tools are **read-only** (`readOnlyHint`):

| Tool | Returns |
|---|---|
| `fleet_summary` | Host counts (total/connected/offline), monitor up/total, hosts needing reboot/updates, outdated/security package totals, container counts, OS/patch distributions |
| `list_hosts` | Every host with connection status and latest CPU / memory / disk / network metrics |
| `get_host` | One host's full detail: OS, packages, repositories, reboot status, Docker, metrics |
| `list_monitors` | All monitors grouped, with status, last latency, and computed **24h average latency** and **24h / 30d uptime** |
| `get_monitor` | One monitor's detail plus recent check points |
| `monitor_events` | A monitor's raw check history (status, `latency_ms`, timestamp), with `since`/`until`/`limit` — the basis for custom uptime / response-time reports |

## Notes & security

- **Read-only.** There are no write tools yet (creating/modifying/deleting monitors is a
  planned `read-write` scope). Scope is enforced by tool visibility: a `read` key is served
  only read-only tools, so even when write tools ship they are invisible/uncallable to a read
  key. Keys also authenticate only the Vigil app API (`/api/app/*` and `/api/mcp`), never the
  raw PocketBase collection API.
- **Auth is the API key.** The MCP endpoint requires a valid `Authorization: Bearer vk_…`;
  an unknown or expired key returns 401. Treat the token like a password.
- **Stateless.** The server keeps no per-session state; each request is independent.
- **Single-tenant.** As elsewhere in Vigil, a key sees the whole fleet (it acts as its owning
  user). See `docs/architecture/auth-and-data-model.md`.

## Implementation

- Endpoint + tools: `internal/hub/mcp_server.go` (uses `github.com/modelcontextprotocol/go-sdk`),
  mounted at `/api/mcp` in `internal/hub/api.go`.
- Auth: the `authenticateApiKey` middleware in `internal/hub/api_keys.go` (the `/api/mcp` path
  is exempt from the read-scope HTTP-method guard since MCP is POST-based; scope is instead
  enforced per-tool — `mcpHandler` serves a read-only vs read-write tool set by the key's scope).
- Tools reuse the same data builders as the REST handlers (`buildDashboard`,
  `buildMonitorsResponse`, `buildMonitorDetail`, `loadMonitorEvents`, `buildHostDetail`), so
  there is no duplicated query logic.
