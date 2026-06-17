---
phase: 3
title: Channel Capability and Telegram Grouping
status: completed
priority: P1
effort: 3h
dependencies:
  - 2
---

# Phase 3: Channel Capability and Telegram Grouping

## Overview

Add channel capability metadata and implement Telegram media-group delivery for compatible batches while preserving ordered fallback elsewhere.

## Requirements

- Functional: Telegram sends compatible media in album chunks of 2-10.
- Functional: Telegram documents group only with documents, audio only with audio, and photo/video may group together.
- Functional: mixed or singleton chunks fall back to existing per-file send helpers.
- Functional: caption behavior remains stable: first item uses shared message content when no per-file caption exists; oversized captions become follow-up text.
- Non-functional: existing thread/reply behavior stays intact; reply applies to first grouped chunk only.

## Architecture

Add small reusable capability structs in `internal/channels/capabilities.go`, then keep platform-specific grouping logic in Telegram package:
- Capability data exposes max batch count and grouping mode for docs/tests.
- Telegram helper classifies outbound media as photo/video/audio/document, validates size, prepares captions, and emits chunks.
- `sendMediaMessage` chooses media-group chunks only when safe; all other cases call the existing `sendPhoto`, `sendVideo`, `sendAudio`, `sendVoice`, or `sendDocument` helpers.

## Related Code Files

- Modify: `internal/channels/capabilities.go`
- Modify: `internal/channels/telegram/send.go`
- Modify/Create: `internal/channels/telegram/*_test.go`
- Modify: `internal/channels/discord/send_media.go` only if regression tests expose a real compatibility gap.

## Implementation Steps

1. Add channel capability metadata for Telegram and Discord; Slack is media-capable but no single-message multi-file guarantee in this phase.
2. Add Telegram pure helpers for chunking compatible media batches.
3. Implement `sendMediaGroup` with `telego.SendMediaGroupParams` and file seek/open handling.
4. Keep fallback path for audio-as-voice, oversized image-as-document, singleton chunks, and any group send error where retrying single sends is safer.
5. Run `go test ./internal/channels/telegram ./internal/channels/discord ./internal/channels/slack`.

## Success Criteria

- [x] Telegram helper tests prove 2-10 compatible grouping and ordered fallback.
- [x] Discord existing multi-file upload path remains unchanged.
- [x] Slack fallback continues ordered file uploads plus text.

## Risk Assessment

Telegram media groups are not transactional across fallback retries. Avoid marking success until the channel call returns nil; do not hide partial failure.
