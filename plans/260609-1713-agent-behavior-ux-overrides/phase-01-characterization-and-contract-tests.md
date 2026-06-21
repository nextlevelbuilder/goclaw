---
phase: 1
title: "Characterization and Contract Tests"
status: completed
priority: P1
effort: "0.5d"
dependencies: []
---

# Phase 1: Characterization and Contract Tests

## Overview

Add failing tests before changing runtime behavior. This phase locks the current
surfaces and proves the target contract: no tool-status messages, deterministic
override order, and no context pollution from delivery-only messages.

## Requirements

- Functional: characterize existing Workspace and Channel behavior resolution.
- Functional: define expected Agent override behavior before implementation.
- Functional: prove Quick Ack and Intermediate Replies resolve independently.
- Functional: prove Quick Ack and Intermediate Replies are delivery events, not
  persisted session messages or main-provider messages.
- Functional: characterize the current Quick Ack bug: `llm_generated` mode does
  not call an LLM today and only falls back to template delivery.
- Non-functional: tests must be deterministic and must not call real providers.

## Architecture

Use package-level unit tests around existing resolver/event boundaries:
`internal/channels/chat_behavior.go`, `internal/channels/events.go`, and
`cmd/gateway_consumer_normal.go`. Add fake provider/generator interfaces only
inside tests until the implementation phase introduces the real seam. Include a
fake tenant-aware provider registry because configured sidecar provider names
cannot be exercised through the current `ConsumerDeps` shape.

## Related Code Files

- Modify: `internal/channels/chat_behavior_test.go`
- Modify: `internal/channels/chat_behavior_events_test.go`
- Create: `cmd/gateway_consumer_behavior_test.go`
- Modify: `ui/web/src/pages/channels/channel-schemas.test.ts`
- Modify: `ui/web/src/pages/config/sections/behavior-chat-card.tsx` tests if a
  local test file already exists; otherwise defer UI assertions to phase 5.

## Implementation Steps

1. Add resolver tests for `Channel > Agent > Workspace` with all three layers
   setting conflicting Quick Ack and Intermediate Replies values.
2. Add a test proving unset Agent behavior inherits Workspace and Channel still
   wins when both Agent and Channel are set.
3. Add tests proving Quick Ack can be disabled while Intermediate Replies stay
   enabled, and the reverse.
4. Add a characterization test proving current `llm_generated` Quick Ack does
   not call a provider, so phase 3 must add the sidecar path rather than only
   changing labels.
5. Add event tests proving a sidecar-generated Quick Ack publishes exactly one
   outbound message and cancels on sidecar or final delivery.
6. Add a test proving sidecar Quick Ack failure or timeout falls back to the
   configured template without blocking the main run.
7. Add a history/context test around the main inbound path proving visible
   ack/progress text does not enter `SessionStore` history or either the main
   provider request messages.
8. Add a provider-resolution test proving explicit delivery provider/model beats
   the agent provider/model and falls back when unset.
9. Add a tool-status retirement test proving tool calls no longer emit
   placeholder `formatToolStatus()` messages.
10. Add UI schema and form-serializer tests proving channel schemas include
    Quick Ack and Intermediate Reply override fields, preserve nested
    provider/model/timeout values, and do not expose Tool Status.

## Success Criteria

- [x] Tests fail for Agent override support before phase 2.
- [x] Tests fail for sidecar Quick Ack before phase 3.
- [x] Tests fail for sidecar Intermediate Replies before phase 3.
- [x] Tests fail when Quick Ack and Intermediate Replies are accidentally
      coupled through the same `quick_ack.enabled` gate.
- [x] Tests fail for tool-status message retirement before phase 4.
- [x] No test requires a live LLM provider, network, or zuey access.

## Risk Assessment

The main risk is writing tests against invented helper APIs. Keep tests anchored
to existing public functions first, then introduce small interfaces only where
the implementation needs them.
