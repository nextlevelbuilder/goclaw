---
phase: 2
title: "Timeline Persistence"
status: complete
priority: P1
effort: "1.5d"
dependencies: [1]
---

# Phase 2: Timeline Persistence

## Context Links

- Event enrichment: `internal/agent/loop_run.go`
- Tool events: `internal/agent/loop_pipeline_tool_callbacks.go`
- Block replies: `internal/pipeline/think_stage.go`
- Final/block reply dedup reference: `internal/pipeline/observe_stage.go`

## Overview

Persist timeline items from existing agent events without changing delivery behavior. This is an event capture layer, not a new chat behavior engine.

## Key Insights

- The shared enriched emitter boundary already carries run, user, channel, chat, session, and tenant context.
- Parallel tool calls can emit from goroutines, so `seq` must be assigned before async persistence.
- `block.reply` already excludes final answers; this helps avoid final duplicate archive items.

## Requirements

- Functional: capture `run.started`, `activity`, `block.reply`, `tool.call`, `tool.result`, `run.completed`, `run.failed`, `run.cancelled`.
- Functional: link tool timeline items to trace/span IDs where the current code can provide them without invasive rewrites.
- Functional: store final answer once; avoid duplicating the last `block.reply` as final answer.
- Non-functional: best-effort persistence must not block agent execution if DB write fails.
- Non-functional: preserve tenant/session/user/channel context already attached in `AgentEvent`.

## Architecture

Introduce a small timeline recorder owned by the agent loop. It receives enriched `AgentEvent` values from the same emitter boundary used by live WS/channel broadcast.

Ordering:
- Maintain per-run atomic sequence counter in the loop/request scope.
- Assign `seq` before async persistence.
- For parallel tool calls, sequence reflects event emission order, not completion timestamp.

Privacy:
- `tool.call`: title/tool name + safe args preview. No full arguments.
- `tool.result`: status + safe result preview. No full result.
- `thinking`: redacted `activity` marker only.
- `block.reply` and final answer: content stored because user-visible.

## Related Code Files

- Create: `internal/agent/run_timeline_recorder.go`
- Create: `internal/agent/run_timeline_recorder_test.go`
- Modify: `internal/agent/loop_types.go`
- Modify: `internal/agent/loop_run.go`
- Modify: `internal/agent/loop_pipeline_callbacks.go`
- Modify: `internal/agent/loop_pipeline_tool_callbacks.go`
- Modify: `internal/agent/loop_tracing.go` only if span ID linkage can be added narrowly.

## Implementation Steps

1. Write recorder unit tests first:
   - event to item mapping.
   - preview truncation and redaction.
   - no raw thinking.
   - final answer dedup when same as last block reply.
   - tool error status mapping.
2. Add a `RunTimelineStore` dependency to loop config/wiring.
3. Add recorder creation in `Loop.Run` after run context is known.
4. Wrap `emitRun` so each relevant event is recorded and then emitted live.
5. Keep recorder DB writes best-effort with bounded timeout/logging.
6. Add tests for sequential and parallel tool event ordering.
7. Verify no #67 behavior changes: no ack, no message splitting, no channel delivery toggles.

## Success Criteria

- [ ] Every supported event type maps to exactly one timeline item shape.
- [ ] Tool args/results are persisted as previews only.
- [ ] Final answer is not duplicated after a matching block reply.
- [ ] Recorder failures do not fail the agent run.
- [ ] Parallel tool call and result events stay ordered by assigned `seq`.

## Todo List

- [ ] Add recorder mapper tests.
- [ ] Add privacy/redaction tests.
- [ ] Add sequence ordering tests.
- [ ] Wire store dependency into loop config.
- [ ] Wrap enriched event emitter with recorder.
- [ ] Add best-effort persistence timeout/logging.

## Risk Assessment

Main risk: recording at too many call sites creates inconsistent item shapes. Mitigation: record at the shared enriched emitter boundary and keep payload normalization centralized.

## Security Considerations

Use the same sanitizer/redaction approach across tool call and result previews. Never persist raw thinking. Log recorder failures without dumping payloads.

## Next Steps

Proceed to Phase 3 after recorder tests prove stable event-to-item mapping.
