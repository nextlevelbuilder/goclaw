---
phase: 1
title: Characterization Tests
status: completed
priority: P1
effort: ''
dependencies: []
---

# Phase 1: Characterization Tests

## Overview

Lock current behavior before changing semantics. These tests should fail only where issue #118 intentionally changes default quick acknowledgement from fixed templates to generated progress plus fallback.

## Requirements

- Functional: prove current template-only quick ack behavior and `block.reply` delivery gates.
- Functional: prove existing final dedup depends on delivered `block.reply`.
- Non-functional: tests must not use external LLM calls or channel network calls.

## Architecture

Use existing unit-test surfaces:
- `internal/channels/chat_behavior_test.go` for resolver and preview decisions.
- `internal/channels/chat_behavior_events_test.go` for timer, fallback, streaming, and block reply delivery.
- Existing pipeline tests in `internal/pipeline/stages_test.go` already cover `block.reply` only when tool calls exist; add only if a behavior gap is discovered.

No DB, migration, or integration fixture is needed.

## Related Code Files

- Modify: `internal/channels/chat_behavior_test.go`
- Modify: `internal/channels/chat_behavior_events_test.go`
- Optional modify: `cmd/gateway_consumer_normal_test.go` if a focused final-dedup test already exists nearby
- Read: `internal/pipeline/think_stage.go`
- Read: `internal/agent/loop_pipeline_adapter.go`

## Implementation Steps

1. Add resolver tests showing old default template behavior and the intended new default mode contract.
2. Add event tests for generated progress canceling fallback before fallback timer sends.
3. Add event tests for fallback sending only when no `block.reply` has been delivered.
4. Add streaming guard test: generated progress/fallback must not duplicate streaming chunks.
5. Add compatibility tests for explicit fixed-template mode so users can keep old behavior.
6. Run the focused Go test package and confirm new tests fail before implementation.

## Success Criteria

- [ ] Tests describe generated-first, template-fallback behavior.
- [ ] Tests prove no separate LLM call is required.
- [ ] Tests and review prove this issue adds no new persistence path.
- [ ] Tests fail before Phase 2/3 implementation and pass after.

## Risk Assessment

Risk: tests accidentally assert impossible "instant" LLM output. Mitigation: state generated progress depends on main-turn `block.reply`, not pre-LLM response.
