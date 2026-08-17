# Phase 08 — Context Optimization: Overhead Budget Accuracy + Fresh Tool-Result Cap + Long-Session Compaction Cap + Failure Context Lens

> **Status: IN PROGRESS** — 2026-08-17. Branch: TBD (tạo từ `dev`). Workstream: agents song song, file-ownership disjoint, **agents KHÔNG commit** — controller commit tuần tự + build/test chung.
> **Scope tickets:** plan §32 Phase 8 (per-section token budgets, tool-output compaction, failure context compression, long-session stabilization). Điều chỉnh sau scout 2026-08-17: đa phần đã có sẵn (final request guard, pruning, compaction) — Phase 8 bổ sung 3 gap thật + 1 lens xác nhận. **KHÔNG xây lại cái đã có.**

## Context (scout 2026-08-17, verified trực tiếp code — controller, sau agent scout fail vì Cloudflare 530)

**ĐÃ CÓ (KHÔNG xây lại):**
- **Per-section budget engine:** `internal/pipeline/final_request_guard.go` — `FinalRequestEstimate{MessageTokens, ToolTokens, InputTokens, OutputReserve, HardInputCap, CompactTarget}` (`:17-26`), `countBudgetInput` tách messages/tools (`:83-98`), reduction ladder `prune_history→compact_history→shrink_memory` (`:161-181`), `MaxRequestShare` default 0.85 (`:28-35`). `internal/agent/loop_request_budget.go` — `guardCompleteModelRequest` hard invariant input+output ≤ window (`:49-99`), `RequestBudgetExceededError` (`:15-36`) re-entered by ThinkStage. `internal/tokencount/budget_counter.go` — `BudgetCounter` fixed `goclaw_budget_cl100k`, model-independent (`:32-46`).
- **Tool-output compaction (old results):** `internal/agent/pruning.go` — `pruneContextMessages` soft-trim head/tail + hard-clear placeholder (`:175-370`), media guard `mediaToolNames` never hard-cleared (`:87-92`, `:346-351`), `findAssistantCutoff` keepLastAssistants (`:375-390`), `hasImportantTail` 70/30 split (`:398-403`). Config: `internal/config/config.go:516-538` `ContextPruningConfig{Mode, TTL, KeepLastAssistants, SoftTrimRatio, HardClearRatio, MinPrunableToolChars, SoftTrim, HardClear}`. Wired: `internal/agent/loop_pipeline_callbacks.go:682-688` `makePruneMessages` → PruneStage.
- **Compaction & memory flush:** `internal/agent/loop_compact.go` `compactMessagesInPlace` (`:60`), max chunks 16 / merge levels 3 (`:44-45`). `CompactionConfig` `internal/config/config.go:491-498` (ReserveTokensFloor, MaxHistoryShare, MaxRequestShare, KeepLastMessages, TimeoutSeconds, MemoryFlush). `internal/agent/memoryflush.go:105-107` compaction-count-gated flush. `internal/pipeline/prune_stage.go` Phase2 flush→compact→AbortRun nếu vẫn over (`:182-220`). `state.Compact.CompactionCount` (`substates.go:110`).
- **Failure context:** `internal/agent/json_repair.go` `compactError` bounded 160 chars (`:368-370`), `shrinkMemoryForFinalRequestBudget` (`final_request_guard.go:250-270`), emergency compaction (`think_stage.go:270-294`).
- **Long-session tracking:** `sessions.CompactionCount` persisted (PG + SQLite + Manager) — `internal/store/pg/sessions_tokens.go:21-25`, `internal/store/sqlitestore/sessions_tokens.go:17-23`, `internal/sessions/manager.go:181-184`; `SessionMetaKeyLastCompactionAt` (`loop_pipeline_callbacks.go:708+`); episodic worker idempotency key `sessionKey:compactionCount` (`consolidation/episodic_worker.go:66`).

