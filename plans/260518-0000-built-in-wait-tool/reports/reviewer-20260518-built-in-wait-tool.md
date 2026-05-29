# Built-in Wait Tool Review

## Scope

- Files: `internal/tools/wait.go`, `internal/pipeline/tool_stage.go`, agent context/policy wiring, Web agent settings, locale files, plan docs.
- LOC: tracked diff 193 additions / 20 deletions across 20 files, plus new `wait.go` 114 lines and `wait_test.go` 83 lines.
- Focus: correctness, security, concurrency, cancellation, policy/UI regression.
- Scout findings: order barrier wired through resolved tool name; aggregate wait budget and cancellation-in-batch are the risky paths.

## Overall Assessment

Implementation is mostly coherent. Same-response `message -> wait -> message` ordering is fixed for the normal path by forcing the whole batch through sequential `ExecuteToolCall` when any resolved call is `wait`. Per-agent bounds are parsed, injected, clamped, and UI save now preserves `wait`.

Blocking issue: cancellation during `wait` does not stop later same-batch tool side effects.

## Critical Issues

- [internal/tools/wait.go:73] `wait` returns `ErrorResult` on `ctx.Done()`, but [internal/pipeline/tool_stage.go:50] continues the sequential batch and can execute later calls such as `message` before the pipeline checks `ctx.Err()` at [internal/pipeline/pipeline.go:96]. This breaks cancellation safety and can send messages after user abort.
  Fix: in `ToolStage`, check `ctx.Err()` before and after each sequential tool call, set `s.result = AbortRun`, and return before executing subsequent calls. Add regression test: `message, wait(cancelled), message` must not run the second message.

## High Priority

- [internal/pipeline/tool_stage.go:50] A single assistant response can contain many `wait` calls; budget is only checked after the whole batch at [internal/pipeline/tool_stage.go:104] and [internal/pipeline/tool_stage.go:194]. With default `max_tool_calls=25` and max wait 300000ms, one response can occupy an agent lane for up to 125 minutes unless manually cancelled.
  Fix: enforce remaining tool-call budget before each call, or pre-truncate/reject batch calls that exceed remaining budget. For sequential wait batches, consider a per-response cumulative wait cap.

## Medium Priority

- [internal/agent/loop_pipeline_adapter.go:144] Prefixed wait calls are handled through `resolveToolCallName`, so behavior appears correct, but [internal/pipeline/stages_test.go:970] only tests raw `wait`.
  Fix: add a focused adapter/stage regression test with `ToolCallPrefix: "proxy_"` and calls `proxy_message, proxy_wait, proxy_message`.

## Low Priority

- [plans/260518-0000-built-in-wait-tool/phase-01-research-and-tdd-design.md] Phase 1 is marked complete, but its success checklist is still unchecked. Phase 2 remains in progress and Phase 3 pending. Plan status should be synced after fixes.

## Edge Cases Found by Scout

- Cancellation inside a wait batch can allow later side effects.
- Per-call max wait is bounded, but cumulative same-response waits are not.
- Prefix resolution is implemented, but missing direct regression coverage.
- UI now preserves `wait` and `toolCallPrefix` when saving enabled tool policy.

## Positive Observations

- No shared timer or package-level mutable state in `WaitTool`.
- Wait bounds clamp to absolute safety limits.
- Runtime/coding/messaging/full visibility paths are covered through policy groups/profiles.
- Focused Go tests and Web build pass.

## Recommended Actions

1. Block landing until cancellation stops the rest of a sequential batch.
2. Enforce max tool-call budget before every tool execution, not after a batch.
3. Add prefixed wait-ordering regression test.
4. Sync plan checkboxes/status after fixes.

## Metrics

- Type Coverage: not measured.
- Test Coverage: focused tests pass; coverage percentage not measured.
- Linting Issues: `git diff --check` clean for reviewed files; CRLF warnings only for `internal/config/config_channels.go` and `internal/pipeline/deps.go`.

## Verification

- `go test ./internal/tools ./internal/pipeline ./internal/agent ./internal/store ./cmd` passed.
- `pnpm build` in `ui/web` passed with existing Vite chunk-size warnings.
- `git diff --check` passed for reviewed files.

## Unresolved Questions

- Should wait have a stricter cumulative per-response cap than generic tool-call budget?
