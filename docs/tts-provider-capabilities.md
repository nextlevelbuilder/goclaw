# TTS Provider Capabilities Schema

Reference for the `ProviderCapabilities` schema, per-provider parameter tables,
storage format, and the checklist for adding a new TTS provider.

---

## 1. ProviderCapabilities Schema

### Go Types

```go
// ProviderCapabilities describes everything the frontend needs to render
// dynamic per-provider settings and validate user input.
type ProviderCapabilities struct {
    Provider       string            // provider ID: "openai", "gemini", etc.
    DisplayName    string            // human-readable name
    RequiresAPIKey bool              // show API key field
    Models         []string          // static model list; nil = user freetext
    Voices         []VoiceOption     // static voices; nil = dynamic or none
    Params         []ParamSchema     // ordered list of configurable params
    CustomFeatures map[string]any    // provider-specific UI flags
}

// ParamSchema describes one configurable parameter.
type ParamSchema struct {
    Key         string       // dot-delimited storage key, e.g. "voice_settings.stability"
    Type        ParamType    // "range" | "integer" | "boolean" | "enum" | "string" | "text"
    Label       string       // English label (UI falls back to this if i18n key missing)
    Description string       // English help text (optional)
    Default     any          // default value (nil = omit from request)
    Min         *float64     // range/integer lower bound
    Max         *float64     // range/integer upper bound
    Step        *float64     // slider increment
    Enum        []EnumOption // allowed values for "enum" type
    DependsOn   []Dependency // show/hide rules
}

// Dependency expresses a show/hide condition on another param's value.
type Dependency struct {
    Field string // key of the controlling param
    Op    string // "eq" (only operator currently supported)
    Value string // required value (string-compared after JSON marshal)
}

// EnumOption is one choice inside an enum param.
type EnumOption struct {
    Value string
    Label string
}
```

### ParamType values

| Constant | Wire value | UI widget |
|---|---|---|
| `ParamTypeRange` | `"range"` | Slider + numeric input |
| `ParamTypeInteger` | `"integer"` | Integer slider / stepper |
| `ParamTypeBoolean` | `"boolean"` | Checkbox / toggle |
| `ParamTypeEnum` | `"enum"` | Select dropdown |
| `ParamTypeString` | `"string"` | Single-line text input |
| `ParamTypeText` | `"text"` | Multi-line textarea |

### DependsOn AND semantics

Multiple `DependsOn` entries are evaluated with **AND** logic: the param is
shown only when **all** conditions are satisfied simultaneously.

```go
// Shown only when format=="mp3":
DependsOn: []audio.Dependency{
    {Field: "audio.format", Op: "eq", Value: "mp3"},
}

// Shown only when provider=="gemini" AND mode=="multi":
DependsOn: []audio.Dependency{
    {Field: "provider", Op: "eq", Value: "gemini"},
    {Field: "mode",     Op: "eq", Value: "multi"},
}
```

### CustomFeatures flags

| Key | Type | Meaning |
|---|---|---|
| `multi_speaker` | bool | Provider supports multi-speaker mode (Gemini) |
| `audio_tags` | bool | Provider supports inline audio expression tags (Gemini) |
| `voices_dynamic` | bool | Voices must be fetched at runtime via `VoiceListProvider` (MiniMax) |

### Read-only contract

`Capabilities()` **must** be a pure function — no side effects, no I/O, no
locking. The manager calls it during startup to populate the catalog and again
on each `GET /v1/tts/capabilities` request.

---

## 2. Per-Provider Parameter Tables

### OpenAI (`internal/audio/openai/`)

| Key | Type | Default | Range / Options | DependsOn |
|---|---|---|---|---|
| `speed` | range | 1.0 | 0.25–4.0, step 0.05 | — |
| `response_format` | enum | `"mp3"` | mp3, opus, aac, flac, wav, pcm | — |
| `instructions` | text | `""` | — | `model == "gpt-4o-mini-tts"` |

### ElevenLabs (`internal/audio/elevenlabs/`)

| Key | Type | Default | Range / Options | DependsOn |
|---|---|---|---|---|
| `voice_settings.stability` | range | 0.5 | 0–1, step 0.01 | — |
| `voice_settings.similarity_boost` | range | 0.75 | 0–1, step 0.01 | — |
| `voice_settings.style` | range | 0.0 | 0–1, step 0.01 | — |
| `voice_settings.use_speaker_boost` | boolean | true | — | — |
| `voice_settings.speed` | range | 1.0 | 0.7–1.2, step 0.01 | — |
| `apply_text_normalization` | enum | `""` (auto) | auto, on, off | — |
| `seed` | integer | 0 | 0–2³²−1 | — |
| `optimize_streaming_latency` | range | 0 | 0–4, step 1 | — |
| `language_code` | string | `""` | ISO 639-1 | — |

