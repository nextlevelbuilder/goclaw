# Gap Analysis Report — GoClaw_Upgrade_Improvement_Plan.md

**Date:** 2026-08-16 03:20 (Asia/Saigon)
**Branch:** `feat/reliability-upgrade`
**Method:** 4× read-only scout agents (internal/reliability, agents/pipeline, providers, gateway/tools/store) vs 38-section plan. No code was written.
**Verdict:** Plan is ~40% already-implemented, ~25% "built but dead" (unwired), ~35% genuine missing. The single largest real gap cluster is **durable agent run** (§7) + its protocol/API/CLI surface (§8/§16/§18/§28/§29/§30 + checkpoint §17).

---

## Executive Summary

Bản `GoClaw_Upgrade_Improvement_Plan.md` (38 sections) nhắm tới một hệ thống **production-grade agent runtime**. Sau khi đối chiếu 4 vùng code bằng scout agents, thực trạng chia làm 3 nhóm:

| Nhóm | Tỷ lệ | Đại diện |
|------|-------|----------|
| **Đã implement & wired** | ~40% | RetryDo 2-layer thật (transport), ModelFallbackProvider, ChatGPTOAuthRouter, loop detector, compaction, context-overflow retry, cancellation, heartbeat app-level, tracing |
| **Built nhưng dead (unwired)** | ~25% | Toàn bộ `internal/reliability` (error taxonomy CF-§3, circuit breaker §4.5, health registry §5, rate-limit coordinator §4.3-4.4, metrics §23) — **zero production import** |
| **Thiếu thật (genuine gap)** | ~35% | Durable run state machine §7, WS resync cursor + Seq populating §8.2-8.3, stream watchdog §9, checkpoint/resume §17, agent_runs/checkpoints/attempts tables §18, /runs API §28, CLI debug §29, config blocks §30, per-tool deadline/retry/progress §13.2-13.3, run-level deadlines §16, run deadlines/cancellation §16 |

## Điểm chết quan trọng nhất

`internal/reliability/` (bao gồm `errors.go` 33 error codes, `circuitbreaker.go`, `health.go`, `ratelimit.go`, `metrics.go`) là một **foundation hoàn chỉnh, có 30/30 unit tests pass, nhưng không một file production nào import nó**. Grep toàn repo: chỉ package itself + tests + 1 comment (`openai_gemini.go:21`). Đây là kết quả phase-03 của plan cũ (`phase-03-reliability-layer.md:105`) — ghi rõ "Not wired to providers is risk... phase sau wiring".

Hệ quả: gate 2 phân hệ error classification song song:
- **Wired:** `internal/providers` `DefaultClassifier`/`FailoverClassification` → `FailoverReason` (9 reasons) — dùng bởi failover/model_fallback/pipeline.
- **Dead:** `internal/reliability/errors.go` `ErrorCode` taxonomy — canonical nhưng không ai đọc.

Wiring `internal/reliability` vào live surface là **công việc P0 đầu tiên phải làm** (cả §37 P0 lẫn phase-03 cũ đều nói vậy), nhưng đây không phải viết mới — nó là hook nối 2 mặt đã tồn tại.

---

## Per-Section Status Matrix (38 sections)

Legend: ✅ Implemented & wired · 🟡 Partial (đã có nền, thiếu phần) · 🔴 Genuine gap · ⚰️ Built-but-dead

