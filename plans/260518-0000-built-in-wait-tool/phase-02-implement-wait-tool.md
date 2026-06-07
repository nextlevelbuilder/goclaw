---
phase: 2
title: "Implement Wait Tool"
status: complete
effort: "2h"
---

# Phase 2: Implement Wait Tool

## Overview

Implement the smallest production-safe `wait` tool and wire it through every runtime visibility layer.

## Requirements

- Functional: `wait({timeMs, reason?})` delays the current agent action sequence and then returns a concise success result.
- Bounds: default minimum 100ms, default maximum 300000ms.
- Per-agent override: `agents.tools_config` may include `{"wait":{"min_ms":500,"max_ms":60000}}`; invalid overrides are ignored or clamped to absolute safety bounds.
- Cancellation: if the run context is cancelled while waiting, return an error quickly.
- Concurrency: no package-level locks or shared timers.
- Ordering: if any resolved tool call in the model response is `wait`, execute that tool-call batch sequentially so `message -> wait -> message` preserves order.
- Cancellation: after a cancelled wait, the sequential batch aborts before later side-effecting calls.
- Abuse guard: a same-response wait batch is capped to 300000ms cumulative wait time.

## Related Code Files

- Modify: `internal/tools/wait.go`
- Modify: `internal/tools/policy.go`
- Modify: `internal/tools/capability.go`
- Modify: `internal/tools/context_keys.go`
- Modify: `internal/pipeline/deps.go`
- Modify: `internal/pipeline/tool_stage.go`
- Modify: `internal/agent/loop_pipeline_adapter.go`
- Modify: `internal/config/config_channels.go`
- Modify: `internal/agent/loop_context.go`
- Modify: `internal/store/run_context.go`
- Modify: `cmd/gateway_tools_wiring.go`
- Modify: `cmd/gateway_builtin_tools.go`
- Modify: `ui/web/src/types/agent.ts`
- Modify: `ui/web/src/pages/agents/agent-detail/agent-overview-tab.tsx`
- Modify: `ui/web/src/pages/agents/agent-detail/config-sections/tool-policy-section.tsx`
- Modify: `ui/web/src/i18n/locales/{en,vi,zh}/agents.json`
- Tests: `internal/tools/wait_test.go`, focused existing tests as needed

## Architecture

Agent loop injects per-agent wait limits into context. The registry calls `WaitTool.Execute(ctx,args)`. `Execute` validates `timeMs`, applies limits, waits on `time.NewTimer`, and selects on `ctx.Done()` for interruption.

`ToolStage` treats resolved `wait` tool calls as a sequential barrier. This disables the multi-tool parallel raw path for that assistant response, preserving order for same-turn tool batches.

## Implementation Steps

1. Define `config.WaitToolPolicy` and add `Wait *WaitToolPolicy` to `ToolPolicySpec`.
2. Add `tools.WithWaitToolConfig` / `WaitToolConfigFromCtx`, with RunContext fallback.
3. Inject `l.agentToolPolicy.Wait` in `Loop.injectContext`.
4. Add a pipeline dependency hook that can mark a resolved tool call as sequential-only, and wire it from the agent loop using `resolveToolCallName`.
5. Add `WaitTool` in `internal/tools/wait.go`.
6. Register `tools.NewWaitTool()` next to `datetime` in `wireExtraTools`.
7. Seed `wait` in `builtinToolSeedData` as runtime enabled by default.
8. Add `wait` to `runtime`, `goclaw`, coding profile if needed, and neutral metadata as appropriate.
9. Mark `wait` neutral in agent tool-loop detection so intentional delay sequences do not count as read-only no-progress loops.
10. Update Web agent settings types/save path and add compact wait min/max controls under tool policy so UI edits do not drop `tools_config.wait`.

## Tests Before

- `go test ./internal/tools -run "TestWaitTool|TestToolGroups|TestInferMetadata"`
- `go test ./internal/store -run TestParseToolsConfig`
- `go test ./internal/pipeline -run TestToolStage`
- `pnpm -C ui/web build` if frontend settings are changed

## Tests After

- Same focused tests plus `go test ./cmd -run BuiltinTool` if existing cmd tests cover seed data.

## Success Criteria

- [x] `wait` appears in provider definitions when policy allows runtime tools.
- [x] `wait` is absent when globally disabled by builtin tool settings.
- [x] A same-response `message, wait, message` batch uses sequential tool execution.
- [x] Cancellation returns before the requested delay.
- [x] No sleeping test exceeds a few hundred milliseconds.

## Risk Assessment

- Long sleeps can tie up one agent run goroutine; bounded max and cancellation prevent indefinite hangs.
- Rate limiting might count wait as a tool execution; accepted for v1 because it prevents abusing wait as a rate-limit bypass.
- Progress notifications for >1 minute are deferred because no existing low-risk tool callback emits user progress.
- UI save path currently reconstructs `tools_config`; missing `wait` in that object would silently erase agent-specific wait limits.
