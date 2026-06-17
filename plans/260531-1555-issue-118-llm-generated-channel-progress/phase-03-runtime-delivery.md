---
phase: 3
title: Runtime Delivery
status: completed
priority: P1
effort: ''
dependencies:
  - 1
  - 2
---

# Phase 3: Runtime Delivery

## Overview

Route generated progress through existing `block.reply` channel delivery and use fixed templates only as the fallback path.

## Requirements

- Functional: generated `block.reply` can be delivered for chat behavior even when global `gateway.block_reply` is false.
- Functional: explicit `gateway.block_reply=true` preserves current full block reply behavior.
- Functional: fallback template cancels when a generated `block.reply` arrives.
- Functional: streaming channels skip generated progress/fallback to avoid duplicate chunks.
- Non-functional: do not store generated progress and do not create another LLM call.
- Non-functional: do not change existing run timeline recorder behavior.

## Architecture

Current flow:
- `cmd/gateway_consumer_normal.go:242` resolves global/per-channel `block_reply`.
- `cmd/gateway_consumer_normal.go:245` resolves `chat_behavior`.
- `internal/channels/events.go:276` drops `block.reply` unless `RunContext.BlockReplyEnabled` is true.
- `internal/channels/events.go:292` cancels quick ack when a `block.reply` is delivered.

Target flow:
- Register runs with both explicit `block_reply` and chat behavior.
- In channel event handling, allow `block.reply` delivery when either:
  - explicit block reply is enabled, or
  - chat behavior quick ack mode is `llm_generated` and non-streaming.
- In generated-progress mode, mark `blockReplySent` and cancel fallback timer after a generated message is published.
- Keep final dedup aligned with actual delivered generated progress, not only explicit `gateway.block_reply`.

## Related Code Files

- Modify: `internal/channels/events.go`
- Modify: `internal/channels/runs.go`
- Modify: `internal/channels/manager.go`
- Modify: `cmd/gateway_consumer_normal.go`
- Modify: `internal/channels/chat_behavior_events_test.go`
- Read only: `internal/agent/run_timeline_recorder.go`
- Read: `internal/pipeline/think_stage.go`
- Read: `internal/pipeline/observe_stage.go`
- Read: `internal/agent/loop_pipeline_adapter.go`

## Implementation Steps

1. Add runtime helper(s) that answer:
   - should schedule fallback template?
   - should deliver generated `block.reply`?
   - was a generated/fixed interim message delivered and should final dedup run?
2. Update run registration context if a new resolved flag is needed; avoid shared mutable global state.
3. Change `AgentEventBlockReply` handling to use generated-progress eligibility as well as explicit block reply.
4. Keep streaming guard before publishing outbound.
5. Ensure fallback timer is canceled when generated progress publishes.
6. Ensure final dedup checks actual delivered interim reply when chat behavior generated mode delivered one.
7. Keep retry/tool status messages unchanged unless tests show direct conflict.

## Success Criteria

- [ ] Non-streaming channel can receive LLM-generated progress from main-turn `block.reply` with `gateway.block_reply=false`.
- [ ] Fallback template sends only if no generated progress was delivered before delay.
- [ ] Streaming channel gets no duplicate progress messages.
- [ ] Existing explicit block reply behavior and final dedup remain covered.

## Risk Assessment

Risk: generated progress could duplicate final answer if final dedup remains tied to explicit block reply only. Mitigation: base dedup on actual delivered interim reply count/content.
