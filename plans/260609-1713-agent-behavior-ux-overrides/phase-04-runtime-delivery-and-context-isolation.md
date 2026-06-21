---
phase: 4
title: "Runtime Delivery and Context Isolation"
status: completed
priority: P1
effort: "1d"
dependencies: [2, 3]
---

# Phase 4: Runtime Delivery and Context Isolation

## Overview

Wire the new resolved behavior into channel runs, retire deterministic tool
status messages, and prove delivery-only messages cannot pollute main context.

## Requirements

- Functional: apply resolved behavior before `RegisterRunWithBehavior`.
- Functional: Intermediate Replies and Quick Ack obey Channel > Agent >
  Workspace.
- Functional: Tool Status Messages no longer emit channel text.
- Functional: Intermediate Reply sidecar delivery does not reuse main
  `block.reply` `resp.Content`.
- Non-functional: no new goroutine leaks; timers cancel on terminal events.

## Architecture

The main inbound path already resolves channel streaming and registers run state
in `cmd/gateway_consumer_normal.go:204` and `cmd/gateway_consumer_normal.go:245`.
Extend that point to parse agent override from `agentLoop.OtherConfig()` and
build a single resolved delivery behavior snapshot for the run. Also thread the
tenant-aware provider registry into `ConsumerDeps`; the current dependency bag
cannot resolve a configured sidecar provider name.

Keep outbound event handling in `internal/channels/events.go`, but add a
delivery event path that is separate from main-pipeline `block.reply` content.
Do not push delivery-only text into `pipeline.RunState.Messages` or
`sessions.Manager`.

## Related Code Files

- Modify: `cmd/gateway_consumer_normal.go`
- Modify: `cmd/gateway_consumer_deps.go`
- Modify: `cmd/gateway_consumer.go`
- Modify: `cmd/gateway_lifecycle.go`
- Modify: `internal/channels/runs.go`
- Modify: `internal/channels/events.go`
- Modify: `internal/channels/chat_behavior_events_test.go`
- Modify: `internal/pipeline/think_stage.go` only if adding a sidecar-trigger
  event requires a clearer separation from legacy `block.reply`.

## Implementation Steps

1. Parse agent delivery override from `agentLoop.OtherConfig()` before
   run registration.
2. Replace current `ResolveBlockReply(channel, gateway)` and
   `ResolveChatBehavior(channel, gateway)` usage with a single resolver that
   includes agent override.
3. Resolve sidecar provider/model with tenant context before run registration
   and pass the resolved generator metadata into `RegisterRunWithBehavior`.
4. Replace generated intermediate delivery that depends on `block.reply`
   `resp.Content` with a sidecar delivery trigger on bounded tool phases.
5. Keep legacy explicit `block_reply` compatible when users rely on main LLM
   progress text, but default/generated Intermediate Replies should use
   sidecar delivery only.
6. Retire tool-status message output by removing or hard-disabling the
   `rc.ToolStatusEnabled && !rc.Streaming` publish path.
7. Keep reaction statuses for channels that support reactions; these are not
   Tool Status Messages and are less intrusive.
8. Ensure terminal events cancel Quick Ack timers and sidecar generation cannot
   publish after run unregister.
9. Add context isolation tests proving visible ack/intermediate outbound text
   does not change session history or provider request messages.

## Success Criteria

- [x] Channel-level false disables Agent/Workspace-enabled Quick Ack.
- [x] Agent-level true enables behavior when Workspace is disabled and Channel
      inherits.
- [x] Workspace still controls defaults when Agent and Channel inherit.
- [x] Quick Ack disabled does not disable Intermediate Replies, and Intermediate
      Replies disabled does not disable Quick Ack.
- [x] Sidecar Intermediate Reply content is not equal to `resp.Content` from
      tool-call assistant messages unless a fake generator intentionally returns
      that value.
- [x] Tool calls no longer publish deterministic status text.
- [x] Existing Show Reasoning bubble behavior still works.
- [x] Existing final answer dedup for block replies still works.

## Risk Assessment

Risk: removing tool-status messages might also remove useful placeholder cleanup.
Verify empty final outbound and stream finalization still clean placeholders.
