# Phase 05 — Weak-Model Resilience: Tool-Call Repair + JSON Repair + Empty Recovery + Premature-Completion Gate + Repetition Wiring + Completion Verifier

> **Status: IN PROGRESS** — 2026-08-16. Scope approved by user: Phase 5 (weak-model resilience) theo `GoClaw_Upgrade_Improvement_Plan.md` §10-11.
> **Branch:** `feat/phase5-weak-model` (tạo từ `dev`). Workstream: agents song song, file-ownership disjoint, **agents KHÔNG commit** — controller commit tuần tự + build/test chung.

## Context (scout 2026-08-16, verified)

- **Tool-call parse path**: `internal/pipeline/think_stage.go` `Execute()` (42-219) — decision point duy nhất: `BreakLoop` khi `len(resp.ToolCalls)==0` (line 185); `toolCallsHaveParseErrors`/`toolCallsHaveMissingRequiredArgs` (336-375) + retry hint tối đa `maxTruncRetries=3`; nudge empty bounded (192-200, `maxEmptyReplyRetries=2`).
- **ToolCall struct**: `internal/providers/types.go:186-192` — `ID, Name, Arguments map[string]any, Metadata, ParseError string`. **Wrong field names bị drop silent** (`json.Unmarshal` vào `map[string]any` bỏ key không hợp lệ, không lỗi).
- **Repair hiện có**: `internal/agent/loop.go:63-92` `normalizeToolCall()` (MCP pseudo-call `exec`→name rewrite); `internal/providers/control_output_normalizer.go:181-247` (Kimi text tool-call extraction); `internal/agent/loop_history_sanitize.go:42-190` (history pairing repair). **Không có** generic JSON repair tool-call args.
- **State machine**: `internal/agent/run_record.go:33-115` + `loop_run.go:253-254` — `terminal(ctx, Completed)` là điểm gate DUY NHẤT trước khi đánh dấu completed. `AgentRunStatusRunning/Completed/Failed/Cancelled` (`internal/store/run_timeline_store.go:66-70`).
- **Loop detection đã ship sẵn**: `internal/agent/toolloop.go` — `toolLoopState.record/recordResult/detect` (same args+same result: warning 3, critical 5); `detectReadOnlyStreak` (stuck 8/12 vs exploration 24/36, uniqueness 0.6); `detectSameResult` (4/6). Wired qua `makeCheckReadOnly` (`loop_pipeline_tool_callbacks.go:245-255`) → `checkExitConditions` (`tool_stage.go:385-403`) → BreakLoop. **Loops hoàn chỉnh — chỉ cần wire reliability metrics vào.**
- **CompletionVerifier**: NOT SHIPPED. Plan §11 spec: `CompletionResult{Complete bool, Confidence float64, Missing []string, Reason string}`, levels L0-L4. Readable state: `RunResult` (`loop_types.go:724-737`), `RunState.Observe.FinalContent`, `ToolState.TotalToolCalls/Deliverables`. `TeamTaskData` KHÔNG có acceptance-criteria column.
- **Empty output hiện tại**: think_stage nudge (max 2) + `finalize_stage.go:49-59` `MsgEmptyReplyFallback`; **không wire** vào reliability health (`health.go:41-54` emptyOutputs counter chỉ đếm qua `observeFailure` — model error codes unwired).
- **Taxonomy đã đủ, chưa wire**: `internal/reliability/errors.go:41-48` — `ErrModelEmptyOutput/MalformedToolCall/InvalidJSON/UnsupportedToolCall/RepeatedToolCall/PrematureCompletion/Looping/LowSignal` + classes (retryable/severity). `health.go:100-108` chỉ map 2 code vào counters.
- **Test pattern**: `stubProvider` (intent_classify_test.go:89-107) cho loop tests; `PipelineDeps.CallLLM` closure cho pipeline stage tests; `pipeline_test.go:201-220` cho full Run + `ContinueAfterFinal` (observe_stage.go:118-121 → pipeline.go:93-98).

## Scope (đã duyệt) — 6 modules

