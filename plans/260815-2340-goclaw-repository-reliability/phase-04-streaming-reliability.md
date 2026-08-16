# Phase 04 — Streaming Reliability: Stream Watchdog + Adaptive Timeouts + WS Event Seq Dedup + Duplicate Suppression

> **Status: IN PROGRESS** — 2026-08-16. Scope approved by user: 4 modules, partial-stream recovery deferred.
> **Branch:** `feat/phase4-streaming` (tạo từ `dev`). Worktree: 4 agents song song, file-ownership disjoint, **agents KHÔNG commit** — controller commit tuần tự + build/test chung.

## Context

Scout (2 lượt, 2026-08-16) xác nhận:

- Mọi provider streaming dùng chung pattern `SSEScanner` + `for sse.Next()` (openai_chat.go, anthropic_stream.go, codex.go) hoặc callback (ollama.go `client.Chat(..., streamFn)`). **Không có idle timeout trên khoảng cách giữa 2 event** — chỉ `ResponseHeaderTimeout: 300s` (first-byte, defaults.go:84).
- `ObserveStreamStall` đã có + có score penalty + test (health.go:118) nhưng **unwired** — chưa nơi nào gọi.
- Agent loop blocking trong `provider.ChatStream(ctx, req, onChunk)` (loop_pipeline_callbacks.go). Run context chỉ `WithCancel` (scheduler/queue.go:267) — stream chết lặng kẹt lane vô thời hạn.
- EOF giữa chừng với partial output = **silent success** (OpenAI default `FinishReason="stop"` khi thiếu `[DONE]`, openai_chat.go:126).
- Metrics: `RecordLLMStreamStall()` + `RecordLLMTimeout()` + Sink/Flush đã tồn tại (metrics.go:61,64,134-199). Not wired production.
- `EventFrame.seq?: number` type-declared cả 2 UI (ui/web/src/api/protocol.ts:26, ui/desktop/frontend/src/lib/ws-types.ts:30) nhưng **server không gửi, client không dùng**.
- Resync đã có (`runs.events` + `afterSeq`, run_timeline.go). Dedup chỉ là DB upsert server-side (pg/run_timeline.go:45).
- `checkpoint` column reserved (run_timeline_store.go:105) — chưa ai ghi/đọc.

## Scope (đã duyệt)

| # | Module | Tóm tắt |
|---|--------|---------|
| 1 | Stream watchdog | Idle timeout per-event trên SSE loop (timer reset mỗi event); gọi `ObserveStreamStall` + `RecordLLMStreamStall`; trả `ReliabilityError`; EOF-sạch-không-marker với **0 chunk** = error (empty recovery thuộc Phase 5) |
| 2 | Adaptive timeouts | `stream.idle_timeout_ms` + `stream.first_byte_timeout_ms` vào `ReliabilityConfig`; per-model override `ModelSpec.StreamTimeoutMs` (0 = inherit global) |
| 3 | WS event seq + client dedup | Server gán per-run `event_seq` tăng dần lên event frame chunk/thinking/status; client (web + desktop) dedup theo `runId + seq` |
| 4 | Duplicate suppression | Không retry/failover sau khi đã emit chunk nào (gate `emitted` khi settle); 1 lần stall error duy nhất/attempt; prevent duplicate final output từ retry |

**Deferred (không làm phase này):** Partial-stream recovery (cần checkpoint/resume từ Phase 3 trước), adaptive timeout theo reasoning-phase riêng, per-model capability profile.

## Contracts (bắt buộc — mọi agent implement theo đúng)

### C1. Config (B)
```go
// internal/config/config.go — ReliabilityConfig thêm:
Stream StreamConfig `json:"stream"`
type StreamConfig struct {
    IdleTimeoutMs   int `json:"idle_timeout_ms"`   // default 60000 (60s) — khoảng lặng giữa 2 event
    FirstByteTimeoutMs int `json:"first_byte_timeout_ms"` // default 0 = disabled (transport 300s still backstop)
}
```
- Constants: `DefaultStreamIdleTimeoutMs = 60000`, `DefaultStreamFirstByteTimeoutMs = 0` (0 = disabled).
- `EffectiveStreamIdleTimeout() time.Duration` (≤0 → 0 = disabled), `EffectiveStreamFirstByteTimeout()`.
- **Không** phá cấu trúc cấu hình upstream: tên key JSON snake_case như `reliability.circuit.*` đã làm (W1). Config viết `reliability.stream.*`.

