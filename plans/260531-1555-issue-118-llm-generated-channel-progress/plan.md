---
title: Issue 118 LLM-Generated Channel Progress
description: >-
  TDD plan for digitopvn/goclaw#118: make channel immediate/progress replies
  LLM-generated through the existing main-turn block.reply path, with fixed
  quick_ack templates retained only as fallback.
status: completed
priority: P2
branch: codex/issue-118-llm-generated-progress-messages
tags:
  - issue-118
  - channels
  - chat-behavior
  - block-reply
  - tdd
blockedBy: []
blocks: []
created: '2026-05-31T08:55:50.410Z'
createdBy: 'ck:plan'
source: skill
---

# Issue 118 LLM-Generated Channel Progress

## Overview

Implement the approved cheapest path for issue #118.

Decision locked by user:
- No separate LLM call for immediate/progress messages.
- Use the existing main-turn `block.reply` event as the generated channel progress message.
- Keep `quick_ack.templates` only as fallback.
- Do not store progress messages.

Hard product constraint: without a separate LLM call, GoClaw cannot guarantee a natural LLM-generated message before the main model emits content. The generated progress message is available when the main LLM emits assistant content before tool calls; otherwise a configured fixed template fallback may fire after the fallback delay.

Current implementation facts:
- `QuickAckConfig` has `enabled`, `min_delay_ms`, and `templates` only, so mode/fallback semantics need an additive config field in `internal/config/config_channels.go:11`.
- `ResolveChatBehavior` currently defaults templates to `"Got it. Working on it..."` and `ShouldSendQuickAck` only checks enabled plus non-streaming in `internal/channels/chat_behavior.go:57` and `internal/channels/chat_behavior.go:140`.
- `run.started` schedules quick ack immediately via `internal/channels/events.go:42`.
- `block.reply` channel delivery is currently gated only by resolved `BlockReplyEnabled` in `internal/channels/events.go:276`.
- Main-turn generated content already emits `block.reply` from tool iterations in `internal/pipeline/think_stage.go:148`, then sanitizes in `internal/agent/loop_pipeline_adapter.go:107`.
- Final dedup depends on `blockReplyEnabled` in `cmd/gateway_consumer_normal.go:542`.
- `internal/agent/run_timeline_recorder.go:137` already maps existing `block.reply` events to assistant-message timeline items. This issue must not add new persistence or broaden recorder behavior; existing timeline behavior is out of scope unless tests show a direct regression.

Scope:
- Backend config contract and resolver.
- Channel event delivery semantics.
- Preview API and dashboard controls.
- Documentation and issue handoff.

Explicitly out of scope:
- New LLM provider call.
- DB schema, timeline/archive persistence, or message history storage for progress messages.
- Changing existing run timeline recorder semantics.
- Raw chain-of-thought or tool trace exposure.
- Per-agent prompt rewriting beyond existing main LLM output.

## Phases

| Phase | Name | Status |
|-------|------|--------|
| 1 | [Characterization Tests](./phase-01-characterization-tests.md) | Completed |
| 2 | [Config Contract](./phase-02-config-contract.md) | Completed |
| 3 | [Runtime Delivery](./phase-03-runtime-delivery.md) | Completed |
| 4 | [Dashboard Controls](./phase-04-dashboard-controls.md) | Completed |
| 5 | [Validation and Ship Handoff](./phase-05-validation-and-ship-handoff.md) | Completed |

## Dependencies

- GitHub issue: `digitopvn/goclaw#118`
- Related existing plan: `../260529-1210-human-like-channel-chat-behavior/plan.md`
- Existing generated-content source: `internal/pipeline/think_stage.go`, `internal/agent/loop_pipeline_adapter.go`, `internal/pipeline/observe_stage.go`
- Existing channel runtime: `internal/channels/events.go`, `internal/channels/runs.go`, `cmd/gateway_consumer_normal.go`
- Existing config/runtime contract: `internal/config/config_channels.go`, `internal/channels/chat_behavior.go`, `internal/gateway/methods/chat_behavior.go`
- Existing web config UI: `ui/web/src/pages/config/sections/behavior-section.tsx`, `ui/web/src/pages/config/sections/behavior-chat-card.tsx`, `ui/web/src/pages/channels/channel-schemas.ts`
- Existing docs: `docs/05-channels-messaging.md`

## Acceptance Criteria

- [ ] Default enabled chat behavior prefers LLM-generated progress from main-turn `block.reply`, not fixed templates.
- [ ] No extra LLM request is introduced.
- [ ] Fixed templates remain configurable fallback only.
- [ ] Fallback does not fire after a generated `block.reply` has already been delivered.
- [ ] Streaming channels still avoid duplicate progress delivery.
- [ ] Existing explicit `gateway.block_reply` behavior and final dedup continue to work.
- [ ] Preview API describes generated-vs-fallback behavior without sending messages.
- [ ] Dashboard config reflects generated default, fallback template semantics, and all new UI text is localized in en/vi/zh.
- [ ] No new DB/archive/session-history storage is added for progress messages by this issue.

## Validation Commands

```bash
go test ./internal/channels ./internal/config ./internal/gateway/methods
go test -tags sqliteonly ./internal/channels ./internal/config ./internal/gateway/methods
go build ./...
go build -tags sqliteonly ./...
go vet ./...
cd ui/web && pnpm test -- --run
cd ui/web && pnpm build
git diff --check
```

## Open Questions

None. User selected the no-extra-LLM, main-turn `block.reply`, no-progress-storage path.
