# Codebase Summary

High-level map of GoClaw modules and key cross-cutting concerns.
For system design see `docs/00-architecture-overview.md`; for API contract see `docs/18-http-api.md`.

---

## Module Map

| Path | Purpose |
|------|---------|
| `cmd/` | CLI (cobra): `serve`, `onboard`, `migrate` commands |
| `internal/agent/` | Agent loop (think→act→observe), router, input guard |
| `internal/audio/` | TTS provider layer (see §TTS below) |
| `internal/bootstrap/` | SOUL/IDENTITY system prompts + per-user seed |
| `internal/channels/` | Telegram, Feishu, Zalo, Discord, WhatsApp connectors |
| `internal/config/` | JSON5 config loading + env overlay |
| `internal/crypto/` | AES-256-GCM encryption for API keys |
| `internal/gateway/` | WS + HTTP server, client, method router |
| `internal/http/` | HTTP API handlers (`/v1/*`) |
| `internal/i18n/` | Backend message catalog (EN/VI/ZH) + `T(locale, key, args)` |
| `internal/memory/` | pgvector 3-tier memory system |
| `internal/mcp/` | Model Context Protocol bridge |
| `internal/permissions/` | RBAC: admin / operator / viewer |
| `internal/pipeline/` | 8-stage agent pipeline |
| `internal/providers/` | LLM providers (Anthropic, OpenAI-compat, Qwen, Claude CLI) |
| `internal/store/` | Store interfaces + PG + SQLite implementations |
| `internal/tools/` | Tool registry (filesystem, exec, web, MCP, delegate) |
| `internal/tts/` | Back-compat alias package for old import paths |
| `internal/vault/` | Knowledge Vault: wikilinks, hybrid search, FS sync |
| `migrations/` | PostgreSQL migration files |
| `ui/web/` | React SPA (Vite, Tailwind, Radix UI, Zustand) |
| `ui/desktop/` | Wails v2 desktop app (SQLite, embedded gateway) |

---

## TTS Subsystem

### Overview

TTS lives in `internal/audio/` (canonical) with a back-compat alias at `internal/tts/`.
Five providers ship out-of-the-box: OpenAI, ElevenLabs, Edge TTS, MiniMax, Gemini.

### Provider Capabilities Schema

Each provider implements `audio.TTSProvider` and optionally `audio.CapabilitiesProvider`:

```go
type CapabilitiesProvider interface {
    Capabilities() ProviderCapabilities
}

type ProviderCapabilities struct {
    Params         []ParamSchema
    CustomFeatures CustomFeatures
}
```

`ParamSchema` carries `Key`, `Type` (string/number/bool/select/textarea), `Label`, `Help`, `Default`,
`Min`/`Max`, `Options` (for select), `DependsOn` (all-of conditions), and `Hidden` flag.
The UI reads `GET /v1/tts/capabilities` and renders per-provider param editors dynamically.
See `docs/tts-provider-capabilities.md` for full schema documentation.

### Providers

| Provider | Package | Voice source | Key param |
|----------|---------|--------------|-----------|
| OpenAI | `internal/audio/openai/` | Static list | `speed`, `response_format`, `instructions` |
| ElevenLabs | `internal/audio/elevenlabs/` | Dynamic (API) | `voiceSettings.*`, `seed`, `language_code` |
| Edge TTS | `internal/audio/edge/` | Static list | `rate`, `pitch`, `volume` |
| MiniMax | `internal/audio/minimax/` | Dynamic (API) | `speed`, `vol`, `pitch`, `emotion`, `audio.*` |
| Gemini | `internal/audio/gemini/` | Static list (30) | multi-speaker, audio tags, 70+ languages |

### Storage: Dual-Read

Tenant TTS config is stored in `system_config` with two complementary strategies:
- **Legacy flat keys** (`tts.provider`, `tts.voice_id`, `tts.api_key`, …) — written by all versions.
- **Params blob** (`tts.<provider>.params` JSON) — written by v3.x+ for per-provider param overrides.

On read, both paths are merged; blob wins on key conflict. This allows gradual migration:
old clients see flat keys; new clients see the full params blob.

### Manager

`audio.Manager` is the central registry:
- `RegisterProvider(p TTSProvider)` — replaces any existing provider by the same name.
- `Primary()` — returns current primary provider name from config.
- `Synthesize(ctx, text, opts)` — delegates to active provider.
- `ListVoices(ctx, opts)` — delegates to `VoiceListProvider` if implemented.

### HTTP Endpoints

| Endpoint | Role | Notes |
|----------|------|-------|
| `POST /v1/tts/synthesize` | operator | Streams WAV response |
| `POST /v1/tts/test-connection` | operator | Ephemeral provider, returns latency |
| `GET /v1/tts/capabilities` | operator | ProviderCapabilities JSON per provider |
| `GET /v1/tts/config` | admin | Tenant TTS config |
| `POST /v1/tts/config` | admin | Save tenant TTS config |

### Gemini Specifics

