---
title: "Agent Behavior UX Overrides and Sidecar Delivery"
description: >-
  TDD plan to remove Tool Status Messages, add sidecar-generated delivery
  updates, and support Channel > Agent > Workspace behavior overrides.
status: in_progress
priority: P1
branch: "codex/agent-behavior-ux-overrides"
tags: [channels, agents, chat-behavior, quick-ack, tdd, zuey]
blockedBy: []
blocks: []
created: "2026-06-09T10:14:07.569Z"
createdBy: "ck:plan"
source: skill
---

# Agent Behavior UX Overrides and Sidecar Delivery

## Overview

Simplify agent/channel Behavior UX after the Show Reasoning work: remove Tool
Status Messages, generate delivery updates through an optional cheap sidecar
provider/model, and add agent-level overrides.

Product decisions:
- Keep Show Reasoning separate for debugging/testing.
- Retire Tool Status Messages as a message feature because it overlaps with
  Intermediate Replies and deterministic tool-name text reads robotic.
- Quick Acknowledgement and Intermediate Replies are channel delivery events
  only; visible progress text must not be added to main session context.
- Delivery updates may use a separate fast/cheap provider and model.
- Override order is Channel > Agent > Workspace (`Config > Behavior`).
- Prefer no DB migration: store agent overrides in `agents.other_config`.

Verified current facts:
- `ChatBehaviorConfig` and `QuickAckConfig` exist in
  `internal/config/config_channels.go:3`; workspace `gateway.block_reply`,
  `gateway.chat_behavior`, and `gateway.tool_status` are at
  `internal/config/config_channels.go:407`.
- Quick Ack currently schedules from channel events in
  `internal/channels/events.go:42`; Intermediate Replies publish outbound
  `block.reply` bubbles in `internal/channels/events.go:330`.
- Channel registration currently resolves Channel > Workspace only at
  `cmd/gateway_consumer_normal.go:245`.
- `RunRequest` already has provider/model override fields in
  `internal/agent/loop_types.go:621`, useful precedent for cheap side calls.
- `ConsumerDeps` currently has no provider registry/store in
  `cmd/gateway_consumer_deps.go:18`; configured sidecar provider names need a
  new dependency path from `gatewayRuntime.providerRegistry`.
- Current `llm_generated` Quick Ack does not call an LLM: `sendQuickAck()` sends
  the first template from `internal/channels/events.go:471`, while
  `ShouldDeliverGeneratedProgress()` is only a block.reply gate in
  `internal/channels/chat_behavior.go:170`.
- Current intermediate replies are main-pipeline `block.reply` events:
  `ThinkStage` appends the assistant tool-call message to context at
  `internal/pipeline/think_stage.go:136` before emitting block.reply at
  `internal/pipeline/think_stage.go:147`.

## Phases

| Phase | Name | Status |
|-------|------|--------|
| 1 | [Characterization and Contract Tests](./phase-01-characterization-and-contract-tests.md) | Done |
| 2 | [Config Resolution and Storage Contract](./phase-02-config-resolution-and-storage-contract.md) | Done |
| 3 | [Sidecar Delivery Message Generator](./phase-03-sidecar-acknowledgement-generator.md) | Done |
| 4 | [Runtime Delivery and Context Isolation](./phase-04-runtime-delivery-and-context-isolation.md) | Done |
| 5 | [Web UI Behavior Simplification](./phase-05-web-ui-behavior-simplification.md) | Done |
| 6 | [Validation and Zuey Beta Handoff](./phase-06-validation-and-zuey-beta-handoff.md) | In Progress |

## Dependencies

- Completed plan: `../260529-1210-human-like-channel-chat-behavior/plan.md`
- Completed plan: `../260531-1555-issue-118-llm-generated-channel-progress/plan.md`
- Completed plan: `../260601-2004-show-reasoning-always-bubbles/plan.md`
- Runtime files: `internal/config/config_channels.go`, `internal/channels/{chat_behavior,events,runs}.go`, `cmd/gateway_consumer_normal.go`
- Agent/UI files: `internal/store/agent_store.go`, `internal/config/config.go`,
  `ui/web/src/pages/{agents,channels,config}/`, `ui/web/src/i18n/locales/`

## Acceptance Criteria

- [x] Tool Status Messages is no longer shown in `Config > Behavior`.
- [x] Runtime no longer emits deterministic tool-status channel messages.
- [x] Quick Ack and Intermediate Replies support sidecar provider/model output.
- [x] Configured sidecar provider names resolve through the tenant-aware provider
      registry, with agent provider/model fallback.
