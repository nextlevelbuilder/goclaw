# WS-B Report — Agent Loop + Resume API (Phase 2 Durable Runtime)

Gaps closed: **G2** (resume entry), **G3** (compacting on retry — record-only gate), **G4** (chunk/thinking/tool.started timeline persistence), **G5** (per-phase status enums). Attempt++ deliberately deferred (see Concerns).

## Files modified/created

| File | Change |
|------|--------|
| `internal/agent/run_record.go` | Added `newRunRecordUpdater(ctx, l, runID)` constructor — heartbeat-only, **no CreateRun** (preserves stored checkpoint on resume). `startRunRecord` now returns `newRunRecordUpdater(...)` after CreateRun. |
| `internal/agent/loop_run.go` | `ResumeRun(ctx, runID) (*RunResult, error)`; error-path compacting on checkpoint-written (G3); `runRequestFromRunRecord(run, savedInput)`; `checkpointRunInput(raw)`; sentinels `ErrRunResumeUnavailable` / `ErrRunResumeNotFound`. |
| `internal/agent/loop_pipeline_adapter.go` | `runViaPipeline(ctx, req, resume *pipeline.RunState, checkpoint func(ctx, state) error)` — re-attaches identity on restored state; wires `WriteCheckpoint` into `PipelineDeps`. |
| `internal/agent/loop_pipeline_callbacks.go` | **Unchanged** — task item 5 said don't change unless required. No resume-specific callback change was needed (checkpoint writer lives in `runViaPipeline`). |
| `internal/agent/run_timeline_recorder.go` | `timelineKindForEvent` maps `ChatEventChunk`→chunk/running, `ChatEventThinking`→thinking/thinking, `AgentEventToolStarted`→tool.started/waiting_tool; `timelineContent` persists full content for content-carrying types; `RunTimelineItemContentPersisted` gate. |
| `internal/gateway/methods/run_timeline.go` | `resumer func(ctx, runID) (*agent.RunResult, error)` field + `SetResumer`; registered `protocol.MethodRunsResume`; `handleRunsResume` with viewer-role ownership parity to `handleRunsGet`. |
| `internal/http/traces.go` | `resumer` field + `SetResumer`; `POST /v1/runs/{runID}/resume` route + `handleRunResume` (viewer parity to `handleRunGet`). |
| `pkg/protocol/methods.go` | `MethodRunsResume = "runs.resume"`. |
| `internal/agent/run_record_test.go` | Added `UpdateRunCheckpoint` to stub; `TestNewRunRecordUpdaterSkipsCreateRun`, `TestNewRunRecordUpdaterNilWithoutStore`. |
| `internal/agent/run_resume_test.go` | NEW — `stubResumeRunsStore`; tests: unavailable without store, empty runID, not-found, store-error propagation, identity merge. |
| `internal/agent/run_timeline_recorder_test.go` | Updated: thinking/chunk/tool.started mapping + content; seq tracking advances. |
| `internal/gateway/methods/run_timeline_test.go` | Added `UpdateRunCheckpoint` to `stubRunsStore`. |
| `internal/gateway/methods/run_timeline_resume_test.go` | NEW — resumer-nil→unavailable, success, missing runID, not-found, viewer scoped to own run. |
| `internal/http/run_timeline_test.go` | Added `UpdateRunCheckpoint` to `stubHTTPRunsStore`; NEW HTTP resume tests (503 no-resumer, 200 success, 404 other-user). |
| `docs/04-gateway-protocol.md` | `runs.resume` RPC row + resume docs. |
| `docs/18-http-api.md` | `POST /v1/runs/{runID}/resume` route + Run Resume section. |

## Final signatures

```go
func (l *Loop) ResumeRun(ctx context.Context, runID string) (*RunResult, error)

// errors
var ErrRunResumeUnavailable = errors.New("run resume unavailable: durable run records not wired")
var ErrRunResumeNotFound    = errors.New("run resume failed: run not found or not resumable")

func (m *RunTimelineMethods) SetResumer(resume func(ctx context.Context, runID string) (*agent.RunResult, error))
func (h *TracesHandler)      SetResumer(resume func(ctx context.Context, runID string) (*agent.RunResult, error))

// pkg/protocol/methods.go
MethodRunsResume = "runs.resume"
```

## How ResumeRun avoids checkpoint clobber