| # | Module | Tóm tắt | Owner |
|---|--------|---------|-------|
| 1 | Tool-call repair | `repairToolCallArgs()`: normalize field-name lệch (`arg`→`arguments`, flatten), parse error → schema-aware recovery, bounded | **E** |
| 2 | JSON repair | Pipeline strict repair (fix quotes/brackets/truncation) → 1 lần compact-error retry → fail với `ErrModelInvalidJSON` | **E** |
| 3 | Empty output recovery | Wire `ErrModelEmptyOutput` vào reliability (emptyOutputs counter + health score), keep nudge hiện có | **F** |
| 4 | Premature-completion gate | `ContinuationGate` stage sau ThinkStage: nếu lenient check fail → `ContinueAfterFinal` (dùng cơ chế sẵn có) | **F** |
| 5 | Repetition wiring | `toolloop.go` events → `ReliabilityError` classification (`ErrModelLooping/RepeatedToolCall`) + metrics | **G** |
| 6 | Completion verifier | L0 (output exists) + L1 (tools done — dùng `ToolState` counters) — **không LLM judge, không DB field mới** | **G** |

**Deferred (không làm phase này):** L2 artifact verification (file/DB state thật), L3 task acceptance (cần acceptance-criteria field trên task — không thêm schema phase này), L4 model evaluator, minimal correction model (linear pipeline step 3 trong plan §10.2 — không đưa LLM thứ 2 vào), team task criteria field.

## Contracts (bắt buộc — mọi agent implement theo đúng)

### C1. Tool-call repair (E)
```go
// internal/agent/toolcall_repair.go (mới, E)
func repairToolCallArgs(tc *providers.ToolCall) bool // true = đã sửa
// - Chạy TRƯỚC json.Unmarshal vào map[string]any tại provider boundary — KHÔNG sửa sau khi args đã drop silent.
// - Field-name lệch "arg"→"arguments", "args"→"arguments": move + parse.
// - Nested { name, arguments:{...} } → flatten.
// - ParseError đã đặt (truncated JSON) → strict repair (khép bracket/quote).
// - LRU cache per (toolName, schemaHash) cho quyết định repair — KHÔNG retry vô hạn.
```
- Gọi tại `loop.go normalizeToolCall()` chain (E sửa `loop.go`? — xem Files: normalizeToolCall thuộc agent, E sửa có 1 site).
- Không đổi `ToolCall` struct shape (public contract giữ).
- Khi repair fail → để `ParseError`, ThinkStage retry hint hiện có xử lý.

### C2. JSON repair (E)
```go
// internal/agent/json_repair.go (mới, E)
func repairJSON(raw []byte) ([]byte, bool) // strict-safe: chỉ sửa lỗi chắc chắn (quote/bracket balance), KHÔNG đoán nghĩa
```
- Pipeline (plan §10.2, cắt minimal correction model): `parse → repair → 1 retry compact-error → ErrModelInvalidJSON`.
- Retry message: 1-shot compact hint (không vô hạn — theo `maxTruncRetries=3` tinh thần hiện có).
- Dính với `repairToolCallArgs` (E integration).

### C3. Empty output recovery (F)
- Wire vào `internal/agent/loop_pipeline_callbacks.go` hoặc `think_stage.go` nơi nudge empty (192-200): khi nudge cạn (`EmptyReplyRetries >= maxEmptyReplyRetries`) hoặc empty cuối → `reliability.Wrap(reliability.ErrModelEmptyOutput, ...)` + `Metrics.RecordLLMEmptyOutput()` (thêm method nếu chưa có pattern — xem metrics.go hiện có `RecordLLMStreamStall`).
- Giữ behavior hiện tại (nudge, fallback finalize) — chỉ thêm observe vào reliability, KHÔNG đổi luồng.

