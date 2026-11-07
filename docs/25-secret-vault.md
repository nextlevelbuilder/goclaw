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
goclaw secret-vault list   --tenant-id <uuid>
goclaw secret-vault set    --tenant-id <uuid> --name gh --kind api_key --domains api.github.com   # value read from stdin
goclaw secret-vault delete --tenant-id <uuid> --name gh
goclaw secret-vault rewrap --tenant-id <uuid> [--dry-run]   # upgrade legacy ciphertext, see "Encryption binding"
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

## Data model

Migration `000103` (PostgreSQL) and `schema.sql` (SQLite) add:

| Table | Purpose |
|---|---|
| `secret_vault_entries` | Name, kind, allowed domains, approval flag, hints, AES-256-GCM encrypted payload. Unique per tenant and name. |
| `secret_vault_grants` | Which agents may use an entry. |
| `secret_vault_audit` | Created, updated, deleted, used, denied and conflict events with agent, user, host and outcome. Never holds a value. Survives entry deletion. |

No endpoint returns a secret value. The REST API (`/v1/secret-vault`) returns
metadata only, and every route requires a tenant admin.

## Encryption binding and concurrent edits

Migration `000104` (PostgreSQL) and SQLite schema version 65 add a `revision`
column to `secret_vault_entries`.

**Entry-bound ciphertext.** New and updated secrets are stored as
`v2:aes-gcm:<base64url>`. The AES-GCM additional data covers the tenant ID,
entry ID, name, kind, allowed domains (sorted and lower-cased), approval flag
and revision. A ciphertext copied onto another entry or tenant, or an entry
whose allowed domains were widened directly in the database, no longer
decrypts. Hints are not bound. Changing metadata through the API decrypts with
the old binding and seals again with the new one.

**Legacy rows.** Values written before this change (`aes-gcm:`) stay readable
and log `security.secret_vault.legacy_ciphertext`. They are upgraded by the
next write to that entry, or in bulk with `goclaw secret-vault rewrap`
(`--dry-run` only counts). Rewrap keeps revisions and timestamps, is safe to
re-run, never prints a value, and exits non-zero listing the IDs of any rows the
configured key cannot open. Keep the encryption key unchanged until it reports
no legacy rows.

**Optimistic concurrency.** Every entry response carries `revision`. `PUT
/v1/secret-vault/{id}` and `PUT /v1/secret-vault/{id}/value` accept
`If-Match: <revision>` (quotes and a `W/` prefix are tolerated). A stale
revision returns `409` with `{"code":"revision_conflict","current_revision":N}`
and writes a `conflict` audit row; nothing is changed. A malformed header
returns `400`. Without the header the write is unconditional, as before, and the
gateway logs `security.secret_vault.unconditional_write` so older clients can be
found. The web UI always sends the header.

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

## File reference

| Area | Path |
|---|---|
| Runtime, redactor, forms, session store | `internal/secretvault/` |
| Stores | `internal/store/pg/secret_vault.go`, `internal/store/sqlitestore/secret_vault.go` |
| Agent loop wiring | `internal/agent/loop_secret_vault.go` |
| Tools | `internal/tools/http_request.go`, `internal/tools/request_secret.go`, `pkg/browser/fill_secret.go` |
| REST, WebSocket, CLI | `internal/http/secret_vault.go`, `internal/gateway/methods/`, `cmd/secret_vault.go` |
| Web UI | `ui/web/src/pages/secret-vault/`, `ui/web/src/components/chat/secret-request-host.tsx` |
| Browser take over | `internal/browsertakeover/`, `pkg/browser/takeover.go`, `internal/gateway/methods/browser_takeover.go`, `ui/web/src/components/chat/takeover-request-host.tsx` |
| Tests | `internal/tools/secret_vault_leak_test.go`, `tests/invariants/secret_vault_tenant_isolation_test.go` |
