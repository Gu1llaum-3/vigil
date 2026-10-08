# Security

## Reporting a vulnerability

Please do not open a public issue. Report it privately through GitHub: the repository's
**Security** tab → **Report a vulnerability**. Include the version (hub and agent), the
configuration involved and how to reproduce it. Fixes ship in the latest release only.

## Threat model

Vigil is built for one organization running its own hub:

- **Users are trusted operators.** Every authenticated user sees the whole fleet: hosts,
  inventory, metrics and monitors. There is no per-owner scoping of hosts. `readonly` users
  change nothing in the fleet or the configuration (only their own settings and API keys);
  hub configuration — notifications, alert rules, maintenance windows, registry credentials,
  jobs, retention, host approvals — is for admins only.
- **The hub is trusted by its agents.** Agents only answer the hub whose public key they are
  given (`KEY`): it signs every connection (see below). They report inventory and metrics and
  run no command sent by the hub; the only thing the hub can write on an agent is its own
  agent token.
- **TLS is required between agents and the hub, and in front of the web UI.** Plaintext
  `http://`/`ws://` hub URLs are accepted for trusted networks only, with a warning.
- **The hub data directory is a secret.** It holds the hub private key (`id_ed25519`), the
  database (agent tokens, user accounts) and `credentials.key` (registry passwords).

## Hardening checklist

- Serve the hub over HTTPS. Behind a reverse proxy, set `TRUSTED_PROXY_IPS` and
  `TRUSTED_PROXY_HEADERS` so rate limits and logs see real client addresses; keep
  `RATE_LIMITS` on (the default).
- Never set `HUB_TLS_INSECURE=true` on agents outside development. For a private CA or a
  self-signed hub, use `HUB_CA_FILE`.
- Keep the enrollment token short-lived (the default is one hour), or disable it once your
  hosts are enrolled. Regenerate it if it may have leaked.
- Upgrade agents soon after the hub: an agent that predates per-agent tokens still shares the
  enrollment token (see below).
- Treat an `agent.duplicate_fingerprint` notification you did not expect as an incident
  (`docs/troubleshooting/common-issues.md`).

## How agents and the hub authenticate each other

- **Agent → hub.** Each agent holds a token of its own, issued by the hub after it first
  enrolls with the shared enrollment token. A connection that uses the enrollment token while
  claiming the identity of an enrolled host waits for an admin's approval and gets nothing.
- **Hub → agent.** On every connection an up-to-date agent sends a random nonce; the hub signs it
  with the agent token using its private key, and the agent checks the signature against `KEY`.
  A signature is good for that connection only, and once a hub key has signed a nonce the agent
  refuses the older, replayable form from it.

Details: `docs/architecture/auth-and-data-model.md` and
`docs/architecture/hub-agent-architecture.md`.

## Known limitations

These are accepted for a single-organization deployment. Plan around them.

- **A broken TLS link exposes the agent.** With `HUB_TLS_INSECURE=true` or a plaintext hub
  URL, someone on the network path sees the agent token and can relay the hub's signature live
  (the nonce prevents replay, not relaying): they can then read the agent's inventory, pose as
  that agent to the hub, or overwrite its agent token (it then falls back to its previous one).
- **Replayable signatures before the first nonce handshake.** Agents older than the nonce, and
  an upgraded agent until it has met a nonce-signing hub once (a fresh install), still accept a
  static signature over their token. Anyone holding a valid token, the enrollment token
  included, can obtain that signature from the hub. Using it still requires breaking TLS on
  the agent's link.
- **Session tokens live in `localStorage`.** The web UI keeps its PocketBase session token
  there, readable by any script running on the hub's origin. The default Content Security Policy
  (scripts limited to the hub itself and a per-request nonce) is the mitigation; a custom `CSP`
  takes that responsibility over.
- **No per-owner scoping.** All users see all hosts, and any non-`readonly` user can rename,
  tag or delete a host. Deleting a host revokes its token; it also stops protecting its identity
  against look-alike enrollments.
- **The enrollment token can be permanent.** A permanent enrollment token is a long-lived shared
  secret: anyone holding it can enroll new hosts (never take over an enrolled one — those wait
  for approval). Prefer the one-hour token, and regenerate after a leak.
- **Agents older than per-agent tokens.** Until it upgrades, an agent keeps the shared
  enrollment token, and anyone holding that token can attach to its record. Such hosts are
  flagged *shared* in Settings → Agents.
- **Hosts awaiting approval are kept until rejected.** All claimants of one fingerprint share a
  single pending record; make sure only your host is connecting before merging it.