### C4. Premature-completion gate (F)
```go
// internal/pipeline/contguard_stage.go (mới, F) — stage sau ThinkStage, trước ToolStage
type ContinuationGate struct{ ... } // StageWithResult + IterationStage
// - Khi ThinkStage trả BreakLoop (no tools, final answer):
//   1. Gate chạy SAU ThinkStage, nhìn state.Think.EmptyReplyRetries + ToolState counters.
//   2. Lenient check: pipeline đã dùng 0 tool iteration → completion sớm, thiếu deliverable → ContinueAfterFinal = true.
//   3. KHÔNG gate khi: run kết thúc bình thường (content + deliverable), iteration = last, `ContinueAfterFinal` vừa dùng.
//   4. Tôn trọng hasDeliverableOutput + last-iteration semantics (think_stage.go:192-196) — không loop cạn kiệt.
```
- Hiện thực qua `ContinueAfterFinal` (observe_stage.go:118-121 + pipeline.go:93-98) — KHÔNG thêm cơ chế mới.
- Config: `reliability.premature_completion.enabled` false default (opt-in) — an toàn product.

### C5. Repetition wiring (G)
```go
// internal/agent/loop_tools.go (G sửa) — trong processToolResult / loopDetector events
func (l *Loop) observeToolLoop(reason LoopViolation, tool string, iteration int)
// - critical (5+) → reliability.Wrap(ErrModelLooping) + Metrics.RecordLLMLoop() (method mới theo pattern metrics.go)
// - warning (3+) → RecordRepeatedToolCall() (method mới) + ErrModelRepeatedToolCall classification
// - Wired vào reliability.Default().Metrics + Health (nil-safe, theo reliability_wiring.go pattern)
```
- **KHÔNG đổi thresholds hiện có** (toolloop.go constants là behavior đã ship, reviewed) — chỉ thêm observability.

### C6. Completion verifier (G)
```go
// internal/agent/completion_verifier.go (mới hoặc trong loop_run.go, G)
type CompletionResult struct {  // plan §11.2
    Complete   bool
    Confidence float64
    Missing    []string
    Reason     string
}
func verifyCompletion(r *RunResult, s *RunState) CompletionResult
// - L0: content + deliverable exists.
// - L1: tool calls đã complete (ToolState.TotalToolCalls > 0, deliverable) / no pending.
// - Gọi tại loop_run.go trước terminal(Completed) (253-254) — KHÔNG đổi quyết định, chỉ ghi vào event + trace.
// - Không LLM judge, không DB field mới, không change terminal decision (record-only phase này).
```

### C7. Observability + taxonomy wiring (chung)
- `internal/reliability/metrics.go` (G sửa): thêm `RecordLLMLoop`, `RecordLLMRepeatedToolCall`, `RecordLLMEmptyOutput`, `RecordLLMPrematureCompletion` theo pattern `RecordLLMStreamStall` (counter + optional label).
- `internal/reliability/health.go`: wire `ErrModelEmptyOutput/MalformedToolCall/InvalidJSON/RepeatedToolCall/PrematureCompletion/Looping` vào `observeFailure`/counters (F/G theo file ownership).
- DOCS: recompute — không đổi public contracts của `internal/reliability` (additive methods only).

### C8. Tests bắt buộc
| Agent | Test |
|-------|------|
| E | `toolcall_repair_test.go`: field-name lệch `arg`→`arguments`, nested flatten, truncated JSON repair, LRU cache, KHÔNG repair khi schema không khớp; `json_repair_test.go`: quote/bracket fix, truncation, retry 1-shot, fail clean |
| F | `contguard_stage_test.go`: gate fires khi 0 tool iteration thiếu deliverable; KHÔNG fire khí content+deliverable đủ; last-iteration không gate; `ContinueAfterFinal` path (theo pipeline_test.go:201-220 pattern); empty-output wire: nudge cạn → ErrModelEmptyOutput + counter delta |
| G | `completion_verifier_test.go`: L0/L1 pass/fail cases (content, deliverable, tool states); loop wiring: critical/warning → metric counters delta + classification; `metrics.go` methods unit (theo singleton_test.go pattern) |

## Files to create/modify