- [x] Sidecar output is sent only as channel outbound events.
- [x] Visible delivery text is never appended to session history or main LLM context.
- [x] Channel overrides beat Agent overrides; Agent overrides beat Workspace.
- [x] Existing channel overrides keep backward compatibility.
- [x] Web UI exposes clear Workspace, Agent, and Channel behavior controls.
- [ ] Validation includes local tests/build and zuey beta deploy verification.

## Red Team Review

### Session - 2026-06-09
**Findings:** 7 (6 accepted, 1 rejected)
**Severity breakdown:** 0 Critical, 4 High, 3 Medium

| # | Finding | Severity | Disposition | Applied To |
|---|---------|----------|-------------|------------|
| 1 | Configured sidecar provider cannot resolve from current consumer deps | High | Accept | Phases 1, 3, 4, 6 |
| 2 | Intermediate Replies are still main-pipeline block.reply content | High | Accept | Phases 1, 2, 3, 4 |
| 3 | Quick Ack root cause is not characterized | High | Accept | Phases 1, 3 |
| 4 | Intermediate Replies and Quick Ack are still conflated through quick_ack gating | High | Accept | Phases 1, 2, 4, 5 |
| 5 | UI schema changes can save wrong nested shapes without serializer tests | Medium | Accept | Phases 1, 5 |
| 6 | Tool Status remains in another config UI surface | Medium | Accept | Phase 5 |
| 7 | `agentLoop.OtherConfig()` is fabricated | Medium | Reject | None |

#### Accepted Finding Details

1. **Configured sidecar provider cannot resolve from current consumer deps.**
   `ConsumerDeps` exposes `UsageCaps` but not `ProviderReg` or `ProviderStore`
   (`cmd/gateway_consumer_deps.go:18`), and `consumeInboundMessages` builds deps
   without the registry (`cmd/gateway_consumer.go:48`). Yet provider registry
   already supports tenant-aware lookup (`internal/providers/registry.go:119`).
   The plan now requires threading `gatewayRuntime.providerRegistry` into the
   consumer and tests proving explicit provider/model is actually used.
2. **Intermediate Replies are still main-pipeline block.reply content.**
   `ThinkStage` appends assistant tool-call content into run messages
   (`internal/pipeline/think_stage.go:136`) before emitting block.reply
   (`internal/pipeline/think_stage.go:147`). The plan now requires sidecar
   intermediate delivery to be generated from delivery metadata/tool phase only,
   not from `resp.Content`.
3. **Quick Ack root cause is not characterized.** Current `sendQuickAck()` always
   sends a template (`internal/channels/events.go:471`); `llm_generated` only
   affects preview/progress gates (`internal/channels/chat_behavior.go:170`).
   Phase 1 now locks this as a failing characterization test.
4. **Intermediate Replies and Quick Ack are conflated.**
   `ShouldDeliverGeneratedProgress()` requires `QuickAck.Enabled`
   (`internal/channels/chat_behavior.go:170`), so disabling ack also disables
   generated progress. The plan now separates `quick_ack` from
   `intermediate_replies`.
5. **UI schema changes need serializer tests.** Channel form coercion deletes
   `"inherit"` and maps string booleans in `coerceSelects()`
   (`ui/web/src/pages/channels/channel-instance-form-dialog.tsx:137`), while the
   schema currently stores nested keys like `chat_behavior.quick_ack.mode`
   (`ui/web/src/pages/channels/channel-schemas.ts:37`). Phase 5 now tests
   flatten/unflatten and coercion for provider/model/timeout fields.
6. **Tool Status remains in another config UI surface.**
   `system-settings-modal.tsx` still reads and saves `gateway.tool_status`
   (`ui/web/src/components/layout/system-settings-modal.tsx:77` and `:135`).
   Phase 5 now removes/hides all user-facing Tool Status controls, not only
   `BehaviorUxCard`.

#### Rejected Finding Details

7. **`agentLoop.OtherConfig()` is fabricated.** Rejected: the method exists on
   the `Agent` interface (`internal/agent/types.go:17`) and `Loop`
   (`internal/agent/loop_tracing.go:36`).

### Whole-Plan Consistency Sweep
- Files reread: `plan.md`, `phase-01-*`, `phase-02-*`, `phase-03-*`,
  `phase-04-*`, `phase-05-*`, `phase-06-*`.
- Decision deltas checked: 6.
- Reconciled stale references: 9.
- Unresolved contradictions: 0.
