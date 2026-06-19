---
title: Issue 172 Multi Attachment Delivery
description: ''
status: completed
priority: P2
branch: codex/issue-172-multi-attachment-delivery
tags: []
blockedBy: []
blocks: []
created: '2026-06-12T10:08:08.060Z'
createdBy: 'ck:plan'
source: skill
---

# Issue 172 Multi Attachment Delivery

## Overview

Implement GitHub issue #172 in beta mode. Add a batch attachment tool API that lets agents queue multiple existing workspace files in one tool call, preserve order/caption metadata, and let channel senders group uploads where the platform supports it.

Scout summary:
- `send_file` currently accepts one `path` and returns one `Result.Media` entry: `internal/tools/send_file.go`.
- `write_file(deliver=true)` already queues one file and marks it in `DeliveredMedia`: `internal/tools/filesystem_write.go`.
- `message` can publish embedded multiple `MEDIA:` refs in one `bus.OutboundMessage`: `internal/tools/message.go`.
- Tool result media flows through `agent.MediaResult` and `appendMediaToOutbound`; captions must be copied there or batch metadata is lost.
- Discord already sends multiple files in one message: `internal/channels/discord/send_media.go`.
- Telegram currently loops one media item at a time, even though `telego.SendMediaGroup` supports 2-10 album items with type restrictions: `internal/channels/telegram/send.go`.
- Slack uploads files one by one, so it remains ordered fallback unless/until a safe multi-upload contract is added: `internal/channels/slack/send.go`.

Scope boundary:
- In scope: backend tool contract, media caption propagation, Telegram grouped album chunks, Discord regression coverage, ordered fallback for unsupported/mixed cases.
- Out of scope: web UI changes, inbound media debounce, database migrations, WhatsApp/Zalo/Lark deep batching beyond existing ordered send behavior.

## Phases

| Phase | Name | Status |
|-------|------|--------|
| 1 | [Research and TDD Contracts](./phase-01-research-and-tdd-contracts.md) | Completed |
| 2 | [Tool API Batch Attachments](./phase-02-tool-api-batch-attachments.md) | Completed |
| 3 | [Channel Capability and Telegram Grouping](./phase-03-channel-capability-and-telegram-grouping.md) | Completed |
| 4 | [Regression Verification and Ship](./phase-04-regression-verification-and-ship.md) | Completed |

## Dependencies

No active blocking plan found in the project plan scan. Related historical plan: Discord thread attachment backfill, read-only context.