| Agent | Files (disjoint) |
|-------|------------------|
| E | mới `internal/agent/toolcall_repair.go`, `internal/agent/json_repair.go` + tests; sửa `internal/agent/loop.go` (normalizeToolCall chain — 1 site), `internal/agents/...` KHÔNG; đụng `internal/providers/types.go`? KHÔNG (struct giữ) |
| F | mới `internal/pipeline/contguard_stage.go` + test; sửa `internal/pipeline/think_stage.go` (nudge thêm observeErrModelEmptyOutput — thay vì loop_pipeline_callbacks), `internal/pipeline/pipeline.go` (đăng ký stage — nếu cần), `internal/pipeline/finalize_stage.go` (KHÔNG) |
| G | sửa `internal/agent/loop_tools.go`, `internal/agent/loop_run.go` (verifier call — record-only), `internal/reliability/metrics.go`, `internal/reliability/health.go` (+wire), `internal/agent/completion_verifier.go` mới + tests |

**⚠️ KHÔNG AI đụng:** `internal/reliability/errors.go` (taxonomy giữ nguyên), `internal/reliability/circuitbreaker.go`, `ratelimit.go`, `toolloop.go` (thresholds là behavior đã ship — G chỉ đọc + wire), `internal/providers/*` (ngoại trừ test pattern đọc), `internal/store/*` (KHÔNG schema change phase này), UI (KHÔNG — server-side only).

Giao thoa kiểm tra: E sửa `loop.go` normalizeToolCall; F sửa `think_stage.go` nudge + `pipeline.go` stage registration; G sửa `loop_tools.go`/`loop_run.go`/`reliability/*`. **E ∩ F = ∅, E ∩ G = ∅, F ∩ G: `loop_run.go` (G-only, F không đụng), `think_stage.go` (F-only)** — nhưng F đọc `toolloop` state; G đọc `RunState` — read-only cross, OK.

## Implementation Steps

1. Controller viết phase file (file này) → dispatch 3 agents (E/F/G) song song theo contract.
2. Agents implement, tự test **phần mình teste được độc lập** (không đòi compile toàn repo — controller build chung sau khi commit đủ).
3. Controller review từng diff (verify pass: đối chiếu contract, grep callers, không tin self-validation).
4. Commit tuần tự: **E → F → G** (E trước — repair tool-call args là nền; F/G độc lập với nhau).
5. Build + vet + test toàn bộ Docker (PG build + sqliteonly build).
6. Push branch `feat/phase5-weak-model` từ `dev` → PR → CI; mỗi agent theo dõi CI phần mình.
7. CI xanh → merge dev → tick Phase 5 + §37.1 items trong main plan → final report.

## Tests / Validation

- Unit tests C8 pass.
- `go build ./...` + `go build -tags sqliteonly ./...` + `go vet ./...` in Docker (`golang:1.26.0-alpine`, mounts: `C:/Users/DORA/Downloads/goclaw-mod:/src`, `goclaw-gomodcache:/go/pkg/mod`, `goclaw-gomodcache:/root/.cache/go-build`).
- CI PR xanh (go incl. unit + invariants + integration nếu chạy).
- **Không viết** load/benchmark tests (rule repo).

## Risks / Rollback

- **Gate premature-completion false-positive**: agent làm task không cần deliverable (chat-only) bị buộc chạy tiếp → exhaustion. Mitigation: opt-in config, gate chỉ fire khi 0 tool iteration + thiếu deliverable + không phải last iteration; tôn trọng hasDeliverableOutput.
- **Repair tool-call sai semantics**: repair chỉ normalize field-name chắc chắn (arg→arguments), KHÔNG đoán nghĩa mới; LRU cache tránh retry vô hạn. Rollback: tắt qua config flag.
- **Wire metrics không nil-safe** → panic production khi reliability chưa init: bắt buộc nil-safe pattern (reliability.Default() guard như reliability_wiring.go).
- Public contracts: không đổi `ToolCall` struct, không đổi `StageResult` enum, không đổi taxonomy codes. Các method metrics mới additive.
- No DB migration phase này (acceptance-criteria column deferred).