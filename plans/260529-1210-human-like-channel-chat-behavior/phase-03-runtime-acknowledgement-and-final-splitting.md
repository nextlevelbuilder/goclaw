---
phase: 3
title: "Runtime Acknowledgement and Final Splitting"
status: pending
priority: P1
effort: "1d"
dependencies: [1, 2]
---

# Phase 3: Runtime Acknowledgement and Final Splitting

## Overview

Attach resolved chat behavior to `RunContext`, send conservative quick acknowledgements for non-streaming channel runs, and split final assistant delivery after final content sanitization.

## Requirements

- Functional: ack only for non-streaming channel delivery and never for Web UI runs.
- Functional: ack skips when config disabled, run completes before threshold, or a `block.reply` already delivered.
- Functional: final split preserves existing `block.reply` and final dedup behavior.
- Non-functional: no goroutine leaks, no sleeps in tests, cancellable timers.

## Architecture

Extend `RunContext` with a resolved behavior struct and ack state. Use injected clock/timer helpers where needed for tests.

Event handling path:
- `run.started`: begin ack timer only if non-streaming and enabled.
- `block.reply`: mark intermediate content delivered; cancel pending ack.
- `run.completed`: cancel pending ack, split final content if enabled, publish outbound parts.
- `run.failed`/`run.cancelled`: cancel pending ack.

Final split happens in the channel manager outbound forwarding path, before `bus.PublishOutbound`. Platform adapters still enforce hard message limits afterward.

## Related Code Files

- Modify: `internal/channels/channel.go`
- Modify: `internal/channels/runs.go`
- Modify: `internal/channels/events.go`
- Modify: channel registration call sites that pass `blockReply`/`toolStatus`
- Tests: `internal/channels/events_test.go`, `internal/channels/runs_test.go`

## Implementation Steps

1. Add tests for ack gate using fake timer/clock.
2. Add tests for no ack on streaming runs, disabled behavior, `block.reply`, quick completion, run failure/cancel.
3. Add tests for final split publish count/order and metadata preservation.
4. Implement `RunContext` behavior state and cancellation.
5. Route final content through semantic splitter, then publish ordered outbound messages with configured delay.
6. Ensure existing streaming and reaction handling unchanged.

## Success Criteria

- [ ] Ack publish is deterministic under tests with no real-time sleeps.
- [ ] Final split preserves routing metadata and tenant ID.
- [ ] Existing `block.reply` tests still pass.
- [ ] No archive/timeline storage touched.

## Risk Assessment

Risk: ack can become spam. Mitigation: default disabled/conservative, non-streaming only, cancel on `block.reply`, one ack max per run.
