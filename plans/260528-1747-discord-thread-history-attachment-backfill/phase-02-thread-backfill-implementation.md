---
phase: 2
title: "Thread Backfill Implementation"
status: complete
priority: P1
effort: "3h"
dependencies: [1]
---

# Phase 2: Thread Backfill Implementation

## Overview

Implement the narrow Discord thread backfill that makes Phase 1 tests pass. The backfill runs only when the inbound message is in a Discord thread and the bot is addressed.

## Requirements

- Functional: detect Discord thread channels reliably.
- Functional: call `ChannelMessages(threadID, limit, before=currentMessageID, after="", around="")`.
- Functional: merge prior text and prior attachments into the same inbound run.
- Non-functional: bounded message count, bounded attachment count, bounded file size, bounded total timeout.

## Architecture

Add focused helper functions instead of growing `handler.go` further. `handleMessage` should ask a helper for `threadBackfillResult`, then prepend its context and merge its media before publishing inbound.

Suggested helper shape:

```go
type threadBackfillResult struct {
    Context string
    Media   []bus.MediaFile
}
```

Use Discord REST only for thread messages before the triggering message. Reverse REST result because Discord returns newest-to-oldest.

## Related Code Files

- Modify: `internal/channels/discord/handler.go`
- Modify: `internal/channels/discord/media.go`
- Create focused helper if needed: `internal/channels/discord/thread-history-backfill.go`
- Read: `internal/channels/history.go`
- Read: `internal/channels/media/media_tags.go`

## Implementation Steps

1. Add constants: history message limit (start 25), history media max bytes (5 MB), history media max refs (15), total timeout (30s).
2. Add `isDiscordThread` helper using session state first and REST `Channel` fallback when state misses.
3. Add `fetchThreadHistoryBefore(ctx, threadID, currentMessageID)` using `ChannelMessages`.
4. Normalize messages: skip current/bot messages, skip empty content unless attachments exist, resolve author display name, keep timestamps.
5. Resolve prior attachments immediately using existing download/classify path with stricter history caps.
6. Build a clear context block: `[Discord thread messages before this mention - for context]`.
7. Merge backfill into current flow before `PublishInbound`: current user message remains the active request; prior history is context only.
8. On REST errors or no permissions, log `slog.Warn("discord: thread history backfill failed", ...)` and continue current message.
9. Keep pending `GroupHistory()` behavior unchanged for normal group mention gating.

## Success Criteria

- [x] Phase 1 tests pass.
- [x] Backfill is invoked only for `Channel.IsThread()` and mentioned/reply-to-bot triggers.
- [x] Attachment download failures skip that attachment, not the whole run.
- [x] No schema/config migration required.

## Risk Assessment

Risk: Discord REST permissions vary by server. Mitigation: graceful fallback and explicit docs note.

Risk: rate limits if called too often. Mitigation: only on thread mention, low default limit, no pagination in v1.