| § | Tên | Status | Cơ sở (file:line từ scout) |
|---|-----|--------|---------------------------|
| §1-2 | Baseline + chẩn đoán lỗi | ✅ | Đã đúng hiện trạng (5 nguyên nhân 429) |
| §3 | Error taxonomy | ⚰️→🟡 | `internal/reliability/errors.go:235,278` — canonical đúng nhưng **dead**; production dùng `FailoverReason` (error_classify.go:41-134). Thiếu `ErrRunStateLost/ErrRunConflict`, `ReliabilityError` thiếu `sessionId` |
| §4.1 | Retry engine 2 tầng | 🟡 | Layer-1 `RetryDo` (retry.go:43-50,107) hoàn chỉnh. **Layer-2 (agent continuation/resume) không tồn tại** |
| §4.2 | Backoff thông minh | ✅ | `computeDelay` (retry.go:155-181) exp backoff ±10% jitter |
| §4.3 | Rate-limit aware retry | 🟡 | Retry-After parsed (retry.go:184-203) wins over backoff, nhưng **không shared single-flight** → retry storm vẫn có thể xảy ra. `reliability/ratelimit.go` dead |
| §4.4 | Single-flight/coalescing | ⚰️ | `RateLimitCoordinator` built nhưng unwired |
| §4.5 | Circuit breaker | ⚰️ | `reliability/circuitbreaker.go:120` built + tested nhưng unwired |
| §5 | Health registry | ⚰️ | `reliability/health.go` built nhưng unwired. Chỉ signal runtime-ish là Codex `RouteEligibility` (codex.go:105-110) |
| §6 | Intelligent fallback | 🟡 | 3 lớp wired: `ModelFallbackProvider` (model_fallback.go:31,133) per-agent, `ChatGPTOAuthRouter` (:117-152), context-overflow. Thiếu: health/cost/latency routing. `RunWithFailover` (failover.go:67) orphaned production-ready |
| §7 | **Durable run state machine** | 🔴 | Run = in-memory goroutine lifetime. `ActiveRun.State` atomic 0/1/2 (router.go:261) — không durable enum. No run-level deadline/heartbeat. Restart không recover |
| §8.1 | Tách UI error khỏi run error | 🟡 | Runs survive disconnect via `WithoutCancel` (chat.go:339). Nhưng UI "Something went wrong" logic nằm ở frontend, chưa audit |
| §8.2-8.3 | Reconnect/resync + event seq | 🔴 | `EventFrame.Seq` field tồn tại (frames.go:50-62) nhưng **`NewEvent` không bao giờ populate** (frames.go:88-94). No cursor, no `/runs/:id/events?after=`, no event replay. Stream drop: `client.go:183-188` log+discard, no gap signal |
| §9.1 | Stream watchdog | 🔴 | Không có watchdog/idle timer/stall detection anywhere. `CtxBody` (sse_reader.go:105-139) = ctx-cancel watchdog, NOT stall detector |
| §9.2 | Adaptive timeouts | 🔴 | Single fixed profile `NewDefaultTransport` (defaults.go:62-101): ResponseHeader 300s, no per-model, no stream idle/total. `providers.request_timeout_sec` chỉ bound verify/models-list |
| §9.3 | Partial stream recovery | 🔴 | No partial recovery. Stream read error→return immediately (anthropic_stream.go:152, openai_chat.go:194, codex.go:214). Fallback blocked after output (model_fallback.go:101-103,126-129). `SSEScanner` no max-line-size |
| §9.4 | Dedup | 🟡 | `EventSessionCompleted` SourceID dedup 5-min (bus_impl.go:127-156). Nhưng no idempotency at run/message level |
| §10.1 | Tool-call repair | 🔴 | Không có. `normalizeToolCall` (loop.go:63-92) chỉ rewrite MCP, không repair JSON |
| §10.2 | Invalid JSON repair | 🔴 | Không có |
| §10.3 | Empty output recovery | 🟡 | `maxEmptyReplyRetries=2` + context-overflow/empty-length retry (think_stage.go:20,133-145) |
| §10.4 | Premature completion detector | 🔴→🟡 | ThinkStage break khi model không trả tool call (think_stage.go:185-203) = rủi ro. Đã có truncation retry 3x, budget nudge 70%/90% |
| §10.5 | Loop detector | ✅ | `toolloop.go:12-33` warn3/crit5 + readOnly 8/12 + sameResult 4/6 → `loopKilled` (loop_tools.go:28-196) |
| §11 | Completion verifier | 🔴 | Không có verification levels (0-4) |
| §12 | Context nâng cấp | 🟡→✅ | Compaction đầy đủ: mid-loop + post-run summarize, memory flush, summary reuse (docs 01 §7), failure-context compaction (think_stage.go:261-285) |
| §13.1 | Tool retry classification | 🔴 | Không có per-tool retry. `Tool` interface chỉ Name/Description/Parameters/Execute (tools/types.go:14) |
| §13.2 | Tool deadline | 🔴 | No per-tool timeout/deadline. Chỉ global 300s transport + maxSequentialWaitBatchMs=300000 (tool_stage.go) |
| §13.3 | Tool progress events | 🔴 | Không có. Có AgentEventToolCall/ToolResult nhưng không progress ticks |
| §14 | Concurrency/backpressure | 🟡→🔴 | Scheduler lanes (Main 30/Sub 50/Team 100/Cron 30) + per-session serialization, nhưng **no provider/key/model/tenant concurrency limit** |
| §15 | Fair queueing | 🟡 | `SessionQueue` FIFO/followup/interrupt modes exist (queue.go:17-26), nhưng không là fair scheduling giữa tenants |
| §16 | Run deadlines & cancellation | 🔴 | Cancellation tốt (2-phase abort router.go:336-375, CancelOne/CancelAll/Reset scheduler.go:392-471). **No run-level deadline** (no WithTimeout) |
| §17 | Checkpoint & resume | 🔴 | `CheckpointStage` (checkpoint_stage.go:24-54) chỉ flush messages every 5 iterations, không phải run position. `RecoverInterruptedRuns` mark-failed, không resume (cmd/gateway_managed.go:214-220) |
| §18 | Event log / run journal | 🟡 | `run_timeline_items` + `RunTimelineStore` + `run.timeline.get` + `RecoverInterruptedRuns` exist. **No agent_runs/agent_run_events/agent_run_checkpoints/agent_run_attempts tables**. `AgentEvent` no seq, not persisted directly |
| §19 | Better user-facing status | 🟡 | `chat.status` isRunning/runId/activity{phase,tool,iteration} (chat.go:99-117). Thiếu run.recovering/llm.delta/tool.progress/run.checkpoint events (chính §7 ý 4: progress != token output) |
| §20 | Capability profiles | 🟡 | Static `ProviderCapabilities` (capabilities.go:7-17), `ModelSpec` (model_registry.go:6-15), ReasoningCapability. **Không được consume** cho timeout/retry/fallback decisions |
| §21 | Reasoning model support | 🟡 | Reasoning effort resolve + thinking tokens + 300s header. **No adaptive/per-model timeout, no stream-idle/total** |
| §22 | LLM attempt record | 🟡 | `CallUsage` (call_usage.go:8-15), LLM spans, ModelFallbackAttemptMetadata. **No attempt_id/first_byte_at/http_status/retry_number, no agent_run_attempts table** |
| §23 | Observability | 🟡 | Traces/spans/collector 5s flush + retry 10x, 2-phase spans. Metrics của reliability dead (unwired). Tracing stale-recovery **disabled** (collector.go:168-181) — hung run không detect |
| §24 | Error budget / SLO | 🔴 | Usage caps exist nhưng không phải SLO engine |
| §25-26 | Test matrix / chaos | 🔴 | Integration + invariant + contracts tests exist (make test-*), nhưng không chaos/load framework |
| §27 | Regression tests 429/stream/false-error | 🟡 | Integration suite yes; chưa có case D/E (premature completion, long reasoning) |
| §28 | /runs API | 🔴 | No HTTP GET/POST `/runs`. Chỉ RPC `run.timeline.get` (methods.go:44) |
| §29 | CLI debug commands | 🔴 | Cobra tree (cmd/root.go:39-59): onboard/agent/traces/... **no `run`, no `providers health/cooldowns`** |
| §30 | Config blocks | 🔴 | Config root (config.go:45-66): **no `runtime`/`reliability`/`agent` blocks**. Chỉ MaxToolIterations/MaxToolCalls + cron retry knobs |
| §31-34 | Architecture/commit/DoD | 🟡 | Đã dùng làm sườn cho phase-03 cũ |
| §35-36 | Target architecture + nguyên tắc | ✅ | Đã đúng hướng design (gate 3-tier foundation) |
| §37 | P0-P2 priority | ⚪ | Dùng làm roadmap (xem Recommended Sequencing bên dưới) |
| §38 | Kỳ vọng | ✅ | Mục tiêu giữ nguyên |

