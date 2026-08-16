# Phase 06 — Intelligent Fallback: Capability Profile + Fallback Policy + Health-Ranked Routing

> **Status: DONE** — 2026-08-17. Merged `feat/phase6-intelligent-fallback` → `dev` (fast-forward `3ee4cd79..d4006376`, PR #4, CI 3/3 SUCCESS: go/release-versioning/web). Scope §32 Phase 6 items 1-4 ticked, cost/latency/reliability policy deferred (cần production telemetry).
> **Scope tickets:** plan §32 Phase 6 (model capability profile, fallback chain, health-based routing, cooldown-aware routing, cost/latency/reliability policy) + §6 + §20.

## Context (scout 2026-08-17, verified)

**ĐÃ CÓ (từ Phase 3 + upstream) — KHÔNG xây lại:**
- **`ModelFallbackProvider`** (`internal/providers/model_fallback.go`): ordered chain; `orderByHealth()` re-rank fallbacks bằng `HealthRegistry.Score` (min 5 attempts); `noFallbackAfterStreamError` guard (không fallback sau khi stream đã emit chunk); `FailoverSummaryError`; cooldown skip (`IsAvailable`/`ShouldProbe` từ `CooldownTracker`).
- **`CooldownTracker`** (`internal/providers/cooldown.go`): per-reason durations (429 30s, overloaded 60s, auth 10m, model_not_found 1h...), overload escalation cap 5 (2x), TTL cleanup, bridge tới `RateLimitCoordinator` process-wide (single-flight: concurrent runs cùng provider:model chờ chung). `WithLocalBridge(false)` cho tests.
- **`RunWithFailover`** (`internal/providers/failover.go`): 2-tier — profile rotation cho transient (rate_limit/overloaded/server_error/timeout/auth), model fallback cho permanent (auth_permanent/billing/format/model_not_found/content_policy); `FailoverStreamed` sentinel; context_overflow → return ngay cho compaction; caps (overload 3, profile 5).
- **`DefaultClassifier`** (`error_classify.go`): pattern-based, provider-specific (openai/anthropic), context-overflow detection (multilingual), network-error → timeout.
- **`HealthRegistry`** (`internal/reliability/health.go`): Score = success ratio − stall penalty 0.25 − tool-error 0.2 × circuit modifier; đã wire model counters (empty/malformed/invalid/repeated/loop/premature) qua `observeFailure` code switch; `Keys()` cho enumeration.
- **Per-agent config** (`internal/store/agent_store.go:511-577`): `agents.model_fallback` JSON — `{enabled, candidates[{provider,model}], strategy, max_attempts, cooldown_enabled}`; `NormalizeModelFallbackConfig` hiện **chỉ giữ** `priority_order` (strategy khác bị force về priority — GAP). Wire: `ResolveAgentProvider` (`internal/providerresolve/agent_provider.go:54-90`) → wrap `ModelFallbackProvider`; gọi tại `internal/agent/resolver.go:179` (1 site).
- **Wiring observability**: `reliability_wiring.go` — `observeSuccess/observeFailure/circuitAllow/waitRateLimit/record429Cooldown/isRateLimitedErr/rateLimitRetryAfter/observeStreamStall` nil-safe. `goclaw health` (cmd/health_cmd.go) dump + `--check`.
- **Singleton bundle-swap**: `reliability.Default()` + `Configure()` + `SetStream/SetPrematureCompletion` pattern (giữ components cũ, swap options).

**GAP cần xây (Phase 6):**
1. **Capability profile**: chưa có `ModelCapabilityProfile` (plan §6.1, §20). `ModelSpec` có `StreamTimeoutMs` (đọc qua reflection — field không tồn tại là 0 = inherit). Cần profile: tool_calling, structured_output, reasoning, streaming, vision, max_context. COLLECTED ở đâu? Chỉ static lookup (map hardcoded mặc định) — KHÔNG nhập thêm dependency.
2. **Fallback policy engine**: chưa có policy cấu hình cho max_hops, khi nào fallback (transport vs quality), có nên fallback khi stream error chưa emit chunk. Hiện `runOrdered` fallback chỉ khi error classifiable; model-quality failures (loop/repeated/premature — Phase 5 wired observability) chưa trigger fallback (đúng — record-only cố ý; Phase 6 KHÔNG đổi).
3. **Health-based routing**: `orderByHealth` chỉ chạy trong wrapper; user không chọn được. Strategy config hiện force về `priority_order` → cần thêm strategy `health_order` (rank cả chain khi có signal) + `max_hops` bounded.
4. **Cooldown-aware routing**: wrapper đã skip cooldown; thiếu observability (đếm skips trong attempts?) + test cho skip logic.

**Ranh giới rõ (KHÔNG làm Phase 6):** thay đổi provider hot-path bên trong từng adapter (openai_chat.go etc. — Phase 6 không đụng), model-quality fallback trigger (Phase 5 record-only giữ nguyên — Phase 7), DB migration, UI, threshold toolloop.

## Scope (đã duyệt) — 4 modules

| # | Module | Tóm tắt | Owner |
|---|--------|---------|-------|
| 1 | Capability profile | `ModelCapabilityProfile` struct + static registry (map default theo known models, fallback zero-value with sensible defaults) | **H** |
| 2 | Fallback policy | `FallbackPolicyConfig` (max_hops, health_strategy `priority_order`|`health_order`, min_attempts_for_health) + `orderCandidates` policy-aware; wire tới `NormalizeModelFallbackConfig` (config model_fallback JSON) | **H** |
| 3 | Health-ranked routing | `orderByHealth` generalized: rank cả chain (trừ primary) khi strategy=health_order; respects min attempts; đếm skip vào attempts diagnostics | **I** |
| 4 | Pre-fallback diagnostics | `FallbackAttempt` mở rộng (skipped_reason, health_score) + `goclaw health` hiển thị entry cho candidates + tests | **I** |

**Deferred (không làm phase này):** model-quality fallback trigger (loop/repeated/premature → switch model — sẽ làm trong phase sau, cần verifier thật), provider hot-path sửa (từng adapter), DB schema, UI, cost/latency routing policy (không đủ dữ liệu — defer tới khi có production telemetry), checkpoint/resume (phase khác).

## Contracts (bắt buộc — mọi agent implement theo đúng)

### C1. Capability profile (H)
```go
// internal/providers/capability_profile.go (mới, H)
type ModelCapabilityProfile struct {
    ToolCalling     CapabilityLevel // strong/medium/none
    StructuredOutput CapabilityLevel
    Reasoning       CapabilityLevel
    Streaming       CapabilityLevel
    Vision          CapabilityLevel
    MaxContext      int   // tokens; 0 = unknown
}
type CapabilityLevel string // "strong" | "medium" | "none"
func ProfileFor(model string) ModelCapabilityProfile // static lookup, default profile cho unknown
func (p ModelCapabilityProfile) SupportsToolCalls() bool
func (p ModelCapabilityProfile) SupportsStreaming() bool
```
- KHÔNG import gì mới; file trong `internal/providers/` nhưng KHÔNG đụng adapter files.
- Default profile: `{ToolCalling: "strong", Streaming: "strong", StructuredOutput: "medium", Reasoning: "medium", Vision: "none", MaxContext: 0}` — conservative cho unknown (giống health default 1.0) nhưng KHÔNG block tool calling.
- Hardcoded map: vài model nổi bật (gpt-4o, claude-3-5/4, gemini, deepseek-reasoner (reducing reasoning), qwen (vision), llama, mistral, phi (weak tool calling), codex) — 10-15 entries tối đa, mọi model khác → default.
- `CapabilityLevel` là string constants, so sánh được.

### C2. Fallback policy config (H)
```go
// internal/store/agent_store.go (H sửa) — ModelFallbackConfig mở rộng
type ModelFallbackConfig struct {
    // ... fields hiện có (Enabled, Strategy, Candidates, MaxAttempts, CooldownEnabled) giữ nguyên JSON tags
    MinAttemptsForHealth int `json:"min_attempts_for_health,omitempty" db:"-"`
}
const ModelFallbackStrategyHealthOrder = "health_order"
// NormalizeModelFallbackConfig: chấp nhận "priority_order" | "health_order"; default priority_order; MinAttemptsForHealth default 5 qua const export
```
- JSON shape: `model_fallback: {"enabled": true, "strategy": "health_order", "candidates": [...], "max_attempts": 3, "cooldown_enabled": true, "min_attempts_for_health": 5}`.
- ĐỔI behavior: `NormalizeModelFallbackConfig` hiện force mọi strategy về `priority_order` → Phase 6 chấp nhận `health_order` (backward compatible: strategy khác không nhận diện vẫn về priority_order như cũ).
- KHÔNG đổi struct JSON field names hiện có (agents cũ config remain valid).

### C3. Health-ranked ordering (I)
```go
// internal/providers/model_fallback.go (I sửa) — orderByHealth generalized
// - Khi policy.Strategy == health_order: rank cả chain (trừ primary) theo Score; candidates dưới MinAttemptsForHealth giữ cấu hình order (nhưng KHÔNG cần trailing như priority — xếp theo configured vị trí).
// - Khi priority_order: hành vi hiện tại (scored-trailing-unscored).
// - primary ALWAYS first (caller's explicit choice wins).
// - Ranh giới: model_fallback.go chỉ đọc policy từ ModelFallbackProvider fields; KHÔNG chạm failover.go.
```
- `NewModelFallbackProvider` thêm param? KHÔNG — thêm `WithFallbackPolicy(policy)` fluent setter (zero-value = priority_order behavior hiện tại, backward compatible).
- H đọc `MinAttemptsForHealth` → pass vào I qua `ResolveAgentProvider` (I sửa `agent_provider.go` — xem Files).

### C4. Pre-fallback diagnostics (I)
```go
// internal/providers/model_fallback.go (I) — FallbackAttempt mở rộng
type FallbackAttempt struct {
    // ... fields hiện có: Candidate, Classification, Err — giữ nguyên
    Skipped    bool   // candidate bị skip (cooldown / probe exhausted)
    SkipReason string // "cooldown" | "probe_exhausted" | ""
    HealthScore float64 // score tại thời điểm xếp hạng; -1 = no signal
}
```
- `runOrdered`: khi skip candidate → append attempt với Skipped=true + SkipReason; khi candidate được thử → HealthScore từ `reliability.Default().Health.Score` (nil-safe, -1 khi no registry).
- `FailoverSummaryError.Error()` — KHÔNG đổi format (public contract) ở failover.go; chỉ thêm fields diagnostics.
- `goclaw health`: thêm dòng "FALLBACK POLICY" dump (strategy + min_attempts_for_health) — thêm vào `cmd/health_cmd.go` nhưng KHÔNG đổi `RemoteHealthSnapshotAll` shape (additive).

### C5. Resolver wire (H sửa `agent_provider.go` — 1 site, hoặc I — xem Files giao thoa)
- `ResolveAgentProvider`: đọc `MinAttemptsForHealth` + strategy từ `fallbackCfg` → `WithFallbackPolicy(&providers.FallbackPolicy{Strategy: ..., MinAttemptsForHealth: ...})` trước khi return wrapper.
- `FallbackPolicy` struct sống trong `internal/providers/model_fallback.go` (C3-C4 cùng file).

### C6. Tests bắt buộc
| Agent | Test |
|-------|------|
| H | `capability_profile_test.go`: known model → profile đúng, unknown → default, Supports* helpers; `agent_provider_test.go` (H sửa/ thêm): strategy health_order wire → wrapper nhận policy |
| I | `model_fallback_test.go` additions: health_order ranking (primary first, scored ordered, unscored configured), priority_order giữ behavior cũ (regression), skip diagnostics (cooldown → Skipped=true + reason), min_attempts gate (dưới ngưỡng → giữ configured), policy zero-value backward compat; FailoverSummaryError fields mới |

## Files to create/modify

| Agent | Files (disjoint) |
|-------|------------------|
| H | mới `internal/providers/capability_profile.go` + test; sửa `internal/store/agent_store.go` (ModelFallbackConfig + NormalizeModelFallbackConfig + strategy const); sửa `internal/providerresolve/agent_provider.go` (wire policy) + `agent_provider_test.go`; sửa `internal/providers/model_fallback.go` (add `FallbackPolicy` struct + `WithFallbackPolicy` + `MinAttemptsForHealth` read) |
| I | sửa `internal/providers/model_fallback.go` (orderByHealth generalized + FallbackAttempt fields + skip diagnostics + HealthScore), `internal/providers/model_fallback_test.go` additions, `cmd/health_cmd.go` (fallback policy dump — additive) |

**⚠️ KHÔNG AI đụng:** `internal/providers/failover.go`, `cooldown.go` (thresholds), `error_classify.go`, `reliability/*` (additive methods only nếu bắt buộc — cố gắng 0), adapter files (`openai_chat.go`, `anthropic*.go`, `codex.go`, `ollama.go`, `vertex.go`, `dashscope.go`, `remote_health.go` shape), `internal/agent/*` (resolver.go read-only), `toolloop.go` thresholds, `internal/store/{pg,sqlitestore}/*` schema (KHÔNG migration), UI.

Giao thoa kiểm tra: **H ∩ I = `internal/providers/model_fallback.go`** — đây là điểm NHẠY. Phân bổ: H thêm struct `FallbackPolicy` + setter `WithFallbackPolicy` + field trên `ModelFallbackProvider` (top của file); I sửa `orderByHealth` body + `FallbackAttempt` + `runOrdered` skip diagnostics (phần giữa file). **Order thực hiện BẮT BUỘC: H trước (tạo struct + setter + config), I sau (sửa logic dùng struct)**. Hai agent chạy song song nhưng H commit trước I. Nếu không tránh được chạm cùng dòng → controller review kỹ merge.

## Implementation Steps

1. Controller viết phase file (file này) → dispatch 2 agents (H/I) song song theo contract. H giữ văn bản rõ "file model_fallback.go: CHỈ thêm FallbackPolicy struct + WithFallbackPolicy + field policy — KHÔNG sửa orderByHealth/runOrdered".
2. Agents implement, tự test phần mình teste được (controller build chung sau khi commit đủ).
3. Controller review từng diff (verify pass: đối chiếu contract, grep callers, merge thủ công nơi chạm nhau).
4. Commit tuần tự: **H → I** (H tạo struct + config + wire; I dùng struct).
5. Build + vet + test toàn bộ Docker (PG + sqliteonly).
6. Push branch `feat/phase6-intelligent-fallback` từ `dev` → PR (REST) → CI; agents theo dõi CI phần mình.
7. CI xanh → merge dev → tick Phase 6 items trong main plan → final report.

## Tests / Validation

- Unit tests C6 pass.
- `go build ./...` + `go build -tags sqliteonly ./...` + `go vet ./...` in Docker (mounts như Phase 4/5).
- CI PR xanh trên fork qkhalk/goclaw.
- Không viết load/benchmark (rule repo).

## Risks / Rollback

- **health_order gây churn khi signal yếu**: configurable `min_attempts_for_health` (default 5); KHÔNG đổi default strategy (priority_order giữ behavior hiện tại). Rollback: đổi strategy về priority_order — zero code change.
- **Chạm cùng file model_fallback.go giữa H/I**: phân bổ rõ ràng (H: struct+setter top; I: logic giữa), H commit trước, controller review merge. Trường hợp conflict → I rebase tay.
- **Backward compat**: JSON field names không đổi; strategy không nhận diện → priority_order (như cũ); `FallbackAttempt` chỉ thêm field (không đổi format Error()).
- Không đổi public contract `RemoteHealthSnapshotAll`/`RemoteHealthEntry` shape.