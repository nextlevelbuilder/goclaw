# Phase 04 - Tests and Docs

## Overview
- Priority: P1
- Status: Complete
- Purpose: Verify fix and sync public docs.

## Requirements
- Add regression tests for Telegram voice, STT channel override, and read_audio no-image fallback.
- Update docs to reflect actual STT provider IDs and failure behavior.
- Do not add load/stress tests.

## Related Code Files
- Modify: `docs/05-channels-messaging.md`
- Modify: `docs/03-tools-system.md` if read_audio contract changes.
- Modify: `docs/project-changelog.md`
- Run: package-level Go tests for touched packages.

## Implementation Steps
1. Run `go test ./internal/channels/telegram ./internal/audio ./internal/tools`.
2. If shared code changes, run `go test ./internal/agent ./internal/channels/... ./internal/tools`.
3. Update docs after tests pass.
4. Record issue #85 in changelog with root cause and validation commands.

## Implementation Notes
- Updated channel docs to use actual provider IDs: `elevenlabs` and `proxy`.
- Documented Telegram voice MIME preservation and platform-type STT override behavior.
- Documented `read_audio` fail-closed behavior for unsupported audio routes.
- Added a Telegram Bot API fixture test that downloads a voice OGG file and verifies `audio/ogg; codecs=opus` survives from `telego.Voice` through media resolution into STT MIME selection.

## Success Criteria
- Tests pass locally.
- Docs no longer claim provider IDs that do not exist in code.
- Issue acceptance criteria are traceable to test names.

## Open Questions
- None.
