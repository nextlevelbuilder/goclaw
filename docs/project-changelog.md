# Project Changelog

Significant changes, features, and fixes in reverse chronological order.

---

## 2026-04-22

### Providers: Native image generation for Codex + OpenAI-compat

**Features**

- **Codex native track:** `CodexProvider` now attaches the `image_generation` tool object to `POST /codex/responses` when the agent permits it. Streams `response.image_generation_call.partial_image` intermediate frames + `response.output_item.done` (type `image_generation_call`) final images; non-stream path walks `response.output[]`. Deduped per `item_id`, partial frames emitted as `ImageContent{Partial:true}` for UI progressive render.
- **OpenAI-compat track:** `tools[]` serializer passes `{type:"image_generation"}` entries through natively; response parser reads `choices[0].message.images[]` / `choices[0].delta.images[]` (data URLs) into `ChatResponse.Images`.
- **Media persistence:** `internal/agent/media.go` `persistAssistantImages()` writes final images to `{workspace}/media/{sha256}.{ext}`, returns `MediaRef` entries, clears inline base64. Idempotent on hash. Wired via `pipeline.Deps.PersistAssistantImages` callback from `FinalizeStage`. Partial frames skipped.
- **Capabilities + gate:** `ProviderCapabilities.ImageGeneration` flag, set true on Codex provider. Tri-level gate in agent loop: provider capability AND `AgentConfig.AllowImageGeneration` (read from `other_config.allow_image_generation`, default true) AND request not opted-out via `x-goclaw-no-image-gen` header.
- **Web UI:** Composer "Images" toggle chip (visible only when provider supports image gen, per-agent persistence in localStorage). Streaming placeholder skeleton in `ActiveRunZone` while partials arrive. `MediaGallery` assigns `generated-{timestamp}.png` filename for assistant-generated PNGs.

**Wire format**

Implementation is evidence-backed against the native ChatGPT Responses API event shape, not the compat shim shape. Research notes in `plans/reports/`.

**i18n**

- 1 UI key (`imageGenDownloadName`) in `ui/web/src/i18n/locales/{en,vi,zh}/chat.json` — download filename for generated images.

**Tests**

- Unit tests across providers (Codex native + OpenAI-compat), agent media persistence, store config. Full test sweep: 2618 pass.

**Internal docs**

- `plans/260422-1349-goclaw-chatgpt-image-gen/` — plan + phase files.
- `plans/reports/researcher-260422-1414-codex-native-image-events.md` — native event schema.

---

## 2026-04-20

### Tools: `send_file` — explicit workspace file delivery

**Features**

- **`send_file` tool** (`internal/tools/send_file.go`): dedicated tool for sending existing workspace files as chat attachments. Takes `path` (required) and `caption` (optional). Replaces implicit `message(MEDIA:path)` convention for re-delivering already-created files. Marks `DeliveredMedia` on success to prevent duplicate delivery.
- **`DeliveredMedia` mark on `message(MEDIA:)` sends** (`internal/tools/message.go`): patched to call `IsDelivered` / mark after successful MEDIA upload — closes the cross-tool duplicate-delivery gap where a file sent via `message(MEDIA:)` was not tracked and could be re-sent by `send_file`.
- Registered as builtin tool in `cmd/gateway_tools_wiring.go` and seeded in `cmd/gateway_builtin_tools.go`.

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
