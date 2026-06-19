---
title: "Group Chat Context System Prompt"
description: "Add explicit group chat context block to agent system prompt for all group-capable channels."
status: completed
priority: P2
issue: 70
branch: "codex/issue-70-group-context-plan"
tags: [backend, channels, prompt, tdd, issue-70]
blockedBy: []
blocks: []
created: "2026-05-28T11:08:27.797Z"
createdBy: "ck:plan"
source: skill
---

# Group Chat Context System Prompt

## Overview

Implement `digitopvn/goclaw#70`: when an agent handles a group chat message, system prompt includes a clear current chat context block with platform, chat type, group name when known, group ID, and sender identity.

Scope is prompt-only. No group-specific memory, rules engine, DB schema, session-key, or workspace behavior changes.

## Current Code Context

- Prompt input shape: `internal/agent/systemprompt.go` (`SystemPromptConfig.ChatID`, `ChatTitle`, `PeerKind`, `SenderID`)
- Prompt assembly: `internal/agent/systemprompt.go` (`BuildSystemPrompt`)
- Loop wiring: `internal/agent/loop_history.go`
- Inbound consumer: `cmd/gateway_consumer_normal.go`
- Metadata key: `internal/tools/team_metadata_keys.go` (`MetaChatTitle`)
- Group-capable channels: Telegram, Discord, Feishu/Lark, Slack, WhatsApp, Zalo Personal, Bitrix24

## Phases

| Phase | Name | Status |
|-------|------|--------|
| 1 | [TDD Prompt Contract](./phase-01-tdd-prompt-contract.md) | Complete |
| 2 | [Channel Metadata Normalization](./phase-02-channel-metadata-normalization.md) | Complete |
| 3 | [Prompt Rendering Integration](./phase-03-prompt-rendering-integration.md) | Complete |
| 4 | [Validation and Issue Handoff](./phase-04-validation-and-issue-handoff.md) | Complete |

## Dependencies

- GitHub issue: `digitopvn/goclaw#70`
- No blocking plan found in this worktree.
- Preserve existing `<current_reply_target>` behavior and group reply hints.

## Acceptance Criteria

- Group prompt contains `## Current Chat Context`.
- Group prompt includes platform, `Chat type: Group`, group ID, and user identity.
- Group name is included only when known after sanitization.
- DM prompt does not include group name or group ID.
- All group-capable channel adapters forward best-effort normalized group metadata without adding DB schema.
- `go test ./internal/agent ./cmd ./internal/channels/...` and `go build ./...` pass before implementation PR.
