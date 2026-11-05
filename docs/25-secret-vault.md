# 25. Secret Vault

The Secret Vault stores API keys and logins on the gateway so an agent can use
them without the value ever reaching the LLM provider, the session history,
traces, events or logs. The agent only handles a placeholder, `{{secret:name}}`.
The gateway swaps in the real value at the last moment, inside the tool that
makes the request, and redacts the value from everything that comes back.

The vault is **local to the tenant that owns it** and is off when
`GOCLAW_ENCRYPTION_KEY` is not set.

## Setup

1. Set `GOCLAW_ENCRYPTION_KEY` (same key as provider credentials; formats in
   [06-store-data-model.md](./06-store-data-model.md) section 5). Without it the
   vault store is not created, the UI page returns 503 and agents cannot use
   placeholders. Changing the key makes existing entries unreadable.
2. Open **Secret Vault** in the web UI (tenant admin only), add a secret, set
   the **allowed domains** it may be sent to, then open **Access** and grant it
   to the agents that may use it. An agent without a grant cannot use the secret.

Entries can also be managed with the CLI (PostgreSQL only):

```bash
goclaw secret-vault list    --tenant-id <uuid>   # shows STATUS (active|pending) and the creating AGENT
goclaw secret-vault set     --tenant-id <uuid> --name gh --kind api_key --domains api.github.com   # value read from stdin
goclaw secret-vault approve --tenant-id <uuid> --name captured-token --domains api.example.com     # activate a pending entry
goclaw secret-vault delete  --tenant-id <uuid> --name gh
```

The CLI needs `GOCLAW_ENCRYPTION_KEY` and a database DSN. The value is read from
stdin without echo and is never printed.

## Using a secret

| Surface | How |
|---|---|
| `http_request` tool | Put `{{secret:name}}` in a header, query string or body. Only `api_key` entries; the host must be in the entry's allowed domains. Redirects to another host are blocked before anything is sent. |
| Browser `fill_secret` act | Fills a form field with a `login` entry. The value is applied to the page and never returned in snapshots or screenshots text. Only `https` (or loopback `http`) pages whose host is allowed. |
| `request_secret` tool | The agent asks the user for a missing secret. A **Secure Form** opens in the web UI (not in the chat), and the agent gets back only the placeholder. Web UI only; fails fast when no UI is connected. |

`login` entries default to **ask before each use**; `api_key` entries do not.
An approval is remembered per secret and host for the run. Approvals and forms
are answered by the user who owns the run, not by another admin.

Form timeout is 5 minutes and approval timeout 2 minutes. A secret saved with
scope "this session only" lives in gateway memory for about 30 minutes and is
never written to the database.

## Agents saving secrets

An agent can also put a secret *into* the vault, for example a session token
it finds after a login. This is off by default and needs two decisions from a
person: the agent must be allowed to save, and every saved secret must be
approved before anyone can use it.

### Per-agent opt-in

The flag is `other_config.vault_agent_write` on the agent (boolean, default
`false`). Only an explicit `true` enables it. Set it on the Secret Vault page
with **Agents allowed to save** ("Allow this agent to save secrets"), or with
`PUT /v1/agents/{id}`.

`PUT /v1/agents/{id}` replaces the whole `other_config` object; it does not
merge keys. Send the agent's current `other_config` with only this key
changed. The web UI does this for you: it re-reads the agent right before
the write. The flag is read from the agent record when a run starts, never
from tool arguments.

### Tools

| Tool | What it does |
|---|---|
| Browser `act` `capture_secret` | Reads a value from `localStorage`, `sessionStorage` or a cookie of the current page and writes it straight to the vault. The value is read inside the gateway's browser code and never goes through the model, the tool result, `evaluate`, logs or traces. The allowed domain is the page's own host, taken from the page, never from arguments. The storage read also reports the protocol and host of the document it read from; if the tab navigated to another origin after the address check, the value is dropped and the capture refused. The result holds only a status and `{{secret:name}}`. Top frame only, 8 KB max, an existing name is rejected. **Use this when the secret is already in the browser.** |
| `vault_secret_set` | Saves a value the agent passes as an argument, either for the session only (at most 20 per chat session) or as a pending vault entry. See the limit below. |
| `vault_grant` | Asks to grant a secret the agent created to another agent in the same tenant. Always needs a live approval from a tenant admin (see below) who can access the receiving agent; otherwise, or with no UI, it is refused (no queue). The grant is added on its own, so a grant an admin revoked during the wait stays revoked. |