- Models: `gemini-2.5-flash-preview-tts`, `gemini-2.5-pro-preview-tts` (preview).
- Multi-speaker: up to 2 simultaneous speakers, each with distinct voice + name annotation.
- Audio tags: inline `<say-as>` / style directives via bracketed prompts.
- Sentinel errors: `ErrInvalidVoice`, `ErrInvalidModel`, `ErrSpeakerLimit` → HTTP 422 with i18n message.
- SSRF guard: `api_base` override validated by `validateProviderURL()` — blocks 127.0.0.1/localhost.

### i18n

Backend validation errors use `i18n.T(locale, key, args...)` pattern.
Locale is extracted from `Accept-Language` HTTP header by `enrichContext` middleware.

UI param labels/help text live in:
- `ui/web/src/i18n/locales/{en,vi,zh}/tts.json`
- `ui/desktop/frontend/src/i18n/locales/{en,vi,zh}/tts.json`

Parity enforced by `ui/web/src/__tests__/i18n-tts-key-parity.test.ts` (vitest).

---

## Webhook Subsystem

External systems invoke agents or send channel messages via webhooks without gateway tokens.

### Components

| Path | Purpose |
|------|---------|
| `internal/http/webhooks_admin.go` | CRUD handlers (create, list, get, patch, rotate, revoke) |
| `internal/http/webhooks_auth.go` | Bearer + HMAC signature verification, IPAllowlist, tenant scope |
| `internal/http/webhooks_nonce.go` | Per-process HMAC replay cache (320s TTL) |
| `internal/http/webhooks_llm.go` | `POST /v1/webhooks/llm` endpoint (sync 30s / async) |
| `internal/http/webhooks_message.go` | `POST /v1/webhooks/message` endpoint (channel delivery) |
| `internal/http/webhooks_ratelimit.go` | Per-webhook + per-tenant rate limiting |
| `internal/http/webhooks_idempotency.go` | `Idempotency-Key` header dedup cache (24h TTL) |
| `internal/http/webhooks_media_fetch.go` | SSRF-guarded media URL fetch + MIME validation |
| `internal/webhooks/worker.go` | Async callback poller + delivery goroutines |
| `internal/webhooks/backoff.go` | Exponential retry schedule `[30s, 2m, 10m, 1h, 6h]` |
| `internal/webhooks/sign.go` | HMAC-SHA256 signing for outbound callbacks |
| `internal/webhooks/limiter.go` | Shared rate limiter for callback delivery |
| `internal/store/webhook_store.go` | `WebhookStore` interface + `WebhookCallStore` |
| `internal/store/pg/webhook_store.go` | PostgreSQL implementation (tenant-scoped) |
| `internal/store/sqlitestore/webhook_store.go` | SQLite implementation (Lite edition) |
| `migrations/` | PG migrations 000056–000058 (webhooks + lease token + encrypted secret) |

### Auth Flow

1. **Bearer auth**: Hash the token, lookup `secret_hash` globally (via `GetByHashUnscoped`) → return webhook + tenantID.
2. **HMAC auth**: Parse `X-Webhook-Id` header, lookup webhook globally → verify signature timestamp + nonce.
3. **Tenant inject**: Re-scope context with webhook's tenantID for all downstream calls.
4. **IP allowlist**: If non-empty, check request source IP (CIDR or exact) against list. Empty = allow all.
5. **Rate limit**: Check per-webhook + per-tenant buckets. Either rejects = 429.

### Idempotency & Lease Tokens

- **Inbound**: `Idempotency-Key` header dedup (24h cache). Same key + same body = cached response; same key + different body = 409 Conflict.
- **Outbound**: Each `webhook_calls` row has `lease_token` (UUID). Worker claims row with CAS. On update, token proves ownership — prevents stale receivers from overwriting.

### Secret Encryption

Raw webhook secret encrypted at rest via AES-256-GCM using `GOCLAW_ENCRYPTION_KEY` (same as LLM provider keys).
- Database: stores `encrypted_secret` column + `secret_hash` (for bearer lookups).
- DB compromise does not leak HMAC material.
- Clients receive plaintext secret once (create/rotate response) — must store securely.

### Audit Payload

All webhook calls logged with canonical `{"body_hash":"<sha256-hex>","meta":{...}}` shape in `webhook_calls.request_payload` (JSON).
Used by idempotency checker to detect body mismatches on replay.

---

## Key Conventions

- **Store layer:** Interface-based; PG (`store/pg/`) + SQLite (`store/sqlitestore/`). Raw SQL, `$1/$2` params.
- **Context propagation:** `store.WithLocale`, `store.WithUserID`, `store.WithTenantID`, etc.
- **Security logs:** `slog.Warn("security.*")` for all security events.
- **SSRF prevention:** `validateProviderURL()` in `internal/http/tts_validate.go`.
- **i18n keys:** Add to `internal/i18n/keys.go` + 3 catalog Go files; UI strings in 3 locale JSON dirs.
- **Migrations:** PG (`migrations/`) + SQLite (`store/sqlitestore/schema.go`) — always update both.
