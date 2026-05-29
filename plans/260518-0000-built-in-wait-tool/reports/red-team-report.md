# Red Team Report: Built-in Wait Tool

## Findings

### Critical: same-turn tool batches can bypass wait ordering

Evidence: `internal/pipeline/tool_stage.go` sends any response with more than one tool call through `executeParallel` when raw/process callbacks are present. The raw path starts all tool I/O goroutines before sequential result processing. A model response containing `message`, `wait`, `message` can therefore send both messages before the wait completes.

Disposition: Accept. Add a sequential-only dependency hook in the pipeline and wire it from the agent loop using the resolved registry name. Any batch containing resolved `wait` must use the existing sequential `ExecuteToolCall` path.

### High: UI save can erase per-agent wait config

Evidence: `ui/web/src/pages/agents/agent-detail/agent-overview-tab.tsx` rebuilds `tools_config` with only `profile`, `allow`, `deny`, `alsoAllow`, and `byProvider`. `ui/web/src/types/agent.ts` has no wait config field. If backend accepts `tools_config.wait`, the next agent settings save drops it.

Disposition: Accept. Add UI type, save mapping, and compact controls.

### Medium: repeated wait calls can look like read-only no-progress

Evidence: `internal/agent/toolloop.go` treats only `exec`, `bash`, and `mcp_*` as neutral. All non-mutating, non-neutral tools increment read-only streak. A message/wait/message pattern is fine, but wait-only polling or long staged waits can trigger irrelevant warnings.

Disposition: Accept. Classify `wait` as neutral.

## Rejected / Deferred

- `wait_until` deferred. Issue asks to consider it, not include v1.
- Long-wait progress notification deferred. Existing tool event path is generic; adding user-facing progress now increases scope.

## Unresolved Questions

- None.