### C2. Reliability Runtime stream knobs (B)
```go
// internal/reliability/singleton.go — Runtime thêm:
Stream StreamOptions
type StreamOptions struct { IdleTimeout, FirstByteTimeout time.Duration } // 0 = disabled
```
- `Runtime` được `Configure(opts CircuitOptions, maxPending int, stream StreamOptions)`? — **thay đổi chữ ký**: `Configure(circuit CircuitOptions, maxPending int, stream StreamOptions)` (compile-time đủ 2 callers: cmd/gateway.go + không test). Hoặc giữ chữ ký + thêm `SetStreamOptions`. **Agent B quyết biến thể ít phá nhất, ưu tiên giữ chữ ký cũ + method mới** `func (r *Runtime) SetStream(StreamOptions)`; gọi từ `cmd/gateway.go` ngay sau `Configure`.
- `reliability.Default().Stream` nil-safe (Runtime cũ không có → RWMutex swap toàn bộ).

### C3. ModelSpec field (B)
```go
// internal/providers/model_registry.go — ModelSpec thêm:
StreamTimeoutMs int  // ms, 0 = inherit global; seed default = 0
```
- `CloneFromTemplate` + patch maps không phải đụng (chỉ thêm field, không đổi logic clone).

### C4. Watchdog helper (A)
```go
// internal/providers/reliability_wiring.go (A thêm):
func streamWatchdogContext(parent context.Context, idle, firstByte time.Duration) (context.Context, context.CancelFunc)
// - idle <= 0 → chỉ first-byte watchdog (nếu firstByte > 0)
// - cả hai <= 0 → trả parent (no-op)
// - cancel x2 lần an toàn (idempotent)
func observeStreamStall(provider, model string)  // nil-safe: Health.ObserveStreamStall + Metrics.RecordLLMStreamStall
```
- **Timer reset per-event**: helper trả ctx được watchdog bằng `time.AfterFunc`/timer, nhưng **mỗi event nhận được phải reset**. Thiết kế đề xuất: helper trả `(ctx, reset func(), cancel func())` — reset gọi mỗi lần `sse.Next()` parse thành công. Agent A quyết chi tiết, kèm test (timer fire → ctx.Done; reset trước deadline → không fire).
- Error khi stall: `reliability.New(reliability.ErrProviderTimeout, "stream idle timeout: "+key).WithRunContext(...)`; **chỉ 1 lần** ghi stall (guard once-per-request).

### C5. Provider wiring (A)
- 4 provider streams (openai_chat.go, anthropic_stream.go, codex.go, ollama.go): wrap `streamWatchdogContext(parent, cfgIdle, cfgFirstByte)`; reset mỗi event; khi stall: `observeStreamStall` + cancel + trả error `ErrProviderTimeout`.
- Đọc timeout: `reliability.Default().Stream` (B cung cấp); per-model override: `ModelSpec.StreamTimeoutMs` nếu > 0 (qua Resolve đã có trong provider).
- **Không retry sau khi đã emit chunk** (giữ ổn định hiện tại: only codex uses `emitted` guard — chuẩn hóa: mọi provider + failover không fallback nếu `onChunk` đã được gọi; gate trong failover.go khi settle).
- Ollama: streamFn callback — watchdog áp tại mức gói: idle timeout tính từ last chunk nhận.

### C6. WS event seq (C)
- Server: per-run counter tăng dần gán cho **mọi** event frame đi qua emitRun (chunk/thinking/status/tool) — đặt ở `internal/agent/loop_tracing.go` / `loop_run.go` emit path (`cfg.OnEvent`), field `EventFrame.Seq` (đã khai báo optional — BÂY GIỜ điền giá trị `> 0`).
- Persisted timeline (runs.events) giữ nguyên `seq` hiện có; **không nhập chung** — WS frame seq là độc lập, đánh số riêng theo run.
- Không thêm DB migration. Không đổi resync logic hiện có.
- Compat: client cũ bỏ qua seq → an toàn.

### C7. UI dedup (D)
- Web `ui/web/src/api/ws-client.ts` + `pages/chat/hooks/use-chat-messages.ts`; Desktop `lib/ws.ts` + `hooks/use-chat.ts`: per-run `lastSeq` map; event có `seq` && `seq <= lastSeq` → drop (dedup); `seq > lastSeq` → xử lý + cập nhật; event không có seq → xử lý như cũ (compat với server cũ).
- Key: `(runId, sessionKey)`.
- Chỉ UI — không đổi server.

## Controller Review Notes (2026-08-16)

1. **Đứt nối C→D (đã fix ở controller)**: `AgentEvent.Seq` sống trong `bus.Event.Payload`; cầu `internal/gateway/server.go` (`NewEvent`) không copy sang `EventFrame.Seq` — frame bị stamp per-connection ở `SendEvent`. Fix: `payloadSeq()` type-assert `agent.AgentEvent` (struct value) trong subscription → `frame.Seq = ev.Seq` nếu > 0. Per-connection `nextSeq` chỉ fallback cho frame không có seq (QR/health etc.).
2. **`FailoverStreamed` = dead code (giữ, vô hại)**: `RunWithFailover` chưa có caller production; protection thật đã ở `ModelFallbackProvider.noFallbackAfterStreamError` + codex `emitted` + ollama `emitted`. Sentinel giữ cho Phase 6.
3. **Config `stream.*` là opt-in (0 = disabled)** — lệch contract ban đầu (default 60s). Quyết định: giữ opt-in — an toàn cho reasoning models (o1/claude thinking không bị cắt bất ngờ khi chưa config); tài liệu ghi rõ.