**GAPS (Phase 8 bổ sung):**
- **Gap A — overhead budget thiếu memory+reminder tokens:** `internal/pipeline/context_stage.go:147-152` tính `OverheadTokens` = system + tool schemas, NHƯNG `:172-181` append `MemorySection` vào system SAU đó (`sys.Content += "\n\n" + section`), và `:163-166` `InjectReminders` inject vào history SAU overhead count. → Budget không tính memory L0 + reminders → prune/compact có thể trễ so với thực tế. Fix: re-count overhead sau các mutation, hoặc count chúng như phần fixed.
- **Gap B — fresh (current-turn) tool result chưa có cap:** `pruneContextMessages` chỉ trim results cũ (trong `cutoffIndex`), fresh results trong `pending` được bảo vệ hoàn toàn (`fixedMessages` trong `pruneForFinalRequestBudget` `final_request_guard.go:209-211` + `prepareFinalRequest` không touch pending). 1 tool `read_file`/`web_search`/`exec` trả về 50-100K tokens trong turn chạy nếu CHƯA vượt budget-hiện-tại sẽ phá `CompactTargetTokens` vô cắt → emergency compaction hoặc abort. Fix: per-result cap trên fresh tool results (tái dùng head/tail trim logic), áp ở tool result injection path (pending append) hoặc final request guard.
- **Gap C — chưa có max compaction cap cho session:** `GetCompactionCount` ≥ 0, `state.Compact.CompactionCount` tăng không giới hạn, `KeepLastMessages` giữ 4, nhưng không có threshold dừng. Session rất dài → compact vô hạn → mất dần thông tin gốc, context degradation không kiểm soát, chi phí LLM compact tăng. Fix: config `MaxCompactionsPerSession` (default, ví dụ 12) → khi vượt: báo qua nudge/block reply + không thêm compact, dùng memory flush + token cap triệt để hơn.
- **Gap D — failure-context lens chưa có telemetry/test:** `compactError` + `shrinkMemory` + emergency compaction tồn tại nhưng chưa có test chứng minh path "repeat failure không phá budget" và chưa có telemetry khi `RequestBudgetExceededError` xảy ra (chỉ slog). Fix: telemetry event `context.budget_exceeded` + characterization tests cho failure-compression path.
- **Gap E (scout agent bổ sung) — L0 memory cap dead code:** `MaxL0Tokens=200`/`MaxL0Items=5` config tồn tại (`retrieval/retriever.go:13-25`) nhưng **không được enforce** — `auto_injector_impl.go:27-103` chỉ cap theo `MaxEntries`, inject thẳng vào system content. → L0 memory section có thể phình vô hạn, đúng gap "overhead thiếu memory". Fix: enforce L0 token/item cap khi auto-inject (tóm kết nếu vượt), đưa vào Module A.
- **Gap F (scout agent bổ sung) — IterationProgress dead:** `tools/context_keys.go:859` `WithIterationProgress` không có call site → adaptive web_fetch caps (20K/10K giảm dần theo iteration, `web_fetch.go:222-231`) **chết trong v3 path**. → tool-output compaction thực tế là static char cap, không adaptive. Fix: set `IterationProgress` per-iteration trong `makeCallLLM`/`loop_pipeline_callbacks.go`, làm adaptive caps hoạt động, đưa vào Module B.

**Contracts (C1-C6):**
- **C1** Overhead budget đếm đúng = system + tool schemas + memory section + reminders (sau mọi mutation). L0 auto-inject tôn trọng `MaxL0Tokens`/`MaxL0Items` (enforce thay vì dead). Zero-config = hành vi hiện tại.
- **C2** Fresh tool-result cap: per-result max tokens cho pending tool results; vượt → head/tail trim (giữ structure head 30% + important tail 70%, theo `hasImportantTail`); media results giữ nguyên (bảo toàn vision/audio mô tả). `IterationProgress` được wire per-iteration để adaptive caps (web_fetch 20K/10K) hoạt động trở lại.
- **C3** Long-session: `MaxCompactionsPerSession` (default 12) dừng compaction lặp; vượt → nudge user (mới session / nén thủ công) thay vì cấm chạy.
- **C4** Failure context compression: path repeat-failure đã có (compactError, shrink_memory) — thêm telemetry `context.budget_exceeded` + tests chứng minh không phá budget.
- **C5** Không đổi wire shape (WS frames, API structs, session JSONB schema). Config mới dùng JSON5 defaults, override theo repo pattern.
- **C6** Dual-DB: mọi thay đổi tracking session phải hợp PG + SQLite + Manager, hoặc tránh lưu DB nếu có thể (nudge-only runtime state).

