# Project Changelog

Significant changes, features, and fixes in reverse chronological order.

---

## 2026-04-21

### Webhook fixes (post-review security & idempotency hardening)

**Fixes**

- **K1: Auth context isolation** — Webhook auth middleware now resolves secret/HMAC signature before tenant injection (eliminating 401 due to tenant scope applied too early). Unscoped store methods `GetByHashUnscoped` + `GetByIDUnscoped` added to WebhookStore interface.
- **K7: IP allowlist enforcement** — Inbound webhook calls now check `ip_allowlist` field (CIDR + exact IP) after bearer/HMAC auth. Empty list = allow all (back-compat). Rejected requests return HTTP 403 with log `security.webhook.ip_denied`.
- **K8: HMAC replay protection** — Per-process nonce cache (key = `sha256(tenant_id + "|" + signature_hex)`) with 320s TTL rejects duplicate signatures within the skew window. Single-node caveat documented. Log: `security.webhook.hmac_replay`.
- **K2: `request_payload` canonical shape** — All webhook audit rows now store `{"body_hash":"<hex64>","meta":{...}}` JSON instead of raw bytes. Idempotency checker compares body hashes to detect replays with different payloads (409 Conflict).
- **K3: Body hash extraction** — `extractBodyHash()` now parses canonical audit payload structure (previously had parsing bugs leading to missed hash validation).
- **K9: Invariant test column fix** — Webhook tenant isolation test now references correct schema columns (`encrypted_secret`, `lease_token`).
- **K4: Worker slot drain** — Fixed channel leak in webhook worker that prevented slot release on successful claims. Concurrency now scales properly under load.
- **K5: Lease-token CAS on UpdateStatus** — Stale webhook receivers can no longer overwrite delivery status. Status updates use optimistic concurrency on `lease_token` (UUID), ensuring only the owning worker can mark the call done. Prevents duplicate delivery from slow receivers.
- **K6: HMAC signing key encryption** — Raw secret (from which `hmac_signing_key = hex(SHA-256(secret))` is derived) is now encrypted at rest via AES-256-GCM using `GOCLAW_ENCRYPTION_KEY`. Database compromise no longer = HMAC key compromise. Clients receive plaintext secret once (create/rotate response) and must store securely.
- **K10: Shared rate limiter instance** — Fixed duplicate `webhookLimiter` instantiation causing doubled RPM enforcement. Single limiter now shared across all webhook endpoints.

**Migrations**

- PostgreSQL: Migration `000057` adds `lease_token` column to `webhook_calls`. Migration `000058` adds `encrypted_secret` column to `webhooks`.
- SQLite: Schema v28 includes both new columns.

**Docs**

- `docs/webhooks.md`: Section 3 clarified bearer/HMAC auth contract + IP allowlist behavior. New Section 14 explains encryption at rest, key contract, DB compromise boundary.
- `docs/00-architecture-overview.md`: Section 12 (Webhook Subsystem) updated to mention lease-token CAS semantics and secret encryption.

**Environment**

- `GOCLAW_ENCRYPTION_KEY` is now **required** for webhook HMAC auth. Same key also encrypts LLM provider credentials.

---

## 2026-04-19

### TTS: Gemini provider + ProviderCapabilities schema engine

**Features**

- **Gemini TTS provider** (`internal/audio/gemini/`): supports `gemini-2.5-flash-preview-tts` and `gemini-2.5-pro-preview-tts`. 30 prebuilt voices, 70+ languages, multi-speaker mode (up to 2 simultaneous speakers with distinct voices), audio-tag styling, WAV output via PCM-to-WAV conversion.
- **`ProviderCapabilities` schema** (`internal/audio/capabilities.go`): dynamic per-provider param descriptor. Each provider exposes `Capabilities()` returning `[]ParamSchema` (type, range, default, dependsOn conditions, hidden flag) + `CustomFeatures` flags. UI reads `GET /v1/tts/capabilities` and renders param editors without hard-coded field lists.
- **Dual-read TTS storage**: tenant config read from both legacy flat keys (`tts.provider`, `tts.voice_id`, …) and new params blob (`tts.<provider>.params` JSON). Blob wins on conflict. Allows gradual migration; no data loss on downgrade.
- **`VoiceListProvider` interface** refactor: dynamic voice fetching (ElevenLabs, MiniMax) now via `ListVoices(ctx, ListVoicesOptions)` instead of per-provider ad-hoc methods. Unified `audio.Voice` type.
- **`POST /v1/tts/test-connection`**: ephemeral provider creation from request credentials + short synthesis smoke test. Returns `{ success, latency_ms }`. No provider registration; no config mutation. Operator role required.
- **`GET /v1/tts/capabilities`**: returns `ProviderCapabilities` JSON for all registered providers.

**i18n**

- Backend sentinel error keys (`MsgTtsGeminiInvalidVoice`, `MsgTtsGeminiInvalidModel`, `MsgTtsGeminiSpeakerLimit`, `MsgTtsParamOutOfRange`, `MsgTtsParamDependsOn`, `MsgTtsMiniMaxVoicesFailed`) in all 3 catalogs (EN/VI/ZH).
- HTTP 422 responses for Gemini sentinel errors now use `i18n.T(locale, key, args...)` — locale from `Accept-Language` header.
- ~80 param `label`/`help` keys across web + desktop locale files (EN/VI/ZH); parity enforced by `ui/web/src/__tests__/i18n-tts-key-parity.test.ts`.

**Security**

- SSRF guard on `api_base` override for test-connection (`validateProviderURL()`) — blocks `127.0.0.1` / `localhost` / RFC1918 ranges.

**Docs**

- `docs/tts-provider-capabilities.md` — schema reference + per-provider param tables + storage format + "Adding a new provider" checklist.
- `docs/codebase-summary.md` — TTS subsystem section documenting manager, providers, storage, endpoints.