---

## P0 Cluster — "Durable Agent Run" (highest priority, §37 chỉ định)

Đây là cụm thật sự còn thiếu và ảnh hưởng lớn nhất. Được 3/4 scout agents xác nhận độc lập.

### Thiếu gì (đối chiếu trực tiếp):

1. **Run-state enum durable.** Hiện chỉ `ActiveRun.State` atomic 0/1/2 trong memory (router.go:261). Plan §7.1 muốn state machine có recoverable states (queued → running → checkpointed → completed/failed).
2. **agent_runs table.** Không tồn tại. `run_timeline_items` chỉ là append-only log display-safe (migration 000074, UNIQUE(tenant_id,run_id,seq)), không phải run record.
3. **Run-level heartbeat + stale detection.** Không có run deadline; tracing stale-recovery bị disabled vì thiếu `last_span_at` column (collector.go:168-181). Hung run trôi vô thời hạn.
4. **Checkpoint/resume.** `CheckpointStage` chỉ flush conversation messages (message checkpoint, không run position). `RecoverInterruptedRuns` mark-failed không resume. Plan §17 muốn resume sau restart.
5. **Protocol/API surface.** `EventFrame.Seq` never populated (frames.go:88-94); no `/runs/:id`, no `/runs/:id/events?after=cursor`; no event replay. Segment này gắn với §8.2/§18/§28/§29.
6. **Progress != token output** (§7.4). `chat.status` activity có phase/tool/iteration, nhưng không phải checkpoint progress; thiếu run.recovering/llm.delta/tool.progress events.

### Khuyến nghị sequencing (bám §32 Phase 3 + §37 P0):