`CreateRun` is an ON CONFLICT upsert on `(tenant_id, run_id)` that SETs `checkpoint = EXCLUDED.checkpoint`. Calling it during resume would NULL out the stored checkpoint. `newRunRecordUpdater(ctx, l, runID)` starts only the heartbeat goroutine (keeps `RecoverStaleRuns` from marking a long resume stale), with no create/upsert. Resume finalizes the record: err + checkpoint-written → `compacting` (still resumable), err + no checkpoint → `failed`, nil → `completed`. A deferred `terminal(failed)` safety-net covers panic/goroutine-leak.

`runRequestFromRunRecord` rebuilds identity from the record (session/user/channel/chat) and message/channel/media/workspace from `checkpointRunInput` (the checkpoint JSON carries `input` because `RestoreCheckpoint` intentionally does not restore Input).

## Tests

- `internal/agent`: `run_record_test.go`, `run_resume_test.go`, `run_timeline_recorder_test.go`
- `internal/gateway/methods`: `run_timeline_resume_test.go` (5 tests)
- `internal/http`: `run_timeline_test.go` (3 resume tests)
- Manual-read verified (no local Go): imports, symbols, interface satisfaction, no import cycle.

## Concerns / blockers

1. **WS permission classification missing (REQUIRED for feature to work):** `runs.resume` is NOT in `internal/permissions/policy.go` (`isWriteMethod` / `isReadMethod`). `permissions.MethodRole("runs.resume")` returns `RoleNone` → `gateway/router.go:70` fails closed with `ErrUnauthorized` "not permitted for this session" for ALL clients. **This file is outside WS-B's ownership list** — needs WS-C/controller to add `protocol.MethodRunsResume` to `isWriteMethod` (operator+). My handler tests call `handleRunsResume` directly (bypassing the router), so they pass regardless.
2. **Attempt++ deferred (G3 partial):** `store.RunsStore.UpdateRunStatus(ctx, runID, status)` does not accept an attempt parameter. Advancing `Attempt` on each resume would require an interface change rippling to PG/SQLite impls + all callers. Coordinator prioritized NOT changing the interface this phase. Resume does not reset `Attempt` (no CreateRun). `Attempt` stays at the original value — documented as a deliberate deferral; revisit when the attempt/retry model is designed.
3. **HTTP POST gate blocks viewers:** `POST /v1/runs/{runID}/resume` goes through `requireAuth` where `httpMinRole(POST) = RoleOperator`. So viewer tokens never reach the in-handler own-run check (they 403 at the gate). The in-handler ownership check is meaningful for non-admin operators (browser-paired). HTTP tests use `operator.write` tokens. WS tests use direct handler invocation with viewer/admin roles.
4. **Chunk flood (G4 follow-up):** each chunk/thinking delta persists one timeline row. Coalescing (batch-merge per iteration/interval) is documented as a TODO in `run_timeline_recorder.go` with a debug log — deferred.
5. **Resumer wiring into `cmd/` NOT done — deferred to controller (Task #73):** `cmd/gateway_methods.go` and `cmd/gateway_http_handlers.go` have no `SetResumer` call yet. The surface (`SetResumer` on both handlers + `Loop.ResumeRun` + `MethodRunsResume`) is ready; wiring is the controller's task.

## Verification

- Type/symbol checks done by careful read (no local Go available). All referenced WS-A symbols confirmed: `UpdateRunCheckpoint(json.RawMessage)`, `RunTimelineItemTypeChunk/Thinking/ToolStarted`, `RunTimelineStatusThinking/WaitingTool`, `RunTimelineItemContentPersisted`, `AgentRun.Checkpoint`, `pipeline.RestoreCheckpoint`/`MarshalCheckpoint`.
- Docs verified against code (routes/rows present in both doc files).

Status: DONE_WITH_CONCERNS
Summary: Agent-loop resume (ResumeRun + heartbeat-only updater + finalize) and WS/HTTP resume APIs implemented with tests; timeline chunk/thinking/tool.started persistence added. Two integration items deferred to controller: `runs.resume` permission classification in policy.go (WS method is currently fail-closed) and cmd/ resumer wiring (Task #73).
Concerns: (1) policy.go classification required for WS resume to be reachable — outside my ownership; (2) Attempt++ deferred — interface untouched per coordinator priority; (3) cmd/ wiring not done (controller's Task #73).
