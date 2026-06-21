---
phase: 1
title: "Characterize Current Parallel Semantics"
status: complete
priority: P1
effort: ""
dependencies: []
---

# Phase 1: Characterize Current Parallel Semantics

## Context Links

- Plan: [plan.md](./plan.md)
- Existing executor: `internal/pipeline/tool_stage.go`
- Existing tests: `internal/pipeline/stages_test.go`
- Loop callbacks: `internal/agent/loop_pipeline_tool_callbacks.go`

## Overview

Add tests first around the current behavior and the desired safety boundaries. This phase should fail on the missing hardening cases before implementation starts.

## Requirements

- Functional: capture current two-phase parallel behavior, stable ordered processing, and sequential fallback.
- Functional: prove current gaps around hook parity, mutating eligibility, and tool budget.
- Non-functional: tests deterministic, no sleep-based timing flake where avoidable.

## Architecture

Use `ToolStage` tests with fake callbacks. Keep tests at pipeline layer first because this is where scheduling, hook firing, budget, and result ordering live. Use small channels/atomic counters only where concurrency must be proven.

## Related Code Files

- Modify: `internal/pipeline/stages_test.go`
- Read: `internal/pipeline/tool_stage.go`
- Read: `internal/pipeline/deps.go`
- Read: `internal/agent/loop_pipeline_adapter.go`

## Implementation Steps

1. Add a test proving parallel processing keeps original result order even when raw execution completes out of order.
2. Add a test proving `PreToolUse` must run before parallel raw execution and can rewrite args.
3. Add a test proving blocked `PreToolUse` calls append synthetic blocked tool messages and do not call raw execution.
4. Add a test proving a batch containing a mutating tool uses sequential execution.
5. Add a test proving a batch containing `exec`, `bash`, `wait`, MCP/unknown, or async tools uses sequential execution.
6. Add a test proving `MaxToolCalls` prevents scheduling extra calls in a parallel batch.
7. Add a test proving fixed concurrency cap bounds peak raw executions.

## Success Criteria

- [x] New tests fail against current implementation for the real gaps.
- [x] Existing parallel path regression remains covered.
- [x] No implementation changes in this phase except test helpers.

## Risk Assessment

Risk: concurrency tests can become flaky. Mitigation: use controlled channels and atomic peak counters, not wall-clock timing.

## Security Considerations

Hook blocking and mutating-tool fallback are security boundaries. Tests must assert denial happens before tool execution.

## Next Steps

Proceed to Phase 2 only after the tests express all approved safety decisions.
