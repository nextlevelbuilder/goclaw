---
phase: 2
title: "Channel Metadata Normalization"
status: completed
priority: P1
effort: "4h"
dependencies: [1]
---

# Phase 2: Channel Metadata Normalization

## Context Links

- Metadata key: `internal/tools/team_metadata_keys.go`
- Consumer mapping: `cmd/gateway_consumer_normal.go`
- Telegram: `internal/channels/telegram/handlers.go`
- Discord: `internal/channels/discord/handler.go`
- Feishu/Lark: `internal/channels/feishu/bot.go`
- Slack: `internal/channels/slack/handlers.go`, `internal/channels/slack/handlers_mention.go`
- WhatsApp: `internal/channels/whatsapp/inbound.go`
- Zalo Personal: `internal/channels/zalo/personal/handlers.go`
- Bitrix24: `internal/channels/bitrix24/handle.go`

## Overview

Normalize best-effort group metadata from all group-capable channels into existing metadata flow. No schema. No fake group names when platform data is unavailable.

## Key Insights

- Telegram already sets `tools.MetaChatTitle`.
- `cmd/gateway_consumer_normal.go` already maps `MetaChatTitle` into `RunRequest.ChatTitle`.
- Discord/Slack have channel IDs today; channel name may need existing cache/context.
- Zalo Personal already sets `group_id`; Bitrix24 group ID is `DialogID`; WhatsApp group ID is group JID.

## Requirements

- Functional: all group-capable adapters preserve group ID through `ChatID`.
- Functional: adapters set `tools.MetaChatTitle` when they already know group/channel display name.
- Functional: user display name still flows through existing `SenderName` resolution.
- Non-functional: no DB migration, no extra hot-path network call only for prompt title.

## Architecture

Normalize into current run fields:

| Prompt Field | Source |
|---|---|
| Platform | `RunRequest.ChannelType`, fallback `Channel` |
| Chat type | `RunRequest.PeerKind` |
| Group ID | `RunRequest.ChatID` |
| Group name | `RunRequest.ChatTitle` from `tools.MetaChatTitle` |
| User | `RunRequest.SenderName` and `RunRequest.SenderID` |

Adapters only populate `tools.MetaChatTitle` where known. Missing title is acceptable.

## Related Code Files

- Modify: `internal/channels/*` group handlers as needed.
- Modify: existing channel tests listed above.
- Do not modify: migrations, store interfaces, session key builders.

## Implementation Steps

1. Audit each group-capable channel for current group title/name availability.
2. Add tests first for metadata forwarding where title is already available.
3. Telegram: keep existing `MetaChatTitle`; add/adjust test to lock behavior.
4. Discord/Slack/Feishu/WhatsApp/Zalo: set `MetaChatTitle` only if handler already has name or cheap cached lookup.
5. Bitrix24: rely on `ChatID`; do not add portal API lookup for group name.
6. Keep topic/thread metadata unchanged.

## Tests Before

- Adapter tests fail for channels with available group name but missing `MetaChatTitle`.
- `go test ./internal/channels/telegram ./internal/channels/discord ./internal/channels/feishu ./internal/channels/slack ./internal/channels/whatsapp ./internal/channels/zalo/... ./internal/channels/bitrix24`

## Refactor

- Prefer reusing `channels.SanitizeDisplayName`.
- Add a tiny helper only if two or more adapters duplicate sanitation.

## Tests After

- Channel unit tests pass.
- Existing routing/thread tests still pass.
- No tests require live platform APIs.

## Todo List

- [x] Lock Telegram title forwarding.
- [x] Add best-effort title tests where available.
- [x] Document unavailable-title platforms in completion notes.
- [x] Ensure Bitrix24 group ID remains `ChatID`.

## Success Criteria

- [x] Every group-capable channel preserves group ID via `ChatID`.
- [x] Every channel with known group name sets `tools.MetaChatTitle`.
- [x] No new schema or session-key format.
- [x] No hot-path title enrichment API calls.

## Completion Notes

- Telegram already forwards `tools.MetaChatTitle` from `message.Chat.Title`.
- Discord now forwards cached channel name from `discordgo.State` when present; no REST lookup added.
- WhatsApp sender display name now flows through `user_name` in `resolveSenderName`.
- Feishu/Lark, Slack, WhatsApp, Zalo Personal, and Bitrix24 keep group ID through `ChatID`; their inbound payloads do not provide a group display name in the current hot path, so `Group name` remains optional.

## Risk Assessment

- Risk: chasing names causes API/cache scope creep. Mitigation: group name optional.
- Risk: inconsistent platform naming. Mitigation: renderer uses channel type label; metadata supplies only title.

## Security Considerations

- Metadata remains untrusted. Prompt renderer sanitizes again.
- Do not log full message content while adding tests.

## Next Steps

- Phase 3 renders the prompt block using normalized metadata and run request fields.
