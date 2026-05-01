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

- Models: `gemini-3.1-flash-tts-preview` (default), `gemini-2.5-flash-preview-tts`, `gemini-2.5-pro-preview-tts` (preview).
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

## Image Generation

Native `image_generation` support in the Codex provider (`POST /codex/responses`) + passthrough in the OpenAI-compat path.

**Provider flag:** `ProviderCapabilities.ImageGeneration bool` (`internal/providers/capabilities.go`). Codex sets `true`; other providers default `false`.

**Gate (agent loop):** `ToolDefinition{Type:"image_generation"}` appended iff (provider capability) AND (`AgentConfig.AllowImageGeneration`, default true) AND (request lacks `x-goclaw-no-image-gen` header). Gate logic in `internal/agent/loop_tool_filter.go`.

**Codex native events** (`internal/providers/codex.go`):
- `response.image_generation_call.partial_image` → `ChatResponse.Images` entry with `Partial:true`.
- `response.output_item.done` with `item.type == "image_generation_call"` → final `ChatResponse.Images` entry; partial frames for same `item_id` replaced.
- `response.completed` walks `response.output[]` for image items (non-stream).

**OpenAI-compat parsing:** `choices[0].message.images[]` + `choices[0].delta.images[]` with `data:image/...;base64,...` URLs decoded in `internal/providers/openai_http.go` and `internal/providers/openai_chat.go`. Helper: `parseDataURL()` in `internal/providers/openai_image_url.go`.

**Persistence:** `internal/agent/media.go persistAssistantImages()` writes final images to `{workspace}/media/{sha256}.{ext}`, returns `MediaRef` entries, clears inline `Images[]`. Idempotent on hash. Invoked from `pipeline.FinalizeStage` via `Deps.PersistAssistantImages` callback.

**Web UI:** Download filename resolver (`imageGenDownloadName`) in `ui/web/src/components/chat/media-gallery.tsx`. Image generation works automatically when the agent has the `create_image` tool — no user-facing toggle.

---

## Key Conventions

- **Store layer:** Interface-based; PG (`store/pg/`) + SQLite (`store/sqlitestore/`). Raw SQL, `$1/$2` params.
- **Session token display:** v3 compaction now uses dynamic max_tokens (`in/25` clamped `[1024,8192]`); session token display reads from `sessions.metadata.last_prompt_tokens` and `last_message_count`. Tool schemas counted via `TokenCounter.CountToolSchemas()` and included in ContextStage overhead.
- **Context propagation:** `store.WithLocale`, `store.WithUserID`, `store.WithTenantID`, etc.
- **Security logs:** `slog.Warn("security.*")` for all security events.
- **SSRF prevention:** `validateProviderURL()` in `internal/http/tts_validate.go`.
- **i18n keys:** Add to `internal/i18n/keys.go` + 3 catalog Go files; UI strings in 3 locale JSON dirs.
- **Migrations:** PG (`migrations/`) + SQLite (`store/sqlitestore/schema.go`) — always update both.
