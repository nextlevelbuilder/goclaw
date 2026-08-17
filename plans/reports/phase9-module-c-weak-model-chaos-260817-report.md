# Phase 9 Module C — Weak-Model Chaos Tests Report

> Date: 2026-08-17
> Scope: Module C of Phase 9 (Testing Suite) — `internal/pipeline/weak_model_chaos_test.go`
> Plan: `plans/260815-2340-goclaw-repository-reliability/phase-09-testing-suite.md` (Module C, lines 103-121)

## Summary

Created ONE new test file, `internal/pipeline/weak_model_chaos_test.go` (package `pipeline`),
driving all five weak-model failure scenarios through the REAL
`NewDefaultPipeline(deps).Run(context.Background(), state)` loop. No production code was
changed; no existing files were edited. All 5 tests pass under `-race`; the full
`./internal/pipeline/` suite stays green.

## Scenarios added

| Test | Scenario | Recovery path asserted | Result |
|------|----------|----------------------|--------|
| `TestWeakModel_MalformedToolCall_RepairsAndContinues` | Model emits tool call with truncated args (empty `Arguments` on `read_file`, `finish_reason="tool_calls"`) | ThinkStage truncation repair: `TruncRetries` 0→1, hint appended, loop continues | PASS |
| `TestWeakModel_EmptyOutput_Recovers` | First response empty (`stop`, no tools), second carries content | ThinkStage empty-reply nudge: `EmptyReplyRetries` 0→1, content delivered | PASS |
| `TestWeakModel_PrematureCompletion_GateForcesContinue` | Model says "done" (empty) 3× while zero tool work and no deliverable | ContinuationGate fires → `ContinueAfterFinal` + one extra iteration → 4th answer completes run | PASS |
| `TestWeakModel_RepeatedToolLoop_DetectedNotInfinite` | Model repeats same tool + args every iteration | Loop-kill signal (`state.Tool.LoopKilled`) → ToolStage BreakLoop → bounded at 5 tool calls | PASS |
| `TestWeakModel_InvalidJSON_RepairThenContinue` | First response has `ParseError` tool call (broken JSON envelope), second valid call, third final answer | ThinkStage parse-error repair (`TruncRetries` 0→1), valid tool executes, final content delivered | PASS |

Every test asserts at least one reliability counter moved (the "recovery path invoked" proof)
and that the run COMPLETED (`state.ExitCode != AbortRun`).

## Real symbol names used (phase file guessed these — actuals below)

### Malformed tool call / invalid JSON repair

The phase file guessed `RepairToolCall` / `repairJSON`. The real, reachable pipeline-level
mechanism is the **ThinkStage truncation/parse-error recovery path** in
`internal/pipeline/think_stage.go`:

- `toolCallsHaveMissingRequiredArgs(calls)` (think_stage.go:438) — Gemini-style truncation
  detection: `finish_reason="tool_calls"` with empty `Arguments` on an allowlisted tool
  (`write_file`/`edit`/`exec`/`create_image`/`read_file`, `mutatingToolsRequireArgs` at :425).
- `toolCallsHaveParseErrors(calls)` (think_stage.go:411) — any call with non-empty
  `providers.ToolCall.ParseError` (broken JSON envelope).
- `state.Think.TruncRetries` (run_state.go / substates.go) — the repair counter; bounded by
  `maxTruncRetries = 3` (think_stage.go:16). Incremented on detection, then an assistant +
  user-hint pair is appended and the stage returns Continue. **Resets to 0 on a successful
  response** (think_stage.go:183), so tests capture the counter inside the `CallLLM` closure
  at the moment the repaired call is made.
- `emptyReplyHint` / `maxEmptyReplyRetries = 2` (think_stage.go:22-26) — the bounded
  empty-final-reply nudge. `state.Think.EmptyReplyRetries` is the counter.

The agent-level repair symbols (`normalizeToolCall` → `repairToolCallArgs` in
`internal/agent/loop.go`, `repairJSON` in `internal/agent/json_repair.go`, `repairParseError`
in `internal/agent/toolcall_repair.go`) are REAL but **unreachable from pipeline tests**:
the agent package imports the pipeline package, so a pipeline→agent import would be a cycle.
At runtime the agent's `makeExecuteToolCall` normalizes each call before dispatch
(loop_pipeline_tool_callbacks.go:41); in a pure pipeline test the fake `ExecuteToolCall`
plays that role.

### Premature completion gate

