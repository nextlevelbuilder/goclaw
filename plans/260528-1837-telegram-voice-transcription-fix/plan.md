# Telegram Voice Transcription Fix Plan

## Context
- Issue: https://github.com/digitopvn/goclaw/issues/85
- Worktree: `/Users/duynguyen/.codex/worktrees/codex/issue-85-telegram-voice-transcription-plan`
- Branch: `codex/issue-85-telegram-voice-transcription-plan`
- Base: `origin/dev` at `5017f7ca` after merging latest dev into the issue branch
- Debug report: `reports/debugger-260528-1837-telegram-voice-transcription.md`

## Goal
Fix Telegram app-recorded voice messages so `<media:voice>` is treated as audio end-to-end, transcribed through the STT path, and never routed through image payloads.

## Verified Findings
- Telegram downloads `msg.Voice` as media type `voice` and carries `msg.Voice.MimeType`, but STT call currently forces MIME to `audio/ogg` instead of using the detected content type: `internal/channels/telegram/media.go:160`, `internal/channels/telegram/handlers.go:474`.
- Telegram calls `audioMgr.Transcribe` without `audio.WithChannel(ctx, c.Name())`, so channel-scoped legacy STT proxy overrides cannot be selected: `internal/audio/manager.go:17`, `internal/audio/manager_stt.go:69`, `internal/audio/legacy_stt_bridge.go:81`.
- When Telegram STT fails or is not configured, the message still reaches the agent with `<media:voice>` and media attachment. `read_audio` can then be invoked: `internal/channels/telegram/handlers.go:488`, `internal/channels/telegram/handlers.go:709`, `internal/agent/loop_input_media.go:120`.
- `read_audio` fallback for non-Gemini/non-OpenAI/non-transcription providers sends audio bytes as `providers.ImageContent`, matching the observed "image format illegal" provider error: `internal/tools/read_audio_resolve.go:160`.
- Docs say unified STT should cover Telegram and use the configured provider chain, but current names and behavior drift from implementation: `docs/05-channels-messaging.md:262`.

## Phases
- [x] [Phase 01 - Characterize Failure](phase-01-characterize-telegram-voice-failure.md)
- [x] [Phase 02 - Fix Telegram STT Routing](phase-02-fix-telegram-stt-routing.md)
- [x] [Phase 03 - Harden Read Audio Fallback](phase-03-harden-read-audio-fallback.md)
- [x] [Phase 04 - Tests and Docs](phase-04-tests-and-docs.md)

## Dependencies
- No schema migration expected.
- If adding audio transcoding, decide between built-in ffmpeg dependency vs provider-compatible no-conversion path first.
- Keep WhatsApp opt-in behavior unchanged.

## Success Criteria
- Telegram `Voice` messages preserve `audio/ogg` / `audio/ogg; codecs=opus` metadata into STT and media refs.
- Channel-scoped STT proxy works for Telegram via `audio.WithChannel`.
- `read_audio` never sends audio as image payload for unsupported provider routes; errors are audio-specific and actionable.
- Regression tests cover Telegram voice media, STT channel context, and `read_audio` unsupported fallback.

## Open Questions
- None.