## Files

**Module A — Overhead budget accuracy + L0 cap (Gap A + E)** — file: `internal/pipeline/context_stage.go`, `internal/memory/auto_injector_impl.go` (hoặc nơi AutoInject xây L0 section), `internal/memory/retriever.go` (nếu cần), `internal/pipeline/context_stage_overhead_test.go` (mới, test), `internal/pipeline/context_stage_integration_test.go` (sửa nếu cần), `internal/memory/auto_injector_test.go` (mới)
- Re-order/sửa ContextStage: tính `OverheadTokens` SAU InjectContext/EnrichMedia/InjectReminders/AutoInject đều xong. Bao gồm memory section tokens trong overhead (không phải trong history budget).
- Khi `AutoInject`/`InjectReminders` mutation thêm content vào system/history, `OverheadTokens`/fixed-section count phản ảnh. Đảm bảo `PruneStage` budget chính xác.
- Enforce L0 cap: trong auto-inject, nếu `MaxL0Tokens`/`MaxL0Items` được wire (RetrievalConfig → makeAutoInjectCallback), tóm kết section theo cap thay vì inject thẳng. `RetrievalConfig` mới được wire (`loop_pipeline_adapter.go:327-344`).
- Test: fixedMessages (system+memory+reminders) tokens đều nằm trong OverheadTokens; PruneStage budget giảm đúng; L0 vượt cap → section bị tóm kết/giới hạn.

**Module B — Fresh tool-result cap + IterationProgress (Gap B + F)** — file: `internal/agent/pruning.go` (thêm `capFreshToolResult` helper tái dùng trim + `resolvePruningSettings` đọc `FreshResultCapTokens`), `internal/pipeline/final_request_guard.go` (áp vào `prepareFinalRequest`/`buildChatRequest` cho pending tool results), `internal/pipeline/final_request_guard_test.go` (mới), `internal/agent/loop_pipeline_callbacks.go` (wire IterationProgress trong makeCallLLM). KHÔNG sửa `internal/config/config.go` — field `FreshResultCapTokens` do Module C thêm, Module B chỉ đọc.
- Helper `capFreshToolResult(content string, cfg, isMedia) string` — đã có head/tail/important-tail logic trong pruning.go:292-296 — tái dùng.
- Áp: trong `prepareFinalRequest` (hoặc `buildChatRequest`), với mỗi pending tool message, nếu content > cfg cap → trim. Chỉ trim PENDING (fresh) results, không trim history (history đã do pruneContextMessages).
- Wire `IterationProgress` per-iteration: trong `makeCallLLM`/adapter, gọi `tools.WithIterationProgress(ctx, state.Iteration)` (context setter đã có `tools/context_keys.go:859`) → làm adaptive web_fetch caps hoạt động trở lại.
- Config: thêm `ContextPruningConfig.FreshResultCapTokens int` (default 12000? — chọn theo mức an toàn; tail quan trọng giữ 70%) — kèm đọc ở resolvePruningSettings.
- Test: 1 tool result 50K tokens trong pending → sau cap ≤ threshold + structure head/tail; media không bị trim; zero-config không đổi behavior; IterationProgress truyền vào ctx qua các iteration.

**Module C — Long-session compaction cap + config fields (Gap C)** — file: **`internal/config/config.go` (chủ quyền: thêm CẢ 2 fields mới `CompactionConfig.MaxCompactionsPerSession` + `ContextPruningConfig.FreshResultCapTokens` — ngăn 2 agent edit cùng file)**, `internal/pipeline/prune_stage.go` (gate compaction, đọc `s.deps.Config.Compaction.MaxCompactionsPerSession` qua deps.Config), `internal/config/config_test.go`, `internal/pipeline/prune_stage_test.go` / `compaction_pressure_e2e_test.go`. KHÔNG chạm callbacks/adapter — PruneStage đã có deps.Config.Compaction wire từ adapter.
- Config: `CompactionConfig.MaxCompactionsPerSession int` (default 12, 0 = unlimited legacy). Đây là SSoT cho Module B (đọc FreshResultCapTokens).
- Gate: trong `PruneStage`/`ThinkStage` khi `state.Compact.CompactionCount >= max` → skip LLM compaction, dùng memory flush + (nếu vẫn over) emit nudge (content: "Session rất dài — hãy bắt đầu session mới hoặc yêu cầu tóm tắt") thay vì AbortRun im lặng.
- Track runtime `state.Compact` đã có `CompactionCount`; nếu cần nudge persistent → chỉ runtime, không DB.
- Test: đạt cap → không compact thêm, nudge được emit; dưới cap → behavior cũ.

