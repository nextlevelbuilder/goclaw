# Phase 01 - Characterize Telegram Voice Failure

## Overview
- Priority: P0
- Status: Complete
- Purpose: Lock current failing behavior before code changes.

## Context Links
- Issue: https://github.com/digitopvn/goclaw/issues/85
- Debug report: `reports/debugger-260528-1837-telegram-voice-transcription.md`

## Requirements
- Build a small unit-level characterization for Telegram voice `MediaInfo`.
- Prove `msg.Voice.MimeType` is available and should not be discarded.
- Prove channel context is required for legacy Telegram STT proxy selection.
- Prove `read_audio` must not route audio bytes through image payload fallback.

## Characterization Notes
- `resolveMedia` already classifies Telegram `msg.Voice` as `MediaInfo{Type: "voice"}` and copies `msg.Voice.MimeType` into `ContentType`; the loss happens later when `processResolvedMessage` hardcodes STT MIME as `audio/ogg`.
- `audio.Manager` already supports channel-scoped STT overrides through `audio.WithChannel(ctx, "telegram")`; Telegram does not set that context before calling `Transcribe`.
- `read_audio` still has a generic chat fallback that attaches audio bytes as `providers.ImageContent`, which matches the provider-side "image format illegal" failure.

## Related Code Files
- Modify: `internal/channels/telegram/media_test.go`
- Modify: `internal/channels/telegram/handlers_test.go` or add focused helper tests near Telegram package.
- Modify: `internal/audio/manager_stt_test.go` only if existing coverage lacks the channel override case.

## Implementation Steps
1. Add a Telegram voice media test using a `telego.Message{Voice: ...}` fixture.
2. Assert media type is `voice`, MIME is preserved, file path is populated by a stub download path.
3. Add/extend audio manager test proving `audio.WithChannel(ctx, "telegram")` selects the channel-scoped STT provider.
4. Avoid live Telegram or provider calls.

## Success Criteria
- Deterministic regression tests can be added without live Telegram or provider calls.
- Phase 02 owns Telegram STT context/MIME test and fix.
- Phase 03 owns `read_audio` unsupported-provider regression and fix.

## Risks
- `processResolvedMessage` is large; prefer extracting a narrow helper only if needed for testability.

## Open Questions
- None.
