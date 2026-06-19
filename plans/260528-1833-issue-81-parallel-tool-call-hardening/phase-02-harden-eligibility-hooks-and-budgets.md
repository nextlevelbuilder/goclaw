---
phase: 2
title: "Harden Eligibility Hooks and Budgets"
status: complete
priority: P1
effort: ""
dependencies: [1]
---

# Phase 2: Harden Eligibility Hooks and Budgets

## Context Links

- Phase 1 tests: [phase-01-characterize-current-parallel-semantics.md](./phase-01-characterize-current-parallel-semantics.md)
- Tool metadata: `internal/tools/capability.go`
- Registry metadata lookup: `internal/tools/registry.go`
- Pipeline deps: `internal/pipeline/deps.go`

## Overview

Implement conservative eligibility and pre-scheduling safety gates. Keep the existing raw/process split, but only enter it for fully safe batches.

## Requirements

- Functional: parallel path only for calls marked read-only by metadata.
- Functional: `exec`, `bash`, `wait`, mutating, async, MCP-bridged, and unknown tools remain sequential.
- Functional: `PreToolUse` runs before parallel I/O and can block or rewrite args.
- Functional: `MaxToolCalls` is enforced before scheduling a parallel batch.
- Non-functional: single-tool and sequential behavior unchanged.

## Architecture

Add a small callback in `PipelineDeps`, likely `ParallelEligibleToolCall func(tc providers.ToolCall) bool`. Wire it from `agent.Loop` using `l.tools.GetMetadata(l.resolveToolCallName(tc.Name))`.

Suggested rule:

```go
func eligible(tc providers.ToolCall) bool {
    name := resolve(tc.Name)
    if name == "exec" || name == "bash" || name == "wait" || strings.HasPrefix(name, "mcp_") {
        return false
    }
    meta := registry.GetMetadata(name)
    return meta.IsReadOnly() &&
        !meta.HasCapability(tools.CapMutating) &&
        !meta.HasCapability(tools.CapAsync) &&
        !meta.HasCapability(tools.CapMCPBridged)
}
```

Unknown tools should be treated as sequential. If `inferMetadata` currently defaults unknown tools to mutating, preserve that behavior.

## Related Code Files

- Modify: `internal/pipeline/deps.go`
- Modify: `internal/pipeline/tool_stage.go`
- Modify: `internal/agent/loop_pipeline_adapter.go`
- Modify: `internal/pipeline/stages_test.go`
- Read: `internal/tools/capability.go`
- Read: `internal/tools/registry.go`

## Implementation Steps

1. Add `ParallelEligibleToolCall` to `PipelineDeps`.
2. Replace `requiresSequential` with a clearer decision helper:
   - return sequential when any `SequentialToolCall` matches
   - return sequential when any call is not parallel eligible
   - return sequential when required callbacks are nil
3. Move `PreToolUse` processing into a shared preflight helper used before both sequential execution and parallel scheduling.
4. For blocked hooks, append blocked tool messages, increment tool count, and exclude those calls from raw execution.
5. Apply hook argument rewrites before eligibility and scheduling decisions.
6. Enforce `MaxToolCalls` against the whole executable batch before launching goroutines.
7. Keep `PostToolUse` after result processing, preserving existing async semantics.

## Success Criteria

- [x] Phase 1 tests for hook parity, eligibility, and budget pass.
- [x] Existing tests for sequential `wait` barrier still pass.
- [x] Single-tool calls still use `ExecuteToolCall`.
- [x] No mutating tool can enter `ExecuteToolRaw` by default.

## Risk Assessment

Risk: a tool inferred read-only is actually stateful. Mitigation: start with explicit conservative checks and no UI override.

Risk: hook rewrite changes eligibility. Mitigation: apply rewrite before eligibility decision.

## Security Considerations

Do not run any tool raw path before `PreToolUse` has allowed it. A blocked hook must be a hard stop for that call.

## Next Steps

Proceed to Phase 3 after eligibility and hook behavior pass focused tests.
