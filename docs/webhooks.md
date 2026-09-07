# Webhook API Reference

> **Authoritative integration guide.** Describes inbound auth, endpoint contracts, outbound callback semantics, retry schedule, and security constraints.

## Table of Contents

1. [Overview](#1-overview)
2. [Admin CRUD](#2-admin-crud)
3. [Authentication](#3-authentication)
4. [Endpoint: POST /v1/webhooks/llm](#4-post-v1webhooksllm)
5. [Endpoint: POST /v1/webhooks/message](#5-post-v1webhooksmessage)
6. [Idempotency](#6-idempotency)
7. [Outbound Callbacks](#7-outbound-callbacks)
8. [Channel Capability Matrix](#8-channel-capability-matrix)
9. [Rate Limits](#9-rate-limits)
10. [Edition Differences](#10-edition-differences)
11. [Security](#11-security)
12. [HMAC Receiver Examples](#12-hmac-receiver-examples)
13. [Audit Payload Shape](#13-audit-payload-shape-webhook_callsrequest_payload)
14. [Encryption at Rest](#14-encryption-at-rest)

---

## 1. Overview

GoClaw webhooks let external systems trigger agents or deliver messages through connected channels. Two webhook kinds exist:

| Kind | Endpoint | Purpose | Editions |
|------|----------|---------|----------|
| `llm` | `POST /v1/webhooks/llm` | Invoke an agent with a user prompt (sync or async) | Standard + Lite |
| `message` | `POST /v1/webhooks/message` | Send a message to a user on a channel | Standard only |

Webhooks are tenant-scoped registry entries. Admins create them via the CRUD API; callers use the returned bearer token or HMAC signing key to authenticate inbound requests.

---

## 2. Admin CRUD

All admin endpoints require tenant-admin role. Bearer token authentication via `Authorization: Bearer <admin-token>`.

### Create — `POST /v1/webhooks`

```json
{
  "name": "my-integration",
  "kind": "llm",
  "agent_id": "<uuid>",
  "require_hmac": false,
  "localhost_only": false,
  "rate_limit_per_min": 60,
  "scopes": [],
  "ip_allowlist": []
}
```

Fields:

| Field | Type | Required | Notes |
|-------|------|----------|-------|
| `name` | string | yes | Max 100 chars |
| `kind` | string | yes | `"llm"` or `"message"` |
| `agent_id` | UUID | for `llm` kind | Agent to invoke |
| `channel_id` | UUID | optional | Pin webhook to a specific channel instance (message kind) |
| `require_hmac` | bool | no | Force HMAC-only auth (disable bearer) |
| `localhost_only` | bool | no | Restrict callers to 127.0.0.1/::1. Auto-set on Lite edition |
| `rate_limit_per_min` | int | no | Per-webhook cap; 0 = use tenant default |
| `scopes` | []string | no | Reserved for future scope enforcement |
| `ip_allowlist` | []string | no | Allowlist of IPs or CIDR ranges. Empty = allow all. See [IP Allowlist](#ip-allowlist) |

**Response — 201 Created**

```json
{
  "id": "<uuid>",
  "tenant_id": "<uuid>",
  "agent_id": "<uuid>",
  "name": "my-integration",
  "kind": "llm",
  "secret_prefix": "wh_ABCD",
  "secret": "wh_ABCDEFGHIJKLMNOPQRSTUVWXYZ234567ABCDEFGH",
  "hmac_signing_key": "a3f4...hex64chars",
  "scopes": [],
  "rate_limit_per_min": 60,
  "ip_allowlist": [],
  "require_hmac": false,
  "localhost_only": false,
  "created_at": "2026-04-21T12:00:00Z"
}
```

**`secret` and `hmac_signing_key` are returned exactly once — on create and rotate. Store them securely; they cannot be retrieved again.**

- `secret` — raw bearer token. Send as `Authorization: Bearer wh_...`
- `hmac_signing_key` — `hex(SHA-256(secret))`. Used as the HMAC signing key for `X-GoClaw-Signature`. To sign: `HMAC_SHA256(key=hex.Decode(hmac_signing_key), payload="{ts}.{body}")`

### List — `GET /v1/webhooks`

Query params (all optional):
- `agent_id=<uuid>` — filter by bound agent.
- `q=<text>` — case-insensitive match on name, or prefix match on `secret_prefix`.
- `include_revoked=true` — include revoked webhooks (default: excluded).
- `limit` — page size (default 20, max 200).
- `offset` — page offset (default 0).

Returns a paginated envelope. `secret` and `hmac_signing_key` are **not** included.

```json
{
  "items": [ /* webhook objects */ ],
  "total": 42,
  "limit": 20,
  "offset": 0
}
```

### List calls — `GET /v1/webhooks/{id}/calls`

Delivery history for a webhook. Query params (all optional): `status` (`queued`|`running`|`done`|`failed`|`dead`), `limit` (default 20, max 200), `offset`. Returns the same `{items, total, limit, offset}` envelope.

### Get — `GET /v1/webhooks/{id}`

Returns full webhook object (no secret).

### Update — `PATCH /v1/webhooks/{id}`

Partial update. All fields optional. Cannot change `kind`.

```json
{
  "name": "new-name",
  "require_hmac": true,
  "localhost_only": false
}
```

### Rotate Secret — `POST /v1/webhooks/{id}/rotate`

Generates a new secret immediately. **No grace window** — the old secret is invalidated the moment rotate completes. Coordinate with callers before rotating.

**Response — 200 OK**

```json
{
  "id": "<uuid>",
  "secret": "wh_NEW...",
  "hmac_signing_key": "newhex...",
  "secret_prefix": "wh_NEWX"
}
```

### Revoke — `DELETE /v1/webhooks/{id}`

Marks the webhook as revoked. All subsequent inbound requests with its secret return `401`. Action is irreversible.

---

## 3. Authentication

Two authentication modes. The webhook row's `require_hmac` field determines which are accepted.

### 3.1 Bearer Auth

```
Authorization: Bearer wh_ABCDEFGHIJKLMNOPQRSTUVWXYZ234567ABCDEFGH
```

The gateway SHA-256 hashes the token and looks up `secret_hash` in the database. Constant-time comparison prevents timing oracle attacks.

Bearer auth is **disabled** when `require_hmac=true` on the webhook row.

### 3.2 HMAC Auth

Recommended for Standard edition integrations. Provides both authentication and payload integrity.

**Required headers:**

```
X-Webhook-Id: <webhook-uuid>
X-GoClaw-Signature: t=<unix_seconds>,v1=<hmac_hex>
Content-Type: application/json
```

**Signing algorithm:**

```
signing_key = hex.Decode(hmac_signing_key)   // decode the hex field to raw bytes
payload     = "{unix_ts}.{request_body_bytes}"
signature   = HMAC_SHA256(key=signing_key, data=payload)
header      = "t={unix_ts},v1={hex(signature)}"
```

**Timestamp skew:** The gateway rejects requests where `|now - t| > 300 seconds`. Ensure your clock is synchronized (NTP).

**Key contract:** `hmac_signing_key` = `hex(SHA-256(raw_secret))`. The signing key is the **decoded bytes** of this hex string. The raw secret is never stored — only its hash.

### HMAC Replay Protection

After a valid HMAC signature is accepted, the gateway records `sha256(tenant_id + "|" + signature_hex)` in an in-memory nonce cache with a 320-second TTL (> 2× skew window). Any request replaying the same signature within the window is rejected with HTTP 401 and logged as `security.webhook.hmac_replay`.

**Single-node caveat:** The nonce cache is per-process and not distributed. In a multi-node deployment a replay could succeed on a different node. This is an accepted trade-off for the current single-process gateway architecture.

### IP Allowlist

When `ip_allowlist` is non-empty, the gateway checks the request's source IP (from `RemoteAddr`) against every entry after successful auth. Each entry can be:
- A single IP address: `"1.2.3.4"`, `"::1"`
- A CIDR range: `"10.0.0.0/8"`, `"2001:db8::/32"`

An empty `ip_allowlist` (the default) allows requests from any source — back-compat with existing webhooks.

Rejected requests return HTTP 403 and are logged as `security.webhook.ip_denied`.

**Proxy note:** `X-Forwarded-For` is **not** trusted — only `RemoteAddr` is used. If your gateway sits behind a reverse proxy, ensure the proxy is configured to terminate TLS and handle allowlist enforcement itself, or accept that `RemoteAddr` will be the proxy IP.

---

## 4. POST /v1/webhooks/llm

Triggers an agent with an input prompt. Available in all editions.

**Auth:** Bearer or HMAC (per webhook `require_hmac` setting). Webhook must have `kind="llm"`.

### Request

```json
{
  "input": "Summarize the latest metrics",
  "session_key": "user-123-session",
  "user_id": "ext-user-456",
  "model": "claude-opus-4-5",
  "mode": "sync",
  "callback_url": "",
  "media": [],
  "metadata": {}
}
```

| Field | Type | Required | Notes |
|-------|------|----------|-------|
| `input` | string or array | yes* | Plain string, or `[{role, content}]` array. *Optional when `media` is non-empty |
| `session_key` | string | no | Stable key for multi-turn conversation continuity |
| `user_id` | string | no | External user identifier for scoping |
| `model` | string | no | Per-request model override |
| `mode` | string | no | `"sync"` (default) or `"async"` |
| `callback_url` | string | required if async | HTTPS URL for delivery. Validated against SSRF policy |
| `media` | array | no | Attachments fetched server-side. Max 10 items, 25 MB each. **Sync mode only.** See [Media Attachments](#media-attachments) |
| `metadata` | object | no | Echoed to callback payload (max 8 KB) |

**Input formats:**

```json
// Plain string
"input": "Hello agent"

// Message array
"input": [
  {"role": "system", "content": "You are a concise assistant"},
  {"role": "user", "content": "List 3 key metrics"}
]
```

### Media Attachments

Pass remote URLs in `media` and the gateway downloads each one before the agent runs, then hands
the agent local files the same way the Telegram and WebSocket paths do. Attachments are fetched by
URL rather than uploaded because `POST /v1/media/upload` requires a user or admin role, which a
webhook caller holding a `wh_` bearer or an HMAC key does not have.

```json
{
  "input": "What is wrong in this screenshot?",
  "media": [
    {"url": "https://cdn.example.com/shot.png", "filename": "shot.png"}
  ]
}
```

| Field | Type | Required | Notes |
|-------|------|----------|-------|
| `url` | string | yes | Public HTTP(S). Validated against the SSRF policy; redirects are followed and every hop re-validated |
| `filename` | string | no | Original name. Used for the stored file name and for the `<media:document>` tag |

Things that are not guessable from the shape:

- **Allowed MIME types** — the same list as `media_url` on `/message`: `image/jpeg`, `image/png`,
  `image/gif`, `image/webp`, `video/mp4`, `audio/mpeg`, `audio/ogg`, `application/pdf`. The
  response `Content-Type` decides, not the URL extension, so an extension-less CDN link works. The
  declared type is also cross-checked against the file's actual leading bytes, and an
  `image/*` file that does not decode is rejected. Source of truth:
  `webhooks.AllowedMediaMIMETypes` in `internal/webhooks/inbound_media.go`.
- **`media` is sync-only.** `mode=async` with a non-empty `media` returns **400** and enqueues
  nothing. This is the first thing an integrator trips over. The async payload is frozen before
  the mode dispatch, so a downloaded file has no way to reach the worker — an accepted request
  would silently run with no image at all.
- **Any failed item fails the whole request.** There is no partial success and no per-item note in
  the answer. Either every attachment arrives or the request errors.
- **Failure detail is deliberately coarse.** The body carries a generic message per status code
  and never reveals whether a host resolved, what it resolved to, or why a connection failed —
  otherwise this endpoint would be an internal-network and port-scanning oracle. Do not write
  client logic that parses it.
- **Private and loopback URLs are refused**, including via redirect and DNS rebinding: the
  resolved destination IP is checked at every hop's dial. A self-hosted object store on a private
  network will not work.
- **Image dimensions are capped at 50 megapixels**, checked from the file header before any
  decode. A small file declaring huge dimensions is rejected with 413. An `image/*` file whose
  header does not decode at all is rejected with 415 — which includes **animated WebP**, since the
  decoder in use handles still WebP only. Send an animated image as a still frame or as
  `video/mp4`.
- **The URL only has to be reachable at POST time.** The download happens before the agent starts,
  so a short-lived signed Zalo or Messenger CDN URL works — that is the point of the design.
- **Latency.** The download runs before the agent starts and shares one deadline with it, so a
  sync call never exceeds `gateway.webhook_sync_timeout_sec` in total. A slow download eats into
  the agent's share of that budget and can end in a 504 — size the timeout for both halves.
- **Concurrency.** Each in-flight attachment reserves the full 25 MB against a 512 MB process-wide
  budget, regardless of its actual size, so roughly **20 attachments can be downloading at once
  across the whole gateway**. Past that, further media requests get 503 with `Retry-After: 5`
  until one finishes. This bound is separate from — and reached sooner than — the webhook lane's
  concurrency limit, because the download happens before a lane slot is taken.
- **The URL's query string is stripped everywhere it is retained or forwarded.** A presigned
  URL's signature is a bearer credential, so it is removed from the audit row
  (`request_payload`, readable via `GET /v1/webhooks/{id}/calls/{callId}` for 30 days), from the
  `<media:image url="...">` tag the agent sees — which is sent to the LLM provider and kept in
  session history — and from every log line. Only the scheme, host, and path survive. The gateway
  uses the full URL to fetch, and nothing else.
- **`user_id` and workspace isolation.** Media is persisted under
  `<workspace>/<agent_key>/<user_id>/.uploads/`. **A caller that omits `user_id` lands in
  `<workspace>/<agent_key>/.uploads/`** — no empty segment is created, and every such caller shares
  that one directory. Send a stable opaque id. The segment keeps only `[A-Za-z0-9_-]` and maps
  everything else to `_`, so `a.b@x.com` and `a_b_x_com` collide.
- **Not supported on the admin test endpoint.** `POST /v1/webhooks/{id}/test` and the web UI test
  dialog stay text-only.

Media the agent produces is **not** returned: the sync response carries `output` text only. The
asymmetry is deliberate for now, not an oversight.

### Integrating a middleware (Zalo OA, Messenger, CRM)

The common shape: a platform webhook hits your middleware, your middleware calls goclaw. The
mistake that costs the most time is putting the image URL **inside `input`**:

```json
// WRONG — the URL is just text. Nothing is downloaded, no media tag is built,
// and the model receives a string that looks like a link.
{
  "input": "Customer sent an image: https://cdn.example.com/abc.jpg\nAnswer based on it."
}
```

```json
// RIGHT — the URL goes in media[]. The gateway downloads it before the agent
// runs and prepends a <media:image> tag to the message.
{
  "input": "Answer based on the image and the customer's question.",
  "media": [
    {"url": "https://cdn.example.com/abc.jpg", "filename": "abc.jpg"}
  ]
}
```

A URL left in `input` is not guaranteed to fail: the agent *may* choose to call `read_image`
with it. That path is non-deterministic (it depends on the model deciding to call the tool),
skips the size cap and the MIME allowlist, and does not persist the file to the workspace.
`media[]` is the deterministic path.

#### Requirements for the URL you send

| Requirement | Why |
|---|---|
| Public HTTP(S), resolving to a public IP | The SSRF policy re-checks the resolved IP at every redirect hop's dial |
| **No authentication needed** | The gateway issues a plain GET and sets no headers. A URL requiring `Authorization`, a cookie, or a signed header will fail — proxy it instead |
| Response `Content-Type` in the allowlist | The header decides the type, not the file extension |
| Reachable at POST time | The download happens before the agent starts, which is what makes short-lived signed CDN links usable |
| Within 25 MB, and 50 MP for images | Hard caps |

#### Pass the platform URL directly, or proxy it first?

Both work. Choose by URL lifetime and auth:

- **Direct** — fine when the platform serves a public, unauthenticated URL. Fewest moving parts.
  The risk is expiry: platform CDN links are often short-lived, so a delayed retry, a replayed
  `Idempotency-Key`, or an operator re-running a request from the audit log will hit a dead URL.
- **Proxy through your own storage** (S3, R2, a Cloudflare Worker) — recommended when the
  platform URL is short-lived or requires a token. A stable URL also keeps the redacted URL in
  the audit row meaningful weeks later, which is when you actually need it.

Proxying is **required** when the platform URL needs an auth header, since the gateway sends
none.

#### Errors your middleware should handle

| Status | Meaning | Retry? |
|---|---|---|
| 400 | URL blocked by SSRF policy, download failed, or `media` sent with `mode=async` | No — fix the request |
| 413 | File over 25 MB, or image over 50 MP | No — resize before sending |
| 415 | MIME type not allowed, or content does not match the declared type | No |
| 503 | Media download capacity exhausted (`Retry-After: 5`) or lane at capacity | Yes, with backoff |
| 504 | The download plus the agent run exceeded `gateway.webhook_sync_timeout_sec` | Yes |

Any one failed attachment fails the whole request, and the status reflects the most severe
failure across all items, not the first one in the array. Failure messages are deliberately
generic — do not parse them.

#### Worked example

```bash
curl -X POST https://<host>/v1/webhooks/llm \
  -H "Authorization: Bearer wh_..." \
  -H "Content-Type: application/json" \
  -d '{
    "input": "Answer based on the image and the customer question.",
    "user_id": "zalo:1234567890",
    "session_key": "zalo:1234567890",
    "media": [{"url": "https://cdn.example.com/abc.jpg", "filename": "abc.jpg"}]
  }'
```

Send a stable `user_id`: media is persisted under
`<workspace>/<agent_key>/<user_id>/.uploads/`, and callers that omit it all share one
directory. `session_key` keeps a multi-turn conversation together.

### Sync Response — 200 OK

```json
{
  "call_id": "<uuid>",
  "agent_id": "<uuid>",
  "output": "Here are the metrics: ...",
  "usage": {
    "prompt_tokens": 250,
    "completion_tokens": 220,
    "total_tokens": 470,
    "cache_read_input_tokens": 120,
    "cache_creation_input_tokens": 30,
    "prompt_tokens_include_cached_segments": true
  },
  "total_cost_usd": 0.0279,
  "calls": [
    {
      "type": "llm_call", "name": "9router/cx/gpt-5.6 #1",
      "provider": "9router", "model": "cx/gpt-5.6",
      "prompt_tokens": 150, "completion_tokens": 200, "total_tokens": 350,
      "cache_read_input_tokens": 120, "cache_creation_input_tokens": 30,
      "prompt_tokens_include_cached_segments": true, "cost_usd": 0.0187
    },
    {
      "type": "tool_call", "name": "read_image",
      "provider": "9router", "model": "cx/gpt-5.5",
      "prompt_tokens": 100, "completion_tokens": 20, "total_tokens": 120,
      "cost_usd": 0.0092
    }
  ],
  "finish_reason": "stop"
}
```

> `cache_read_input_tokens` and `cache_creation_input_tokens` are present only when
> prompt caching was active (omitted otherwise). When
> `prompt_tokens_include_cached_segments` is `true`, `prompt_tokens` already counts
> the cached segments, so non-cached input = `prompt_tokens - cache_read_input_tokens`.
>
> `calls[]` lists every LLM call and every tool that makes a **direct** internal LLM
> call (e.g. `read_image`, `read_video`), each attributed to its `provider`/`model`
> with its own tokens and `cost_usd`. `usage` is the **sum of all calls** — so it
> includes tool-internal LLM tokens — and `total_cost_usd` is the sum of
> `calls[].cost_usd` (best-effort; `0` when a model has no configured pricing).
>
> Note: token spend inside **nested agent runs** (`subagent`/`delegate` tools, which
> spawn a separate child agent loop) is **not** itemized in `calls[]` and not included
> in `usage`/`total_cost_usd` — consistent with how the child run's usage has always
> been excluded from the parent's totals.

Sync mode times out after the configured deadline (default **600s**). On timeout: `504 Gateway Timeout` with `webhook.llm_timeout`.

#### Agent-run timeouts (configurable)

Both webhook agent-run deadlines default to **600s** and are capped at **3600s**. Configure via `config.json` or environment variables (env overrides config):

| Setting (config) | Env var | Applies to | Default |
|------------------|---------|-----------|---------|
| `gateway.webhook_sync_timeout_sec` | `GOCLAW_WEBHOOK_SYNC_TIMEOUT_SEC` | sync + admin test calls | 600 |
| `gateway.webhook_async_timeout_sec` | `GOCLAW_WEBHOOK_ASYNC_TIMEOUT_SEC` | async worker runs | 600 |

> Sync mode holds the HTTP connection open for the whole run — a value above an upstream proxy/load-balancer read timeout may be cut before the agent finishes. Async mode returns `202` immediately and runs in the background, so a longer deadline is safe.

#### Prompt caching (internal streaming)

Server-side webhook agent runs (sync, async, and admin test) stream provider responses internally by default. This lets OpenAI-compatible providers/routers populate and serve their prompt cache for the large, stable system+tools+history prefix — non-streaming requests are not cached by some routers, so webhook runs would otherwise pay full input-token price on every turn even with a stable session. The response returned to the caller is unchanged (the gateway assembles the streamed chunks into the same JSON/SSE payload).

| Setting (config) | Env var | Applies to | Default |
|------------------|---------|-----------|---------|
| `gateway.webhook_stream` | `GOCLAW_WEBHOOK_STREAM` | sync + async + test webhook agent runs | `true` |

> Set to `false` to restore non-streaming requests (e.g. for a provider that misbehaves when streaming). Caching for `cache_control`-style providers (Anthropic/DashScope) is unaffected by this flag.

### Async Response — 202 Accepted

```json
{
  "call_id": "<uuid>",
  "status": "queued"
}
```

The agent runs asynchronously. Results are delivered via outbound callback (see [Section 7](#7-outbound-callbacks)).

### Error Responses

| Status | Code | When |
|--------|------|------|
| 400 | `invalid_request` | Missing `input` and `media`, bad `mode`, missing `callback_url` for async, `media` sent with `mode=async`, or a `media` URL blocked by the SSRF policy |
| 401 | — | Auth failure (bearer invalid, HMAC mismatch, revoked, HMAC replay) |
| 403 | `unauthorized` | `localhost_only` violation, IP allowlist denial, kind mismatch, tenant mismatch |
| 404 | `not_found` | Agent not found |
| 413 | `invalid_request` | A media file exceeds 25 MB, or an image exceeds 50 megapixels |
| 415 | `invalid_request` | Media MIME type not in the allowlist, or the content does not match the declared type |
| 429 | — | Rate limit exceeded; `Retry-After: 60` header set |
| 503 | — | Webhook processing lane at capacity, or media download capacity temporarily exhausted (retryable) |
| 504 | — | LLM timeout (sync mode only) |

When several attachments fail, the status is chosen by a fixed severity order —
SSRF, then MIME denied, then too large, then download failed, then capacity — so it never depends
on which array position happened to fail.

---

## 5. POST /v1/webhooks/message

Sends a message to a user on a connected channel. **Standard edition only** — not available on Lite.

**Auth:** Bearer or HMAC (per webhook `require_hmac` setting). Webhook must have `kind="message"`.

### Request

```json
{
  "channel_name": "telegram-prod",
  "chat_id": "123456789",
  "content": "Hello from the integration!",
  "media_url": "https://example.com/image.jpg",
  "media_caption": "Optional caption",
  "fallback_to_text": false
}
```

| Field | Type | Required | Notes |
|-------|------|----------|-------|
| `channel_name` | string | yes (unless webhook has bound `channel_id`) | Channel instance name |
| `chat_id` | string | yes | Channel-specific recipient ID |
| `content` | string | yes (unless `media_url`) | Text body; max 16 KB |
| `media_url` | string | no | HTTPS URL to media file. SSRF-guarded + HEAD-probed |
| `media_caption` | string | no | Caption for media |
| `fallback_to_text` | bool | no | If true, send text-only when channel can't handle media |

### Response — 200 OK

```json
{
  "call_id": "<uuid>",
  "status": "sent",
  "channel_name": "telegram-prod",
  "chat_id": "123456789",
  "warning": ""
}
```

`warning` is set to `"media_not_supported_fallback_text"` when `fallback_to_text=true` and media was dropped.

### Error Responses

| Status | Code | When |
|--------|------|------|
| 400 | `invalid_request` | Missing `chat_id`, `content`, SSRF-blocked `media_url` |
| 403 | `unauthorized` | Channel belongs to different tenant |
| 404 | `not_found` | Channel instance not found |
| 413 | `invalid_request` | `media_url` exceeds the 25 MB size limit |
| 415 | `invalid_request` | MIME type denied for media |
| 429 | — | Rate limit exceeded |
| 501 | `invalid_request` | Channel does not support media and `fallback_to_text=false` |

---

## 6. Idempotency

All webhook endpoints support idempotency via the `Idempotency-Key` header.

```
Idempotency-Key: <opaque-string-max-255-chars>
```

**Semantics:**
- First request with a given key: processed normally.
- Subsequent requests with the **same key and identical body**: return the cached response immediately with `200 OK` (no duplicate processing).
- Subsequent requests with the **same key but different body**: return `409 Conflict` with `webhook.idempotency_conflict`.
- Keys expire after 24 hours (implementation: `webhook_calls` table TTL).

**Recommendation:** Use a UUID or hash of request content as the key. Re-send the exact same request body on retry.

---

## 7. Outbound Callbacks

Async LLM calls (`mode=async`) deliver results to the `callback_url` via HTTP POST.

### Delivery Guarantee

Callbacks are **at-least-once**. Receivers must be idempotent.

### Stable Headers

Every delivery attempt carries:

```
X-Webhook-Delivery-Id: <uuid>           -- stable across retries
X-Webhook-Signature: t=<unix>,v1=<hex> -- recomputed per attempt (timestamp differs)
Content-Type: application/json
User-Agent: goclaw-webhook/1
```

`X-Webhook-Delivery-Id` is stable for all retry attempts of the same call. Receivers **SHOULD** deduplicate by this ID within a window of at least 24 hours.

`X-Webhook-Signature` uses the **same HMAC algorithm** as inbound auth. Verify with the `hmac_signing_key` from the create response.

### Payload

```json
{
  "call_id": "<uuid>",
  "delivery_id": "<uuid>",
  "agent_id": "<uuid>",
  "status": "done",
  "output": "Agent response text...",
  "usage": {
    "prompt_tokens": 250,
    "completion_tokens": 220,
    "total_tokens": 470,
    "cache_read_input_tokens": 120,
    "cache_creation_input_tokens": 30,
    "prompt_tokens_include_cached_segments": true
  },
  "total_cost_usd": 0.0279,
  "calls": [
    {
      "type": "llm_call", "name": "9router/cx/gpt-5.6 #1",
      "provider": "9router", "model": "cx/gpt-5.6",
      "prompt_tokens": 150, "completion_tokens": 200, "total_tokens": 350,
      "cache_read_input_tokens": 120, "cache_creation_input_tokens": 30,
      "prompt_tokens_include_cached_segments": true, "cost_usd": 0.0187
    },
    {
      "type": "tool_call", "name": "read_image",
      "provider": "9router", "model": "cx/gpt-5.5",
      "prompt_tokens": 100, "completion_tokens": 20, "total_tokens": 120,
      "cost_usd": 0.0092
    }
  ],
  "metadata": {},
  "error": ""
}
```

> `calls[]` and `total_cost_usd` on the async callback follow the same semantics as
> the sync response above: `usage` is the sum of all `calls[]` (including
> tool-internal LLM calls), each call carries its own `provider`/`model`/tokens/`cost_usd`.

`status` is `"done"` on success, `"failed"` on agent error. `error` is non-empty on failure.

### Retry Schedule

| Attempt | Delay (±10% jitter) |
|---------|---------------------|
| 1 | 30 seconds |
| 2 | 2 minutes |
| 3 | 10 minutes |
| 4 | 1 hour |
| 5 | 6 hours |

After 5 failed attempts the row moves to `status=dead`. No further retries.

**`Retry-After` header:** If the receiver returns `429` with a `Retry-After` header, the worker respects it (capped at 6 hours).

**Permanent failure:** `4xx` responses (except `429`) are treated as permanent — no retry.

**Success:** Any `2xx` response marks the delivery as done.

### Verifying Outbound Signatures

```go
// Go — verify X-Webhook-Signature on your callback endpoint
import (
    "crypto/hmac"
    "crypto/sha256"
    "encoding/hex"
    "fmt"
    "net/http"
    "strconv"
    "strings"
    "time"
)

func verifyWebhookSignature(r *http.Request, body []byte, hmacSigningKey string) error {
    sigHeader := r.Header.Get("X-Webhook-Signature")
    // Parse "t=<unix>,v1=<hex>"
    var ts int64
    var sigHex string
    for _, part := range strings.Split(sigHeader, ",") {
        if strings.HasPrefix(part, "t=") {
            ts, _ = strconv.ParseInt(strings.TrimPrefix(part, "t="), 10, 64)
        }
        if strings.HasPrefix(part, "v1=") {
            sigHex = strings.TrimPrefix(part, "v1=")
        }
    }
    if ts == 0 || sigHex == "" {
        return fmt.Errorf("missing signature header fields")
    }
    // Verify timestamp skew
    if abs(time.Now().Unix()-ts) > 300 {
        return fmt.Errorf("timestamp skew too large")
    }
    // Decode HMAC key from hex
    key, err := hex.DecodeString(hmacSigningKey)
    if err != nil {
        return err
    }
    // Recompute HMAC
    payload := append([]byte(fmt.Sprintf("%d.", ts)), body...)
    mac := hmac.New(sha256.New, key)
    mac.Write(payload)
    expected := mac.Sum(nil)
    // Decode received sig
    received, err := hex.DecodeString(sigHex)
    if err != nil || !hmac.Equal(expected, received) {
        return fmt.Errorf("signature mismatch")
    }
    return nil
}
```

---

## 8. Channel Capability Matrix

Relevant for `POST /v1/webhooks/message` with `media_url`.

| Channel Type | Text | Media |
|--------------|------|-------|
| `telegram` | yes | yes |
| `discord` | yes | yes |
| `whatsapp` | yes | yes |
| `feishu` | yes | yes |
| `slack` | yes | yes |
| `zalo_personal` | yes | yes |
| `pancake` | yes | yes |
| `facebook` | yes | yes |
| `zalo_oa` | yes | no |

When `media_url` is sent to a non-media-capable channel:
- `fallback_to_text=true` → text content delivered, `warning` field set
- `fallback_to_text=false` (default) → `501 Not Implemented`

---

## 9. Rate Limits

Rate limiting is two-tier:

| Tier | Cap | Notes |
|------|-----|-------|
| Per-webhook | `rate_limit_per_min` field (0 = disabled) | Configured per webhook row |
| Per-tenant | Platform default (configurable) | Applies across all webhooks for a tenant |

Both tiers must pass. If either rejects the request, `429 Too Many Requests` is returned with `Retry-After: 60`.

---

## 10. Edition Differences

| Feature | Standard | Lite |
|---------|----------|------|
| `/v1/webhooks/llm` | Available | Available (localhost_only forced) |
| `media[]` on `/v1/webhooks/llm` | Available | Available |
| `/v1/webhooks/message` | Available | Disabled |
| `localhost_only=false` | Configurable | Always true; cannot be unset |
| `kind="message"` webhook creation | Allowed | Rejected (403) |

On Lite, all webhooks are automatically created with `localhost_only=true` regardless of the request field. Attempting to unset `localhost_only` via PATCH returns `403`.

`media[]` is not edition-gated: it adds no channel dependency. Note that `localhost_only`
restricts who may **call** the webhook, not where the media may be hosted — a Lite caller on
localhost can still reference a public CDN URL, and a private-network media host is refused on
both editions.

---

## 11. Security

### SSRF Protection

- `media_url` in message webhooks: validated against SSRF policy + HEAD-probed before fetch.
- `media[].url` in LLM webhooks: validated before the fetch, and the resolved destination IP is re-checked at every redirect hop's dial, so a redirect into a private range or a DNS rebind is refused mid-fetch. Only the failure category reaches the caller — never a host, IP, port, or error string.
- `callback_url` in async LLM webhooks: validated at enqueue time and re-validated at delivery time (prevents DNS rebinding attacks).
- Log event: `security.webhook.ssrf_blocked` / `security.webhook.callback_ssrf_blocked`.

### Secret Storage

Secrets are never stored in plaintext. Only `SHA-256(secret)` is kept in the database. Secrets are never logged.

### HMAC Timestamp Skew

Requests with `|now - t| > 300 seconds` are rejected immediately (before any DB lookup) to prevent replay attacks.

### Tenant Isolation

- Agent must belong to the webhook's tenant.
- Channel must belong to the webhook's tenant (or be a legacy config-based channel).
- Log events: `security.webhook.tenant_mismatch`, `security.webhook.tenant_leak_attempt`.

### Secret Rotation

**No grace window.** The old secret is invalidated immediately when `POST /v1/webhooks/{id}/rotate` completes. Coordinate with callers before rotating in production.

---

## 12. HMAC Receiver Examples

### curl (signing with openssl)

```bash
WEBHOOK_HMAC_KEY="a3f4...your_hmac_signing_key_hex"
WEBHOOK_ID="your-webhook-uuid"
BODY='{"input":"hello","mode":"sync"}'
TS=$(date +%s)
PAYLOAD="${TS}.${BODY}"
SIG=$(echo -n "$PAYLOAD" | openssl dgst -sha256 -mac HMAC \
      -macopt "hexkey:${WEBHOOK_HMAC_KEY}" | awk '{print $2}')

curl -X POST https://example.com/v1/webhooks/llm \
  -H "Content-Type: application/json" \
  -H "X-Webhook-Id: ${WEBHOOK_ID}" \
  -H "X-GoClaw-Signature: t=${TS},v1=${SIG}" \
  -d "$BODY"
```

### curl (bearer auth)

```bash
curl -X POST https://example.com/v1/webhooks/llm \
  -H "Authorization: Bearer wh_ABCDEFGHIJKLMNOPQRSTUVWXYZ234567ABCDEFGH" \
  -H "Content-Type: application/json" \
  -d '{"input":"hi","mode":"sync"}'
```

### Node.js (HMAC signing)

```js
const crypto = require('crypto');

function signWebhookRequest(body, hmacSigningKeyHex) {
  const ts = Math.floor(Date.now() / 1000);
  const keyBytes = Buffer.from(hmacSigningKeyHex, 'hex');
  const payload = Buffer.concat([
    Buffer.from(`${ts}.`),
    Buffer.isBuffer(body) ? body : Buffer.from(body),
  ]);
  const sig = crypto.createHmac('sha256', keyBytes).update(payload).digest('hex');
  return { ts, signature: `t=${ts},v1=${sig}` };
}

// Usage
const body = JSON.stringify({ input: 'hello', mode: 'sync' });
const { signature } = signWebhookRequest(body, process.env.WEBHOOK_HMAC_KEY);

await fetch('https://example.com/v1/webhooks/llm', {
  method: 'POST',
  headers: {
    'Content-Type': 'application/json',
    'X-Webhook-Id': process.env.WEBHOOK_ID,
    'X-GoClaw-Signature': signature,
  },
  body,
});
```

### Python (HMAC signing)

```python
import hashlib
import hmac
import json
import time
import requests

def sign_webhook(body: bytes, hmac_signing_key_hex: str) -> str:
    ts = int(time.time())
    key = bytes.fromhex(hmac_signing_key_hex)
    payload = f"{ts}.".encode() + body
    sig = hmac.new(key, payload, hashlib.sha256).hexdigest()
    return f"t={ts},v1={sig}"

body = json.dumps({"input": "hello", "mode": "sync"}).encode()
signature = sign_webhook(body, os.environ["WEBHOOK_HMAC_KEY"])

requests.post(
    "https://example.com/v1/webhooks/llm",
    headers={
        "Content-Type": "application/json",
        "X-Webhook-Id": os.environ["WEBHOOK_ID"],
        "X-GoClaw-Signature": signature,
    },
    data=body,
)
```

---

## 13. Audit Payload Shape (`webhook_calls.request_payload`)

Every call creates a row in `webhook_calls` with a `request_payload` column (`jsonb` on PostgreSQL, `TEXT` on SQLite). The canonical shape is:

```json
{
  "body_hash": "<sha256-hex-64-chars>",
  "meta": { ... handler-specific fields ... }
}
```

### `body_hash`

SHA-256 hex digest of the raw request body bytes. Used by the idempotency subsystem to detect body-mismatch replays (same `Idempotency-Key`, different body → 409 Conflict).

### `meta` by handler

**`POST /v1/webhooks/llm`** — meta mirrors the decoded request fields:

```json
{
  "input": "<raw JSON — string or message array>",
  "session_key": "optional-key",
  "user_id": "optional-uid",
  "model": "optional-override",
  "mode": "sync",
  "callback_url": "",
  "media": [{"url": "https://cdn.example.com/shot.png", "filename": "shot.png"}],
  "metadata": null
}
```

`media[].url` is stored with its **query string and userinfo stripped**. A presigned URL's
signature is a bearer credential and this column is readable via
`GET /v1/webhooks/{id}/calls/{callId}` for the full 30-day retention window. The same redaction
is applied to the media tag the agent sees and to every log line. `body_hash` is computed over
the raw body, so idempotency still sees the original request.

**`POST /v1/webhooks/message`** — meta contains delivery context:

```json
{
  "channel_name": "telegram-main",
  "chat_id": "123456789",
  "has_media": false
}
```

### Notes

- `body_hash` is always exactly 64 lowercase hex characters. Any stored value that does not match this format is treated as "no hash" by the idempotency checker (fail-closed).
- External consumers reading `request_payload` via SQL should parse it as JSON, not as raw bytes.
- Shape is stable across LLM and message handler calls — only `meta` contents differ.

---

## 14. Encryption at Rest

### Raw Secret Encryption

The webhook secret is encrypted at rest using AES-256-GCM, keyed by the environment variable `GOCLAW_ENCRYPTION_KEY` (required for webhook HMAC auth to work). Only the database stores encrypted secret material.

**Key contract (POST /v1/webhooks create/rotate response):**

```json
{
  "secret": "wh_ABCDEFGHIJKLMNOPQRSTUVWXYZ234567ABCDEFGH",
  "hmac_signing_key": "a3f4...hex64chars"
}
```

- `secret` — Raw bearer token in plaintext. Clients **must store securely** on their end; the gateway will not retrieve it again.
- `hmac_signing_key` — Derived as `hex(SHA-256(secret))`. This is also returned once and should be stored securely by clients.

**Database storage:**

- `webhooks.secret_hash` column: `SHA-256(secret)` in hex. Used for bearer auth lookups (constant-time comparison).
- `webhooks.encrypted_secret` column (PG/SQLite): AES-256-GCM encrypted raw secret. Used to support lease-token reclamation and idempotency recovery on stale calls.
- Environment variable `GOCLAW_ENCRYPTION_KEY` — required for webhook processing. Same key also encrypts LLM provider API keys. Format: base64-encoded 32-byte key.

**Migration notes:**

- PostgreSQL: Migration `000058` added `encrypted_secret` column.
- SQLite (Lite edition): Schema v28 includes encrypted secret support.

**DB compromise impact:**

A database-layer attacker with read-only access to `webhooks` table **cannot** derive the raw secret or `hmac_signing_key`:
- `secret_hash` alone does not reverse-engineer the secret (cryptographic hash).
- `encrypted_secret` requires `GOCLAW_ENCRYPTION_KEY` to decrypt (environment-only, not in database).
- Attackers gain no actionable HMAC material.

### Environment Variable Security

`GOCLAW_ENCRYPTION_KEY` must be:
- Stored securely (e.g., sealed in a secret manager, not in `config.json`).
- Same across all gateway instances in a cluster (standard multi-replica key).
- Rotated as part of incident response — rotation requires re-encrypting all webhook secrets (automated migration).

---