- **Nền:** migration `agent_runs` + `agent_run_attempts` + (optional) `agent_run_events` (có thể tái dùng `run_timeline_items` làm event source giai đoạn đầu).
- **Runtime:** run-state enum + heartbeat tick + stale-run recovery (fix tracing stale sweep cần `last_span_at`).
- **Protocol:** populate `EventFrame.Seq` (per-run sequence) + `GET /runs/:id` + `/runs/:id/events?after=` (replay/cursor).
- **CLI:** `goclaw run list|get|events|recover`.
- **Config:** `reliability.runs.*` block (heartbeat interval, extension budget, max-runs, sweep window).

⚠️ Surface parity (per CLAUDE.md): durable run buộc phải đổi DB (migrations dual: PG `migrations/` + SQLite `schema.sql`/`schema.go`) + protocol + web UI + CLI đồng bộ. Không phải backend-only change.

---

## Priority 2 — Wiring `internal/reliability` (rẻ nhất, hiệu quả cao nhất)

`internal/reliability` đã built+tested nhưng dead. Wiring rẻ (chạm file providers/pipeline đã có), đem lại ngay:
- Circuit breaker trên từng provider:model (thay chỗ default 3-attempt luôn thử).
- Health registry → feed fallback routing (§5 → §6).
- Rate-limit coordinator single-flight → chặn retry storm 429 (giải concern retry.go shared state).
- Metrics counters → observability §23 thật.

**Quyết định cần user:** với `RunWithFailover` (failover.go:67, orphaned production-ready 2 tầng) — wire nó hoặc xóa nó (tránh dead code tích lũy). Đây là decision point cho user.

---

## Priority 3 — Stream watchdog (§9) + per-tool reliability (§13)

- **§9.1 watchdog:** idle-timeout per stream (giải §0 "agent đứng im khi stream silent"), SSEScanner max-line-size guard chống unbounded memory.
- **§9.3 partial recovery:** nối với §7 checkpoint — nếu provider đã emit chunks rồi mới fail, tiếp tục từ checkpoint thay vì bỏ toàn bộ.
- **§13:** per-tool deadline + retry classification (retry/repair-then-retry/never) + progress events.

---

## Priorities Thấp hơn

- §10 weak-model repair (P1): JSON repair, tool-call repair, premature-completion completion verifier — nền là `toolloop.go` loop detector + `think_stage.go` retries đã có, thêm repair layers.
- §22 attempts record: gắn với `agent_run_attempts` table (resume của §7).
- §24 SLO, §25-26 chaos/load: P2, cần framework mới — đừng làm trước khi §7 wiring xong.
- §14/§15 concurrency/fair queue: scheduler đã có nền tốt, nâng cấp là additive.

---

## Open Questions cho User

1. **Scope:** Với quyết định "Khảo sát + đối chiếu trước", sau report này bạn muốn:
   - (A) Viết execution plan P0 (durable run §7) — plan-only, chưa code;
   - (B) Implement ngay wiring `internal/reliability` (rẻ, độc lập, không cần migration);
   - (C) Implement cả P0 cluster durable run (migrations + runtime + protocol + CLI);
   - (D) Chốt trước một P0 nhỏ để học cadence, làm tiếp sau.
2. **`RunWithFailover` (failover.go:67):** wire nó (2 tầng profile+model cho agent loop) hay xóa/extract? Plan §6 không bắt buộc nó, nhưng nó là code production-ready duy nhất còn orphan.
3. **`run.timeline.get` vs `agent_run_events`:** có muốn dùng `run_timeline_items` làm event source (ít migration hơn) hay tạo bảng `agent_run_events` riêng (đúng plan §18)?
4. **reliability metrics → OTel:** mong muốn expose qua existing tracing/OTel (build-tag) hay giữ atomic counters local?

---

## Method note

Report này tổng hợp 4 scout agents (toàn bộ read-only, không build/test/không sửa code):
- Scout A: `internal/reliability` (no changes to repo)
- Scout providers: `internal/providers`, `providerresolve`, streaming (a02655d604e4c31a2)
- Scout agent pipeline: `internal/agent`, `internal/pipeline`, `internal/scheduler`, `internal/sessions`, `internal/bus`+`eventbus`, `internal/tracing`, gateway/WS, run_timeline_items (af91b443b244c4e6c)
- Scout gateway: `pkg/protocol`, `internal/gateway`, `internal/tools`, store interfaces, `migrations/`, `internal/config`, `cmd/root.go` (a88856444e4b0bd49)

Claims keystrength: file:line dari scout reports; plan sections dikaitkan ke §37 P0/P1/P2 priorities.