Phase guessed `continueGate`. Real name: **`ContinuationGate`**
(`internal/pipeline/contguard_stage.go`), built by `NewContinuationGate(deps)`. It is an
opt-in stage in the default iteration list (`pipeline.go:44`), reading
`reliability.Default().PrematureCompletion.Enabled`. Enabled in tests via the shared
`enableContinuationGate(t)` helper (contguard_stage_test.go:48), which calls
`reliability.Configure(...)` then sets `rt.PrematureCompletion.Enabled = true`.

Fire condition (contguard_stage.go:54-105): final-answer path, empty content, no deliverable,
not last iteration, not already fired, `EmptyReplyRetries >= maxEmptyReplyRetries`,
`TotalToolCalls == 0`. Effect: `state.Observe.ContinueAfterFinal = true` +
`state.Observe.ContinuationGateFired = true` (both are the REAL marker fields).

### Repeated tool loop

Phase guessed `LoopDetector`. Real name: **`toolLoopState`** in
`internal/agent/toolloop.go` (thresholds `toolLoopWarningThreshold = 3`,
`toolLoopCriticalThreshold = 5`). It is surfaced to the pipeline as `state.Tool.LoopKilled`
via `syncBridgeToState` in `internal/agent/loop_pipeline_tool_callbacks.go`; ToolStage
consumes it (`tool_stage.go:113-117`, `checkExitConditions` at :385) and forces `BreakLoop`.

A pipeline test cannot attach `toolLoopState` (unexported + agent package). The fake
`ExecuteToolCall` simulates the critical signal by setting `state.Tool.LoopKilled = true`
after the 5th identical call — exactly what ToolStage reads. The `state.Tool.LoopDetector`
field exists (`substates.go:65`, typed `any`) but is not consulted by ToolStage; the
`LoopKilled` bool is the real control channel.

## Test-construction notes

- **Config**: each test uses the minimal `PipelineConfig{MaxIterations: N, MaxTokens: 1000}`
  with NO `TokenCounter`/`ContextWindow`. This deliberately hits the `prepareFinalRequest`
  early-return (final_request_guard.go:148-156: `ContextWindow <= 0 && TokenCounter == nil`
  → no budget enforcement) and keeps PruneStage a no-op (budget ≤ 0). This is the same
  pattern the existing `stages_test.go` ThinkStage tests use.
- **`t.Parallel()`**: used on 4 of 5 tests. The gate test
  (`TestWeakModel_PrematureCompletion_GateForcesContinue`) is NOT parallel because
  `enableContinuationGate(t)` mutates the process-wide reliability singleton.
- **Fake tool executor**: the `ExecuteToolCall` closure pattern from `stages_test.go`
  (TestToolStage_SingleTool_ExecutesSequentially) is reused; parallel paths
  (`ExecuteToolRaw`/`ProcessToolResult`) are not needed because every scenario dispatches a
  single tool call.
- **Counter capture**: because `TruncRetries` resets on success, the malformed/JSON tests
  capture it inside the `CallLLM` closure (`truncRetriesAtRepair`) rather than after `Run()`.
  `EmptyReplyRetries` does NOT reset on success, so it is asserted post-run.

## Validation

Executed in Docker (no local Go on this Windows host) against `golang:1.26.0` (Debian
variant — the alpine image lacks gcc for `-race`/cgo):

```
gofmt -l internal/pipeline/weak_model_chaos_test.go   # no output → clean
go vet ./internal/pipeline/                           # PASS
go test -race -timeout=120s ./internal/pipeline/ -run "TestWeakModel" -v   # 5/5 PASS
go test -race -timeout=180s ./internal/pipeline/      # full suite PASS (no regression)
```

Observable gate log line from the run:
```
INFO continuation_gate.fired run_id=run-1 iteration=1 empty_reply_retries=2
```

## Skipped scenarios

None. All five Module C scenarios were drivable through the real pipeline loop with the
existing `PipelineDeps` harness + fake tool executor; no production code was needed.

## Deliverables

- `internal/pipeline/weak_model_chaos_test.go` — new test file (the only file created).
- This report.

Status: DONE
Summary: Module C complete — 5 weak-model chaos tests through the real pipeline loop, all
green under `-race`, full pipeline suite unaffected, gofmt/vet clean, no production changes.
Concerns/Blockers: None. Note for the record: the real repair and loop-detection symbols live
in the agent package (`normalizeToolCall`→`repairToolCallArgs`, `repairJSON`,
`toolLoopState`) and are simulated in pipeline tests via `TruncRetries` (real pipeline-level
repair) and `state.Tool.LoopKilled` (real pipeline-level control channel).
