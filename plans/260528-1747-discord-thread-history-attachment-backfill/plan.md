---
title: "Discord Thread History Attachment Backfill"
description: "TDD plan for Discord thread REST backfill so a newly mentioned bot can see prior thread messages and attachments."
status: complete
priority: P2
branch: "codex/issue-69-discord-thread-history-attachments"
tags: [discord, threads, media, tdd, issue-69]
blockedBy: []
blocks: []
created: "2026-05-28T10:47:50.194Z"
createdBy: "ck:plan"
source: skill
---

# Discord Thread History Attachment Backfill

## Overview

Fix GitHub issue #69 with a narrow Discord-thread-only backfill. When the bot is mentioned in a Discord thread, fetch recent thread messages before the triggering message via Discord REST, convert prior text into context, download bounded prior attachments, and pass those files through the existing inbound media pipeline so `read_image` and `read_document` can access them.

Non-goals: no global Discord group/channel history backfill, no schema migration, no full-thread archive export, no always-on polling. If Discord permissions do not allow history, log and continue with current message.

## Phases

| Phase | Name | Status |
|-------|------|--------|
| 1 | [Characterization Tests](./phase-01-characterization-tests.md) | Complete |
| 2 | [Thread Backfill Implementation](./phase-02-thread-backfill-implementation.md) | Complete |
| 3 | [Media Tool Integration Tests](./phase-03-media-tool-integration-tests.md) | Complete |
| 4 | [Docs and Verification](./phase-04-docs-and-verification.md) | Complete |

## Dependencies

- GitHub issue: https://github.com/digitopvn/goclaw/issues/69
- Discord docs: `GET /channels/{channel.id}/messages` returns newest-to-oldest messages, `limit` 1-100, with `before/after/around`; guild channels need `VIEW_CHANNEL`, and missing `READ_MESSAGE_HISTORY` returns no messages.
- Discord attachment docs: fetched message payload attachment URLs are signed CDN URLs valid when received, so history attachments must be downloaded immediately.
- Existing Discord handler: `internal/channels/discord/handler.go`
- Existing Discord media download/classification: `internal/channels/discord/media.go`
- Existing media tool pipeline: `internal/agent/loop_input_media.go`, `internal/agent/media_tool_routing.go`, `internal/tools/read_image.go`, `internal/tools/read_document.go`

## Success Criteria

- [x] In a Discord thread, message A before bot mention with image/doc attachment becomes available to the run triggered by message B.
- [x] Agent receives prior text context and prior attachment files.
- [x] `read_image` can analyze prior image; `read_document` can resolve prior document.
- [x] Backfill runs only for Discord threads when the bot is addressed.
- [x] Backfill is bounded by message count, attachment count, file size, and timeout.
- [x] Missing Discord history permission is graceful: no crash, current message still processed.

## Implementation Boundary

Use TDD. Do not implement a broad channel history system. Keep this as a Discord thread adapter feature around current `handleMessage` flow and existing media pipeline.