**Module D — Failure-context lens (Gap D)** — file: `internal/pipeline/think_stage.go` (telemetry emit tại nơi `prepareFinalRequest` abort — KHÔNG chạm `final_request_guard.go` vì Module B sở hữu), `internal/pipeline/think_stage_telemetry_test.go` (mới), `internal/agent/json_repair.go` (đọc-only nếu cần characterization), `internal/agent/json_repair_test.go` (mở rộng characterization), `internal/pipeline/think_stage_budget_test.go` (mở rộng nếu cần)
- Telemetry: khi `prepareFinalRequest` abort vì `budget exceeded` → emit event `context.budget_exceeded` (eventbus DomainEvent hoặc EventBus publish + slog) với estimate fields (message/tool/input/output/cap/target/window). Đặt emit trong call site `think_stage.go` (nơi bắt lỗi từ guard + nơi `reduceForBudgetExceeded` re-enter).
- Tests: characterization cho `compactError` (bounded), `shrinkMemoryForFinalRequestBudget` (repeat failure → budget giữ ổn), emergency compaction trigger.
- KHÔNG xây lại compression logic — chỉ xác nhận + telemetry + tests.

## Implementation steps

1. Module A: sửa ContextStage re-order + overhead count bao gồm memory/reminders. Tests.
2. Module B: helper capFreshToolResult + wire vào final request guard. Config field. Tests.
3. Module C: config field + gate compaction cap + nudge. Tests.
4. Module D: telemetry event + characterization tests.
5. Controller: build/test Docker (PG + sqliteonly), vet, tests tools+agent+pipeline. TS typecheck nếu UI chạm (Module C nudge có thể chạm UI nếu render, nhưng giữ minimal — nudge qua block.reply content, không i18n mới nếu tránh được).
6. Commit tuần tự, push branch, PR, CI, merge dev, tick plan.

## Validation / acceptance criteria

- `go build ./...`, `go build -tags sqliteonly ./...`, `go vet ./...` pass.
- `go test ./internal/pipeline/ ./internal/agent/ ./internal/tokencount/ ./internal/config/` pass.
- C1: ContextStage OverheadTokens bao gồm memory+reminders — test mới pass.
- C2: fresh tool result > cap được trim — test pass; media giữ nguyên.
- C3: compaction cap hoạt động + nudge — test pass; dưới cap behavior cũ (regression tests pass).
- C4: telemetry budget-exceeded emit — test pass; zero-config không đổi behavior.
- Dual-DB: không thay đổi schema JSONB/session table (chỉ thêm field config runtime + nudge).

## Risks & rollback

- **Gap A re-order ContextStage** — động chạm overhead tính toàn pipeline; nếu sai có thể khiến prune compact sớm hơn (an toàn, chỉ tốn 1 lần compact) hoặc trễ (nguy hiểm). Rollback: revert ContextStage.
- **Gap B trim fresh result** — rủi ro: cắt nhầm thông tin quan trọng của turn hiện tại. Giảm thiểu: chỉ trim nếu thực sự vượt cap (không trim mặc định), tail quan trọng giữ 70%, media được bảo toàn. Rollback: tắt config (cap=0).
- **Gap C compaction cap** — rủi ro: session dài bị abort thay vì compact → mất khả năng tiếp tục. Giảm thiểu: cap cao (12), vượt cap vẫn chạy nhưng nudge, không cấm. Rollback: config max=0 (unlimited).
- **Gap D telemetry** — chỉ thêm emit, rollback dễ (bỏ emit).

## Report
- `{work_context}/plans/reports/phase8-context-optimization-scout-report.md`, `...-module-*.md`