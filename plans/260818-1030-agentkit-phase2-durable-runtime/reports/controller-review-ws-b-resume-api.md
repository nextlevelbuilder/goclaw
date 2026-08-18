# Controller Review — WS-B + Resumer Wiring (Phase 2 Durable Runtime)

Date: 2026-08-18
Scope: verify WS-B deliverables (agent loop + resume API) + controller patch (cmd wiring + policy classification), then commit gate.

## Verdict: APPROVE (with controller patches applied)

## WS-B review — APPROVED

Verified against working tree (git diff + read):

| Item | Verdict |
|------|---------|
| `Loop.ResumeRun(ctx, runID)` (loop_run.go:329) | ✅ Reads run, restores checkpoint, corrupt→fresh-start fallback, `newRunRecordUpdater` heartbeat-only (NO CreateRun → no checkpoint clobber), checkpointWriter continues same checkpoint, finalize: err+checkpointWritten→`compacting` / err+no-checkpoint→`failed` / nil→`completed`, safety-net defer |
| `newRunRecordUpdater` (run_record.go:75) | ✅ Splits heartbeat from CreateRun; `TestNewRunRecordUpdaterSkipsCreateRun` proves 0 CreateRun calls |
| `runRequestFromRunRecord` + `checkpointRunInput` | ✅ RestoreCheckpoint does NOT restore Input — checkpoint JSON carries it; identity merged from record |
| Sentinels `ErrRunResumeUnavailable` / `ErrRunResumeNotFound` | ✅ Distinct so handlers map cleanly |
| WS `runs.resume` handler + `SetResumer` + Register (run_timeline.go) | ✅ Viewer-role ownership parity with handleRunsGet (NotFound leak-prevention) |
| HTTP `POST /v1/runs/{runID}/resume` + `SetResumer` + route (traces.go) | ✅ Operator-gate via requireAuth, in-handler ownership for non-admin operators, error mapping 404/503/500 |
| `MethodRunsResume = "runs.resume"` (methods.go:52) | ✅ |
| Timeline chunk/thinking/tool.started persistence (run_timeline_recorder.go) | ✅ Full content, contentKeeper gate, phase status enums |
| Tests | ✅ run_resume_test (5), run_timeline_resume_test (5), HTTP resume (3), run_record_test additions; stubs updated |
| Docs | ✅ 04-gateway-protocol.md, 18-http-api.md |

## Controller patches (Task #73)

### 1. WS permission classification (REQUIRED — WS-B flagged blocker)

`runs.resume` was unclassified → `permissions.MethodRole("runs.resume")` → `RoleNone` → `gateway/router.go:70` fails closed for ALL clients.
Fix: `internal/permissions/policy.go` — added `protocol.MethodRunsResume` to `isWriteMethod` (operator+), consistent with state-changing RPCs.

### 2. cmd/ resumer wiring

New `cmd/gateway_resume.go`:
- `loopResumer` interface (compile-checked surface, `Agent` interface has no `ResumeRun`)
- `makeRunResumer(agents *agent.Router, runs store.RunsStore) func(ctx, runID) (*agent.RunResult, error)`:
  1. `runs.GetRun(ctx, runID)` → `run.AgentID` (UUID)
  2. `agents.Get(ctx, run.AgentID.String())` → `agent.Agent` (resolver builds `*Loop` with `RunsStore` wired)
  3. type-assert to `loopResumer` → `l.ResumeRun(ctx, runID)`
- nil-safe: no store/router → nil (handlers report unavailable)

Wired into:
- `cmd/gateway_methods.go` (L32) — `runMethods.SetResumer(makeRunResumer(agents, runsStore))` in `registerAllMethods`
- `cmd/gateway.go` (after wireHTTP) — `tracesH.SetResumer(makeRunResumer(agentRouter, pgStores.Runs))` guarded `tracesH != nil && pgStores != nil`

## Build/test gate (Docker)

| Check | Result |
|-------|--------|
| `go build ./...` (PG) | ✅ |
| `go build -tags sqliteonly ./...` | ✅ (fixed missing `encoding/json` import in `internal/store/sqlitestore/run_timeline.go`) |
| `go vet ./...` | ✅ (fixed `mockTokenCounter` ptr receiver in pipeline_resume_test.go:95; removed unused uuid import in pg/run_timeline_checkpoint_test.go) |
| `go vet -tags sqliteonly ./...` | ✅ |
| `go test ./internal/pipeline/` | ✅ (fixed test bug: system prompt slot vs history) |
| `go test ./internal/agent/` | ✅ |
| `go test ./internal/gateway/methods/` | ✅ |
| `go test ./internal/permissions/` | ✅ |
| `go test ./internal/http/ -run 'Run|Resume|Timeline'` | ✅ |
| `go test ./internal/store/pg/ -run Checkpoint` | ✅ |
| `go test -tags sqliteonly ./internal/store/sqlitestore/ -run Checkpoint` | ✅ |

### Test fixes made during gate

1. **sqlitestore/run_timeline.go**: missing `encoding/json` import (WS-A's `UpdateRunCheckpoint(ctx, ..., json.RawMessage)` needed it; PG build didn't surface because PG path imports json elsewhere).
2. **pipeline/pipeline_resume_test.go:95**: `mockTokenCounter{...}` → `&mockTokenCounter{...}` (Count has pointer receiver).
3. **store/pg/run_timeline_checkpoint_test.go**: removed unused `uuid` import.
4. **pipeline/run_state_checkpoint_test.go**: `TestRunStateCheckpointMessageCapTrimsHistory` was testing with the system prompt inside `history` then expecting it preserved as index 0. Real model: system prompt lives in `MessageBuffer.system` (set by ContextStage), `All()` = [system, history, pending]. Fixed test to `SetSystem(sys)` + `SetHistory(users)` — cap then keeps system + recent 199.

### Pre-existing (not Phase 2) — documented

- **`internal/http` Ollama model-list tests fail** in Docker (`provider_models_test.go`, `providers_ollama_url_test.go`): `dial tcp host.docker.internal:... connection refused`. Tests bind httptest on localhost; provider code rewrites localhost→`host.docker.internal` inside container. No Phase 2 file touches `internal/providers/` or those tests (git diff confirms). Environment artifact, not a regression.
- **Duplicate `runStaleRunsSweep`** (gateway_managed.go:665 from Phase 10 + gateway_heartbeat.go:110-116 pre-existing): two sweep goroutines on PG path. `RecoverStaleRuns` is idempotent (heartbeat-gated), so harmless — just redundant. Out of Phase 2 scope; noted for a future cleanup commit.

## Notes

- Attempt++ (G3) deferred deliberately: `UpdateRunStatus(ctx, runID, status)` has no attempt param; changing `RunsStore` interface ripples to PG/SQLite + all callers. Resume does not reset Attempt (no CreateRun). Recorded in WS-B report.
- Chunk-flood coalescing deferred (TODO in run_timeline_recorder.go) — each chunk persists a row; coalesce later.

Status: DONE
Summary: WS-B resume surface APPROVED; controller added policy classification + cmd resumer wiring; Docker gate green for all Phase 2 packages; 4 test/import fixes landed; Ollama HTTP failures confirmed pre-existing env artifacts.
