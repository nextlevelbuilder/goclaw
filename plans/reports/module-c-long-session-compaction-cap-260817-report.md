# Module C — Long-Session Compaction Cap + Config Fields — Report

**Date:** 2026-08-17
**Branch:** feat/phase8-context-optimization
**Scope:** Phase 8 Module C (Gap C) — `MaxCompactionsPerSession` cap + `FreshResultCapTokens` field (SSoT for Module B).
**Parallel-agent boundary respected:** Only owned files edited (config authority + prune_stage + tests). No changes to `context_stage.go`, `think_stage.go`, `loop_pipeline_adapter.go`, `loop_pipeline_callbacks.go`, `pruning.go`, `final_request_guard.go`, `json_repair.go`, `memory/*`, `retriever.go`.

## Summary

Implemented the long-session compaction cap (default 12, 0 = unlimited legacy) in the pipeline's PruneStage Phase 2, and added both new config fields to `internal/config/config.go` (the single write-point for parallel agents).

## Changes

### `internal/config/config.go` (authority file)
- Added `CompactionConfig.MaxCompactionsPerSession int \`json:"maxCompactionsPerSession,omitempty"\`` — caps LLM compactions per session. Default 12; 0 = unlimited legacy.
- Added `ContextPruningConfig.FreshResultCapTokens int \`json:"freshResultCapTokens,omitempty"\`` — per-result cap for fresh (current-turn) tool results; default 0 = disabled. Field-only (Module B reads it).

### `internal/config/defaults.go`
- Added `DefaultMaxCompactionsPerSession = 12` const.

### `internal/config/config_load.go`
- Seeded `Default()` → `Agents.Defaults.Compaction = &CompactionConfig{MaxCompactionsPerSession: DefaultMaxCompactionsPerSession}` so fresh installs ship the 12 cap. All existing CompactionConfig consumers use `> 0` nil/zero guards (verified: `loop_compact.go`, `loop_history_sanitize.go`, `final_request_guard.go`, `memoryflush.go`, `loop_types.go`), so the minimal pointer is harmless. Explicit `0` in a loaded config reverts to unlimited legacy.

### `internal/pipeline/prune_stage.go`
- Added the cap gate in Phase 2 (after memory flush, before `CompactMessages`):
  - Reads `s.deps.Config.Compaction.MaxCompactionsPerSession` (wired from adapter, no callback changes).
  - When `maxCompactions > 0 && state.Compact.CompactionCount >= maxCompactions`: skips LLM compaction, emits a `Transient` nudge via `AppendPending` when still over budget, returns `Continue` — never `AbortRun` purely due to the cap. `CompactionCount` is not incremented; `MemoryFlushedThisCycle` stays true.
  - When cap is 0 (or Compaction nil), the gate is skipped entirely — 100% legacy behavior.

### Tests
- `internal/config/config_test.go` (new): default cap = 12; zero-value = unlimited; FreshResultCapTokens default 0; JSON round-trip preserves the field.
- `internal/pipeline/prune_cap_test.go` (new): cap reached → no compaction + nudge emitted + CompactionCount unchanged; below cap → compacts normally; cap 0 → unlimited legacy with no nudge; cap reached while still over budget → no abort.
- `internal/pipeline/compaction_pressure_e2e_test.go` (extended): full-pipeline run at the cap → run completes without AbortRun, CompactMessages not called, nudge present in pending.

## Testing

No local Go on Windows — controller runs Docker builds/tests after review. Files verified manually:
- Brace balance check passes on all owned files.
- Valid UTF-8, tab indentation (gofmt-style) verified.
- Struct tag alignment matches gofmt column layout (type col 22, tag col 47 across both config structs).
- Existing regression tests are unaffected: `TestPruneStage_StillOverAfterCompaction_ReturnsAbortRun` and the e2e compaction tests use cap=0 (Compaction nil), so the gate is skipped.

## Concerns / Notes

- **Nudge frequency:** The nudge is emitted on every iteration after the cap is reached while still over budget. It is `Transient` (not persisted, per `persistableMessages`), so no history pollution — but it does accumulate in pending across iterations. Kept minimal per acceptance criteria; could be once-per-run guarded later if needed.
- **Final request guard untouched:** `final_request_guard.go:246` still increments `CompactionCount` in `compactForFinalRequestBudget`. The cap gate intentionally lives only in `prune_stage.go` per task ownership. A session that hits the cap via the final-request-guard path would still compact there; the prune path stops mid-loop compactions. This matches the assigned scope.

Status: DONE
Summary: Long-session compaction cap (default 12, 0 = unlimited legacy) gated in PruneStage Phase 2 with a transient nudge instead of abort; both new config fields added to the authority config file; defaults const + Default() seeding + 3 test files.
Concerns/Blockers: None blocking. Nudge-per-iteration accumulation noted above; final_request_guard compaction not capped (out of scope).
