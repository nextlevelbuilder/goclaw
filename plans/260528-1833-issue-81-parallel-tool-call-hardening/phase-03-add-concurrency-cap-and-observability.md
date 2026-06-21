---
phase: 3
title: "Add Concurrency Cap and Observability"
status: complete
priority: P2
effort: ""
dependencies: [2]
---

# Phase 3: Add Concurrency Cap and Observability

## Context Links

- Phase 2 safety gates: [phase-02-harden-eligibility-hooks-and-budgets.md](./phase-02-harden-eligibility-hooks-and-budgets.md)
- Tool spans: `internal/agent/loop_pipeline_tool_callbacks.go`
- Tracing docs: `docs/10-tracing-observability.md`

## Overview

Bound parallel execution with a fixed default and make batches debuggable in logs/traces without adding new settings UI.

## Requirements

- Functional: parallel raw I/O uses fixed bounded concurrency.
- Functional: stable result aggregation/order remains unchanged.
- Functional: partial per-tool errors stay visible as per-call tool results when represented as `tools.Result`.
- Non-functional: no user-facing config or web UI in this phase.
- Non-functional: log fields must avoid full arguments/secrets.

## Architecture

Add a package-level constant in `internal/pipeline/tool_stage.go`, for example:

```go
const defaultParallelToolCallLimit = 4
```

Use a semaphore channel inside `executeParallel`. Store results by original index as today. Do not reorder by finish time.

Observability should be low-noise:
- batch start/end debug/info logs with run/session, count, limit
- per-call index/name/id in existing `tool.call` and span data
- avoid full argument logging beyond existing `args_len`

If trace attributes are easy through existing span APIs, add batch index/count metadata. If not, keep this phase to logs and existing per-tool spans to avoid a tracing schema detour.

## Related Code Files

- Modify: `internal/pipeline/tool_stage.go`
- Modify: `internal/pipeline/stages_test.go`
- Optional modify: `internal/agent/loop_pipeline_tool_callbacks.go`
- Optional modify: `docs/10-tracing-observability.md`

## Implementation Steps

1. Add `defaultParallelToolCallLimit`.
2. Gate raw goroutines through a semaphore.
3. Add a unit test that raw execution peak never exceeds the fixed limit.
4. Add batch start/end logs with safe metadata.
5. Verify cancellation behavior still waits for launched goroutines and does not orphan spans.
6. If simple, enrich tool span metadata with batch index/count; otherwise document why logs + existing spans are sufficient for v1.

## Success Criteria

- [x] Peak raw execution count is bounded in test.
- [x] Result order remains original assistant tool-call order.
- [x] Logs indicate when a parallel batch starts and completes.
- [x] No settings schema, migration, API, or web UI changes introduced.

## Risk Assessment

Risk: too low cap leaves latency wins on table. Mitigation: fixed `4` is conservative; make configurable later only with evidence.

Risk: logs leak sensitive args. Mitigation: log tool names, ids, counts, durations, args length only.

## Security Considerations

Do not emit raw tool args or outputs in new logs. Existing trace verbose mode controls larger previews.

## Next Steps

Proceed to Phase 4 after bounded parallelism is verified.
