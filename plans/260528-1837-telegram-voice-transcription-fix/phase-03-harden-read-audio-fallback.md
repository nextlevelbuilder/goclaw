# Phase 03 - Harden Read Audio Fallback

## Overview
- Priority: P0
- Status: Complete
- Purpose: Stop audio being sent as image payload.

## Requirements
- `read_audio` must never put audio bytes in `providers.ImageContent`.
- Unsupported provider/model routes must return an audio-specific error.
- Preserve working routes:
  - Gemini File API.
  - Native OpenAI input audio.
  - OpenAI transcription endpoint for transcription model names.

## Related Code Files
- Modify: `internal/tools/read_audio_resolve.go`
- Modify: `internal/tools/read_audio_resolve_test.go`
- Optional: `internal/tools/read_audio.go` description to mention `<media:voice>`.

## Implementation Steps
1. Replace "Other providers" image fallback with explicit unsupported audio route error.
2. Include provider name, provider type, model, and accepted options in the error, without secrets.
3. Add regression test using an OpenAI-compatible non-transcription provider/model that previously went through `Images`.
4. Add test ensuring `<media:voice>` id still resolves through `read_audio`.

## Implementation Notes
- Removed generic chat/image fallback from `read_audio`.
- Added regression proving unsupported audio routes do not call provider chat and do not create `providers.ImageContent`.
- Preserved Gemini File API, native OpenAI input audio, and OpenAI-compatible transcription model routes.

## Success Criteria
- The observed "image format illegal" class cannot be produced by GoClaw for audio fallback.
- User-facing error says audio provider/model unsupported or STT provider unavailable.
- Existing Gemini/OpenAI/transcription tests still pass.

## Risks
- Some provider may have relied on chat fallback for audio. Treat as unsafe legacy path because it is semantically wrong and produced this issue.

## Open Questions
- DashScope/Qwen audio chat can be added later as a first-class verified route.
