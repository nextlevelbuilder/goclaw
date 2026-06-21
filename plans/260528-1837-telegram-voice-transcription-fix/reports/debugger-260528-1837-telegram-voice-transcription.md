# Telegram Voice Transcription - Investigation Report

## Executive Summary
- **Issue:** Telegram app-recorded voice messages fail transcription; provider returns image-format error.
- **Impact:** Telegram voice workflow blocked when auto-STT misses and agent/tool fallback is used.
- **Root cause:** Audio/voice can fall through to `read_audio`; unsupported provider fallback encodes audio as image content.
- **Status:** Root cause isolated; implementation plan ready.
- **Fix:** Preserve voice MIME + channel context in Telegram STT, then block or convert unsafe `read_audio` fallback.

## Evidence
- Telegram voice is detected as voice:
  - `internal/channels/telegram/media.go:160` handles `msg.Voice`.
  - `internal/channels/telegram/media.go:167` appends `MediaInfo{Type: "voice"}`.
  - `internal/channels/telegram/media.go:171` stores `msg.Voice.MimeType`.
- Telegram STT loses metadata:
  - `internal/channels/telegram/handlers.go:474` enters audio/voice branch.
  - `internal/channels/telegram/handlers.go:480` calls `audioMgr.Transcribe(... MimeType: "audio/ogg")`.
  - This ignores `m.ContentType` and does not set `audio.WithChannel(ctx, c.Name())`.
- Channel-scoped STT exists but is skipped by Telegram call:
  - `internal/audio/manager.go:17` defines `WithChannel`.
  - `internal/audio/manager_stt.go:69` checks channel override before default chain.
  - `internal/audio/legacy_stt_bridge.go:81` registers channel-scoped proxy providers for Telegram/Feishu/Discord.
- Fallback reaches agent/tool path:
  - `internal/channels/telegram/handlers.go:488` only logs STT failure.
  - `internal/channels/telegram/handlers.go:516` emits media tags, transcript optional.
  - `internal/channels/telegram/handlers.go:709` publishes media files to the agent.
  - `internal/agent/loop_input_media.go:120` collects audio refs for `read_audio`.
- Observed image-format error maps to tool fallback:
  - `internal/tools/read_audio_resolve.go:72` documents provider dispatch.
  - `internal/tools/read_audio_resolve.go:160` sends "Other providers" through chat API with `Images`.
  - `internal/tools/read_audio_resolve.go:172` puts audio bytes in `providers.ImageContent`.

## Root Cause
Primary root cause: `read_audio` has a best-effort chat fallback that represents audio as image data. For OpenAI-compatible providers without a transcription model name or dedicated audio route, this generates an image payload and surfaces provider errors like "image format illegal".

Contributing causes:
- Telegram STT hardcodes `audio/ogg`, losing actual MIME detail such as `audio/ogg; codecs=opus`.
- Telegram STT does not attach channel context, so legacy channel-specific proxy STT cannot win.
- Telegram keeps processing after STT failure with only a warn log, so the LLM can call `read_audio` and hit the unsafe fallback.
- Docs describe a unified STT chain, but UI/code naming currently uses `elevenlabs` / `proxy`, while docs show `elevenlabs_scribe` / `proxy_stt`.

## Recommended Plan
### Immediate (P0)
- Add focused tests proving Telegram voice path uses actual voice MIME and `audio.WithChannel`.
- Change Telegram STT call to pass `MimeType: fallbackAudioMime(m.ContentType)` and a channel-scoped context.
- Keep failure behavior user-friendly: log audio-specific cause and keep `<media:voice>` tag, but do not imply image analysis.

### Short-Term (P1)
- Change `read_audio` fallback: if provider route is not Gemini, native OpenAI audio, or transcription endpoint, return clear audio-specific unsupported-provider error instead of `Images`.
- Consider explicit provider capability check for audio input/transcription before dispatch.
- Add regression around DashScope/openai-compatible non-transcription model to ensure no image payload is produced.

### Longer-Term (P2)
- Decide conversion layer: ffmpeg OGG/Opus to WAV/MP3 before STT when selected provider cannot accept OGG/Opus.
- Sync docs and UI labels with actual provider ids.

## Open Questions
- Should `builtin_tools[stt]` provider order drive Telegram STT now, or only after a separate STT settings migration?
- Should conversion be implemented now, or only after provider rejection proves it is needed?