### C8. Tests bắt buộc
| Agent | Test |
|-------|------|
| A | `stream_watchdog_test.go`: httptest SSE server gửi header rồi im lặng → watchdog fire trong `idle`; reset mỗi event không fire; stall ghi đúng 1 lần (metrics delta); harness chung cho openai/anthropic/codex path |
| B | `stream_config_test.go`: parse JSON5 `reliability.stream.*`, default, 0=disabled, negative→0; singleton SetStream; per-model override không phá clone |
| C | unit: per-run seq tăng dần qua emit (mock cfg.OnEvent); frame có Seq>0 |
| D | (nếu test framework có sẵn ở ui) dedup logic unit test; tối thiểu `pnpm build` pass cho web + desktop |

## Files to create/modify

| Agent | Files (disjoint) |
|-------|------------------|
| A | `internal/providers/reliability_wiring.go` (+helper), `openai_chat.go`, `anthropic_stream.go`, `codex.go`, `ollama.go`, `failover.go`; mới `stream_watchdog_test.go`; đụng `go.mod`? Không. |
| B | `internal/config/config.go`, `internal/config/circuit_config_test.go` (hoặc mới `stream_config_test.go`), `internal/reliability/singleton.go`, `internal/providers/model_registry.go`, `cmd/gateway.go` (gọi SetStream) |
| C | `internal/agent/loop_tracing.go`, `loop_run.go` (emit path), `internal/gateway/server.go` (nếu cần chuyển seq qua frame), `pkg/protocol/events.go` (nếu field thiếu — hiện đã có `Seq`?) + test |
| D | `ui/web/src/api/ws-client.ts`, `ui/web/src/pages/chat/hooks/use-chat-messages.ts`, `ui/web/src/stores/use-chat-messages-store.ts` (nếu cần), `ui/desktop/frontend/src/lib/ws.ts`, `hooks/use-chat.ts`, `stores/chat-message-store.ts` |

**⚠️ KHÔNG AI đụng:** `internal/reliability/health.go`, `metrics.go`, `ratelimit.go`, `circuitbreaker.go` (đã reviewed kỹ — chỉ consume). Confliếu giữa A/B trên `internal/providers/`: file disjoint (A: reliability_wiring.go + providers file; B: model_registry.go) — tuân thủ nghiêm.

## Implementation Steps

1. Controller viết phase file (file này) → dispatch 4 agents (A/B/C/D) song song theo contract.
2. Agents implement, tự test **phần mình teste được độc lập** (không đòi compile toàn repo — controller build chung sau khi commit đủ).
3. Controller review từng diff (verify pass: đối chiếu contract, grep callers, không tin self-validation).
4. Commit tuần tự: **B → A → C → D** (B trước A vì A đọc `Runtime.Stream` + `ModelSpec.StreamTimeoutMs`; C/D độc lập).
5. Build + vet + test toàn bộ Docker (PG build + sqliteonly build).
6. Push branch `feat/phase4-streaming` từ `dev` → PR → CI; mỗi agent theo dõi CI phần mình.
7. CI xanh → merge dev → tick Phase 4 trong main plan → final report.

## Tests / Validation

- Unit tests C8 pass.
- `go build ./...` + `go build -tags sqliteonly ./...` + `go vet ./...` + `go test ./internal/reliability/... ./internal/providers/... ./internal/config/...` in Docker (`golang:1.26.0-alpine`, mounts: `C:/Users/DORA/Downloads/goclaw-mod:/src`, `goclaw-gomodcache:/go/pkg/mod`, `goclaw-gomodcache:/root/.cache/go-build`).
- CI PR xanh (go incl. unit + invariants + integration nếu chạy).
- **Không viết** load/benchmark tests (rule repo).

## Risks / Rollback

- **Watchdog false-positive trên reasoning models**: default idle 60s — reasoning (o1/claude thinking) có khoảng lặng > 30-60s giữa events khi reasoning dài. Mitigation: idle 60s default + per-model override (`StreamTimeoutMs`) + config tắt (0). Review sẽ đánh giá ngưỡng.
- **Đổi chữ ký Configure** — tránh; ưu tiên `SetStream` additive (không đổi contract W1).
- **Seq không tăng trên mọi frame** → client dedup vô hại (bỏ qua khi seq=0). Rollback từng module độc lập (config flag tắt watchdog = 0; UI drop theo `seq` guard có sẵn).
- Public contracts: `EventFrame.Seq` đã optional — điền giá trị không phá compat client cũ. Không thêm DB field mới.