### Edge TTS (`internal/audio/edge/`)

No API key required. Full voice list available via `edge-tts --list-voices`.

| Key | Type | Default | Range / Options | DependsOn |
|---|---|---|---|---|
| `rate` | integer | 0 | -50–+100 (%) | — |
| `pitch` | integer | 0 | -50–+50 (Hz) | — |
| `volume` | integer | 0 | -50–+100 (%) | — |

### MiniMax (`internal/audio/minimax/`)

Voices are dynamic — fetched at runtime via `VoiceListProvider`.
`CustomFeatures["voices_dynamic"] = true` signals the frontend to call
`GET /v1/voices?provider=minimax`.

| Key | Type | Default | Range / Options | DependsOn |
|---|---|---|---|---|
| `speed` | range | 1.0 | 0.5–2.0, step 0.1 | — |
| `vol` | range | 1.0 | 0.01–10.0, step 0.01 | — |
| `pitch` | integer | 0 | -12–+12 (semitones) | — |
| `emotion` | enum | `""` | none/happy/sad/angry/fearful/disgusted/surprised/neutral/excited/anxious | — |
| `text_normalization` | boolean | nil (omit) | — | — |
| `audio.format` | enum | `"mp3"` | mp3, pcm, flac, wav | — |
| `audio.sample_rate` | enum | `""` (default) | 8000/16000/22050/24000/32000/44100 | — |
| `audio.bitrate` | enum | `""` (default) | 32000/64000/128000/256000 bps | `audio.format == "mp3"` |
| `audio.channel` | enum | `""` (default) | 1=mono, 2=stereo | — |

### Gemini (`internal/audio/gemini/`)

No configurable `Params` — advanced features exposed via `CustomFeatures`.

| Feature | CustomFeatures key | Notes |
|---|---|---|
| Multi-speaker | `multi_speaker: true` | Up to 2 speakers; each has `name` + `voice` |
| Audio tags | `audio_tags: true` | Inline `[laughs]`, `[sighs]`, etc. |
| Preview badge | — | Models are preview-stage; UI shows badge |

Sentinel validation errors (422 Unprocessable Entity):

| Error | i18n key |
|---|---|
| `ErrInvalidVoice` | `MsgTtsGeminiInvalidVoice` |
| `ErrSpeakerLimit` | `MsgTtsGeminiSpeakerLimit` |
| `ErrInvalidModel` | `MsgTtsGeminiInvalidModel` |

All 422 error messages are run through `i18n.T(locale, key, args...)` using the
locale extracted from `Accept-Language` by `requireAuth → enrichContext`.

---

## 3. Storage Format — Dual-Read / Dual-Write

TTS configuration is persisted in `system_configs` (tenant-scoped key-value store).
Two layouts coexist for backward compatibility:

### Legacy flat keys

```
tts.provider            → "openai"
tts.openai.api_base     → "https://api.openai.com/v1"
tts.openai.voice        → "nova"
tts.openai.model        → "tts-1"
tts.gemini.speakers     → [{"name":"Alice","voice":"Kore"}]  (JSON array)
```

### Params blob (Phase C+)

```
tts.<provider>.params   → {"speed":1.2,"response_format":"mp3"}  (JSON object)
```

### Dual-read logic (`resolveTenantProvider`)

1. Read legacy flat keys to populate `testConnectionRequest` fields.
2. Read `tts.<provider>.params` blob via `loadParamsBlob()`.
3. **Blob wins** on key conflict — blob values override legacy flat keys for the
   same logical param.
4. Legacy flat keys fill gaps not covered by the blob.

### Rollback

Reverting Phase C+ is non-destructive: legacy flat keys remain valid; the blob
is simply ignored when `loadParamsBlob` returns `nil`.

---

## 4. Adding a New Provider

Follow these touchpoints in order. Each step is required; skipping any causes
a compile or runtime failure.

### Step 1 — Provider package

Create `internal/audio/<name>/tts.go` implementing `audio.TTSProvider`:

```go
type Provider struct { /* config fields */ }
func (p *Provider) Name() string { return "<name>" }
func (p *Provider) Synthesize(ctx context.Context, text string, opts audio.TTSOptions) (*audio.SynthResult, error)
func (p *Provider) Capabilities() audio.ProviderCapabilities
```

Follow the SSRF guard pattern for external HTTP calls (validate `apiBase` via
`validateProviderURL` before creating the HTTP client).

### Step 2 — Capabilities()

Return a fully populated `audio.ProviderCapabilities`.

- Set `RequiresAPIKey: true/false`.
- Populate `Params` with `[]audio.ParamSchema`; use `&float64` literals for
  `Min`/`Max`/`Step` (package-level `var` to avoid address-of-literal).
