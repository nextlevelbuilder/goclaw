# Agent F Report — Phase 5 Modules 3+4 (Empty Output Recovery + Premature-Completion Gate)

Date: 2026-08-17 · Branch: feat/phase5-weak-model · Files owner: F (pipeline)

## Implemented

### C3 — Empty output recovery (wire vào reliability)
- `internal/pipeline/think_stage.go` (modified):
  - Nudge block tách `emptyFinal` predicate; behavior nudge GIỮ NGUYÊN (max 2, bounded).
  - Khi nudge cạn (`EmptyReplyRetries >= maxEmptyReplyRetries`) HOẶC last iteration → `observeEmptyOutput(state)` rồi BreakLoop như cũ.
  - `observeEmptyOutput` (mới): `reliability.Default()` nil-safe guard → `Health.ObserveFailure(provider, model, ErrModelEmptyOutput)` (tăng emptyOutputs + attempts) + `Metrics.RecordLLMEmptyOutput()`. Provider/model lấy từ `state.Provider.Name()`/`state.Model` (fallback "unknown").
  - KHÔNG đổi luồng fallback finalize (MsgEmptyReplyFallback vẫn chạy).

### C4 — Premature-completion gate
- `internal/pipeline/contguard_stage.go` (NEW): `ContinuationGate` — implement `StageWithResult` (luôn Continue; không cần IterationStage vì field này không tồn tại trong codebase — stage chạy mỗi iteration qua iteration list bình thường, đúng tinh thần contract).
  - Điều kiện ALL: gate enabled + `LastResponse.ToolCalls` empty (final answer) + content trống + không deliverable (media/forwarded/content-suffix) + iteration chưa phải last + nudge đã cạn (`EmptyReplyRetries >= maxEmptyReplyRetries`) + `TotalToolCalls == 0` → set `Observe.ContinueAfterFinal = true` + marker.
  - Config: `reliability.Default().PrematureCompletion.Enabled` — zero value = disabled (opt-in, production default). Nil-safe.
- `internal/pipeline/pipeline.go` (modified): register `NewContinuationGate(d)` giữa ThinkStage và ToolStage trong `NewDefaultPipeline`.
- `internal/pipeline/substates.go` (modified): thêm `ObserveState.ContinuationGateFired bool` — marker per-run để gate KHÔNG fire lần 2 (pipeline clear ContinueAfterFinal sau khi consume, nên không thể dùng chính nó làm guard). One continuation per run, run luôn kết thúc hữu hạn.

## Design decisions đáng lưu ý
1. **Gate gate ~ nudge cạn**: gate chỉ fire SAU khi nudge empty đã cạn (nếu không, gate sẽ chèn iteration của nó cạnh nudge → 3-retry khôn lường). Timeline thực nghiệm (MaxIter=5): nudge iter0 → nudge iter1 → gate fire iter2 → exit iter3. Không cascade — verified bởi `TestContinuationGate_SecondIterationGatedOnce`.
2. **Marker per-run**: `ContinueAfterFinal` bị pipeline clear mỗi iteration (pipeline.go:93-98), nên dùng nó làm re-fire guard không đủ — cần marker riêng. Field thuộc pipeline package (ObserveState) — không đụng internal/agent/*.

## Tests (C8-F) — `internal/pipeline/contguard_stage_test.go` (NEW)
16 tests, tất cả PASS trong Docker (`golang:1.26.0-alpine`, mount theo memory pattern):
- Gate fires: 0 tool iterations + no deliverable ✓
- Không fire: disabled default ✓, content đủ ✓, media/forwarded/suffix ✓ (3 subtests), tool iteration ✓, last iteration ✓, đã gate 1 lần ✓, nudge còn budget ✓, tool-iteration response ✓
- ContinueAfterFinal path full pipeline (pattern pipeline_test.go:197-249) ✓
- Không cascade đến MaxIterations ✓
- Empty-output wire: counter delta (metrics LLMEmptyOutputs +1, health EmptyOutputCount +1, Attempts +1 cho "stub:stub-model") ✓; nudge-path không observe ✓; media-only không observe ✓; last-iteration observe ✓; nil-runtime safety ✓

Verified thêm: `go test ./internal/pipeline/` (full suite) ok, `go vet ./internal/pipeline/` ok, `go build ./...` ok — trên bản copy có stub C7 (xem Concerns).

## KHÔNG đụng (đúng contract)
`internal/agent/*`, `internal/reliability/errors.go|circuitbreaker.go|ratelimit.go|toolloop.go`, `internal/store/*`. Chưa commit (đúng chỉ thị).

## Concerns / Cross-agent dependencies
1. **C7 dependency chưa commit**: code F gọi 3 symbols chưa tồn tại trong tree (G sẽ thêm): `Metrics.RecordLLMEmptyOutput()`, `Snapshot.LLMEmptyOutputs` (test assert), `Runtime.PrematureCompletion` (field type `PrematureCompletionOptions{Enabled bool}` — tôi đặt tên theo pattern `Stream StreamOptions`; G nên khớp đúng tên này hoặc controller điều chỉnh 1 dòng). Tôi verify bằng stubs trong bản copy riêng `%TEMP%\goclaw-f-verify` (đã xóa) — KHÔNG chạm tree.
2. Full-repo build chung phải chờ G commit metrics/config + E commit agent files (`loop.go`, `loop_types.go` đang modified bởi E).

Status: DONE
Summary: C3 empty-output wire (nudge-cạn/last-iteration → ErrModelEmptyOutput + metrics/health, nil-safe, giữ nguyên behavior) + C4 ContinuationGate stage (opt-in, one-shot per run, đăng ký sau ThinkStage) + 16 tests pass, full pipeline suite + vet + build ok.
Concerns: C7 symbols (RecordLLMEmptyOutput, Snapshot.LLMEmptyOutputs, Runtime.PrematureCompletion) chờ G commit; tên field `PrematureCompletion PrematureCompletionOptions` là quy ước của tôi — cần khớp với G hoặc sửa 1 dòng ở controller.