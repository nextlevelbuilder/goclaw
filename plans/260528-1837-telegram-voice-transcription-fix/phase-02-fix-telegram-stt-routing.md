# Phase 02 - Fix Telegram STT Routing

## Overview
- Priority: P0
- Status: Complete
- Purpose: Make Telegram voice STT use the right MIME and provider chain.

## Requirements
- Use `m.ContentType` for audio/voice STT, with safe fallback to `audio/ogg`.
- Wrap STT context with `audio.WithChannel(ctx, c.Type())`.
- Preserve current 10s timeout and fallback behavior.
- Do not change WhatsApp opt-in semantics.

## Related Code Files
- Modify: `internal/channels/telegram/handlers.go`
- Optional: create helper in `internal/channels/telegram/stt.go` if `handlers.go` growth needs containment.

## Implementation Steps
1. Introduce a small helper for audio MIME fallback: empty or `application/octet-stream` -> `audio/ogg`.
2. Replace hardcoded `MimeType: "audio/ogg"` with the helper result.
3. Call `audio.WithChannel(sttCtx, c.Type())` before `audioMgr.Transcribe`.
4. Preserve `m.Transcript = res.Text` only on non-empty success.
5. Log provider failure as Telegram audio STT failure with type, MIME, and provider error.

## Implementation Notes
- Used `c.Type()` instead of `c.Name()` so DB channel instances like `telegram-main` still resolve the legacy override registered for platform type `telegram`.
- Added a focused regression where tenant STT is configured but Telegram channel-scoped STT wins and receives `audio/ogg; codecs=opus`.
- Empty transcripts remain non-fatal; no transcript tag is added.

## Success Criteria
- Telegram legacy `stt_proxy_url` can be selected via channel override.
- Telegram OGG/Opus MIME survives into STT input.
- Existing Telegram media flow still publishes `bus.InboundMessage.Media`.

## Security Considerations
- No new secret storage.
- Do not log raw audio path if it can expose tenant filesystem details beyond existing debug behavior; prefer basename when possible.

## Open Questions
- None.