- Defaults **must** match the values your `Synthesize` applies when the param
  is absent from `opts.Params`.
- If voices are dynamic, set `CustomFeatures["voices_dynamic"] = true` and
  implement `VoiceListProvider` (see Step 6).

### Step 3 — createEphemeralTTSProvider (`internal/http/tts_test_connection.go`)

Add a `case "<name>":` branch:

```go
case "<name>":
    return newpkg.NewProvider(newpkg.Config{
        APIKey:    req.APIKey,
        APIBase:   req.APIBase,
        VoiceID:   req.VoiceID,
        ModelID:   req.ModelID,
        TimeoutMs: req.TimeoutMs,
    }), nil
```

Also add `"<name>": true` to `supportedTestProviders` and, if an API key is
needed, to `providersRequiringAPIKey`.

### Step 4 — resolveTenantProvider (`internal/http/tts.go`)

Add a `case "<name>":` branch reading the provider's config keys from
`h.systemConfigs` / `h.configSecrets` and calling `loadParamsBlob`.

### Step 5 — Wire sentinel errors (if any)

If the provider defines sentinel errors (like Gemini's `ErrInvalidVoice`),
add `errors.Is` checks in both `handleSynthesize` and `handleTestConnection`,
using `i18n.T(locale, MsgXxx, ...)` for the response body.

### Step 6 — VoiceListProvider (dynamic voices only)

Implement the interface:

```go
type VoiceListProvider interface {
    TTSProvider
    ListVoices(ctx context.Context) ([]VoiceOption, error)
}
```

The voices handler (`GET /v1/voices?provider=<name>`) uses a type-assertion to
detect this interface at runtime — no registration needed.

### Step 7 — tts_config.go schema validation

If the provider has required fields (e.g. `group_id` for MiniMax), add
validation in `internal/http/tts_config.go` so `PUT /v1/tts/config` returns a
clear 400 when mandatory fields are missing.

### Step 8 — i18n keys

For each `ParamSchema`, add:

1. `tts.<name>.<param_key>.label` and `tts.<name>.<param_key>.help` to all
   6 locale JSON files (web × 3 + desktop × 3).
2. Any backend validation error keys to `internal/i18n/keys.go` and all three
   catalog files (`catalog_en.go`, `catalog_vi.go`, `catalog_zh.go`).

### Step 9 — Tests

- Unit test `Capabilities()` returns non-empty `Provider` + `DisplayName`.
- Test `createEphemeralTTSProvider` builds the provider without error.
- Test `Synthesize` against a mock HTTP server.
- If sentinel errors exist, test 422 body uses `i18n.T` for all locales
  (see `TestSynthesize_GeminiInvalidVoice_I18n` pattern in `tts_test.go`).

### Consumers to check

| File | What to verify |
|---|---|
| `internal/http/tts.go` | `resolveTenantProvider` case |
| `internal/http/tts_test_connection.go` | `createEphemeralTTSProvider` + allow-lists |
| `internal/http/tts_config.go` | Schema validation for required fields |
| `internal/http/voices.go` | `VoiceListProvider` type-assertion path |
| `internal/audio/manager.go` | Auto-discovers via `RegisterProvider`; no change needed |
| `ui/web/src/i18n/locales/*/tts.json` | Param label/help keys (3 locales) |
| `ui/desktop/frontend/src/i18n/locales/*/tts.json` | Desktop locale parity |

---

## 5. Gemini Specifics

### Models

Gemini TTS uses preview models only (as of 2026-04):

- `gemini-3.1-flash-tts-preview` (**default** — higher Elo, more stable)
- `gemini-2.5-flash-preview-tts`
- `gemini-2.5-pro-preview-tts`

The frontend displays a "Preview" badge (i18n key `tts.gemini.previewBadge`).

### Multi-speaker mode

- Up to **2 speakers** per request.
- Each speaker: `{name: string, voice: string}` — voice must be one of the
  30 prebuilt Gemini voices.
- Serialized as `tts.gemini.speakers` JSON blob in `system_configs`.
- At synthesis time, `loadSavedSpeakers` restores the blob into `opts.Speakers`.

### Audio tags

Inline expressive markers injected by the user directly into text:

```
Hello [laughs] world [sighs] how are you?
```

Categories: Emotion, Pacing, Effect, Voice quality. Full tag list is hardcoded
in the frontend (`gemini/audio-tags.ts`).

### Language support

Gemini TTS supports 70+ languages automatically based on input text — no
explicit language parameter needed.

### SSRF guard

`api_base` overrides for Gemini (and all providers) are validated by
`validateProviderURL` in `internal/http/tts_url_validator.go`. Private IP
ranges (127.x, 10.x, 192.168.x, etc.) and `localhost` are rejected with a 400
response to prevent Server-Side Request Forgery.