**`vault_secret_set` cannot meet the "never in a transcript" bar.** The value
is a tool argument, so the model has already produced it and it is already in
the model context and the session history before the tool runs. Redacting the
result does not undo that. As a mitigation the agent loop registers the
value with the run redactor as soon as the model response arrives, for every
`vault_secret_set` call in it at once, before the LLM span, any tool call of
the batch (including parallel siblings that carry the same value) or the
stored assistant turn records it. A call dropped as truncated or malformed is
covered for whatever part of its arguments the provider could parse; the
providers keep no raw argument text beyond a parse error. The provider has
still seen the value. Prefer `capture_secret` (value in the browser) or
`request_secret` (value from the user through the Secure Form).

### Pending approval

Every entry an agent saves is created with `status = pending`,
`created_via = agent` and `created_by_agent = <agent UUID>`. A pending entry
is invisible to every resolution path (placeholders, `fill_secret`, granted
lists) and cannot be used until approved. Its name is taken, though: a write
under that name is refused. Every name an agent cannot use (another agent's,
a user's, a pending one, one already in the chat session) gets the same
"this secret name cannot be used" answer, so the reason cannot be probed; the
audit row records it. Agents cannot overwrite or delete entries; edits and
deletes stay with tenant admins. Each agent may hold at most 20 pending
entries; further saves are refused.

When the Web UI is open during the run and the run's user is a tenant admin,
that user is asked live and a yes activates the entry. A live yes counts with
the rule of the REST approve route: the user is an owner or admin of the
tenant and answers from a client with at least the admin role (a system owner
always counts). The tenant role is read from the tenant store when the
question is raised; without it nothing counts (fail closed). A user who is no
tenant admin is not asked, and a yes from a client below admin is ignored and
audited. If an admin approved the entry on the Secret Vault page meanwhile,
the run reports it active. Otherwise the entry waits on the Secret Vault page, where it shows
a **Pending approval** badge and can be filtered. **Approve** shows the
domains the agent proposed and the creating agent; the user confirms or edits
the domains, and only the confirmed list is stored.

```http
POST /v1/secret-vault/{id}/approve
{"allowed_domains": ["api.example.com"]}
```

| Result | Status |
|---|---|
| Activated; body is the entry with `status: "active"` | 200 |
| `allowed_domains` missing, empty or invalid; bad id or JSON | 400 |
| Caller is not a tenant admin | 403 |
| Unknown id, or an entry of another tenant | 404 |
| Entry is already active | 409 |

The store writes one `approved` audit row with the approving user in the same
transaction. Every agent write and refusal is audited as well.

## Data model

Migration `000103` (PostgreSQL) and `schema.sql` (SQLite) add:

| Table | Purpose |
|---|---|
| `secret_vault_entries` | Name, kind, allowed domains, approval flag, hints, AES-256-GCM encrypted payload. Unique per tenant and name. |
| `secret_vault_grants` | Which agents may use an entry. |
| `secret_vault_audit` | Created, updated, deleted, used, denied and approved events with agent, user, host and outcome. Never holds a value. Survives entry deletion. |

Migration `000104` (PostgreSQL) and SQLite schema version 65 add `status`
(`active` or `pending`), `created_via` (`user` or `agent`) and
`created_by_agent` to `secret_vault_entries`. Existing entries are `active`.

No endpoint returns a secret value. The REST API (`/v1/secret-vault`) returns
metadata only (including `status`, `created_via` and `created_by_agent`), and
every route requires a tenant admin.

## Threat model

**Protected**

- The LLM provider, session store, traces and spans, WebSocket events, logs and
  REST responses never see the value. The redactor is loaded with every secret
  granted to the agent before the first tool call, and masks the raw value and
  its URL, base64 (standard and URL-safe) and JSON/quoted encodings, plus the `username:password` pair used by Basic auth in tool results, errors and
  deliverables, including text read back later from a server that stored it.
- A secret cannot go to a host outside its allowed domains, including through
  redirects, and cannot be resolved in a URL host.
- Another tenant cannot list, read, change, grant, audit or use an entry
  (enforced by the tenant in the request context, covered by the tenant
  invariant test).
- Subagents do not inherit the parent's vault runtime. The MCP bridge and tools
  reached outside the agent loop (for example `/tools/invoke`) have no runtime,
  so a placeholder there fails closed without any network call.
- The vault writes no temporary file containing a value.

**Not protected**

- A user pasting a secret into the chat. Use the Secure Form instead.
- A page or server that transforms the value into a form the redactor does not
  know (custom encoding, splitting it across fields, an image of it).
- Content in iframes or shadow DOM that the browser tool cannot mask.
- A malicious server on an *allowed* domain that is sent the value on purpose:
  the vault limits where a secret goes, not what that server does with it.
- Values shorter than 4 characters are redacted only on word boundaries to
  avoid corrupting normal text. A secret embedded in a larger base64 payload at
  a non-aligned offset is not recognised.
- A value given to `vault_secret_set`: it is a tool argument and has already
  reached the model and the session history (see above).

**Agents saving secrets**

- Prompt injection could make an agent save an attacker-chosen value or
  propose an attacker-chosen domain. Mitigations: the per-agent flag is off by
  default, every saved entry is pending until a person approves it and
  confirms the domains, `capture_secret` takes the domain from the page and
  not from arguments, and the pending cap limits flooding.
- Browser state is isolated per tenant + user + agent through incognito
  browser contexts, so another agent or user does not see this agent's
  `localStorage` or cookies.
- Browser scopes with no user and no agent use the main Chrome profile, which
  is not isolated. `capture_secret` must not run there.
- The agent that owns a browser context can still read its own storage with
  `evaluate`, in the same run or a later one. `capture_secret` does not block
  that: what it guarantees is that the value it captures does not pass through
  the model context, logs or traces of the run, and that the write is audited.

## Browser take over

When a step needs a person (SSO, CAPTCHA, QR code, one-time code), the agent
calls the browser tool with `action: "takeover"`. The Web UI shows a card with
the site and a countdown (10 minutes at most); the user finishes the step in the
browser window and presses Done, or declines.

- Works only from the Web UI and only with a visible, locally launched Chrome.
  A headless or remote (Docker sidecar) browser has no window yet, so the action
  returns an error telling the agent to ask the user to do the step another way.
- What the user types stays in the browser. It is never sent to the agent.
- Afterwards the tab is treated like a secret tab: input values are hidden in
  snapshots, `evaluate`, console and screenshot are closed until the agent
  navigates to another site, and only the run that asked can use the tab.
- Events `browser.takeover.requested` and `.resolved` reach only the owning
  user. Answers are bound to tenant and user; a foreign answer looks like
  "not found" and leaves the request open.
- Passkeys (WebAuthn) cannot be completed this way.

## Known limits

- `request_secret` is registered as a tool but not seeded in `builtin_tools`,
  so it does not appear in the tool settings UI.
- When a form replaces the value of an existing entry, its allowed domains stay
  as they were.
- The Phase 05 browser path is covered by unit tests, not yet by a run against
  real Chrome.
- Lite (desktop) edition: the store exists for SQLite, the CLI is PostgreSQL only.
  Agents cannot save secrets there: `vault_secret_set` and `vault_grant` are not
  registered and `capture_secret` refuses, because Lite has no surface to
  approve a pending entry.
- Surface parity: desktop N/A because the Lite edition has no vault approval surface.

## File reference

| Area | Path |
|---|---|
| Runtime, redactor, forms, session store | `internal/secretvault/` |
| Stores | `internal/store/pg/secret_vault.go`, `internal/store/sqlitestore/secret_vault.go` |
| Agent loop wiring | `internal/agent/loop_secret_vault.go` |
| Tools | `internal/tools/http_request.go`, `internal/tools/request_secret.go`, `internal/tools/vault_secret_set.go`, `internal/tools/vault_grant.go`, `pkg/browser/fill_secret.go`, `pkg/browser/capture_secret.go` |
| REST, WebSocket, CLI | `internal/http/secret_vault.go`, `internal/gateway/methods/`, `cmd/secret_vault.go` |
| Web UI (incl. approve dialog and agent opt-in) | `ui/web/src/pages/secret-vault/`, `ui/web/src/components/chat/secret-request-host.tsx` |
| Browser take over | `internal/browsertakeover/`, `pkg/browser/takeover.go`, `internal/gateway/methods/browser_takeover.go`, `ui/web/src/components/chat/takeover-request-host.tsx` |
| Tests | `internal/tools/secret_vault_leak_test.go`, `tests/invariants/secret_vault_tenant_isolation_test.go` |
