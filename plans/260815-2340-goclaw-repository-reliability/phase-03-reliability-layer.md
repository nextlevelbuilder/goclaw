# Phase 03 — Reliability Layer: Circuit Breaker + Health Registry + Rate-Limit Coordinator + Metrics

> **Status: ✅ DONE** — All components implemented + reviewed (1 critical + 4 major fixed), 12 new tests, race clean.

## Context

Scout xác nhận đã có: retry engine (`providers/retry.go`), failover 2 tầng (`failover.go`), cooldown LRU (`cooldown.go`), error classifier (`error_classify.go`). **Còn thiếu:** circuit breaker state machine, provider/model health registry + scoring, shared rate-limit coordination (single-flight, tránh retry storm), và OTel metrics counters (hiện chỉ có tracing).

## Requirements

### 3.1 CircuitBreaker
```go
type CircuitState int  // Healthy, Degraded, Open, HalfOpen

type CircuitBreaker struct { ... }
func NewCircuitBreaker(opts Options) *CircuitBreaker   // failureThreshold, cooldown, halfOpenSuccesses, etc.
func (c *CircuitBreaker) Allow(key string) bool          // key = provider+model; false khi Open
func (c *CircuitBreaker) RecordSuccess(key string)
func (c *CircuitBreaker) RecordFailure(key string)      // chuyển state theo ngưỡng
func (c *CircuitBreaker) State(key string) CircuitState
func (c *CircuitBreaker) NextRetryAt(key string) time.Time
```
- Khi Open: `Allow`=false, không gửi request mới trong cooldown; khi hết cooldown chuyển HalfOpen, chỉ cho phép probe request.

### 3.2 HealthRegistry
```go
type ModelHealth struct {
    Provider, Model      string
    ConsecutiveFailures  int
    RateLimitUntil       time.Time
    TimeoutCount         int
    StreamStallCount     int
    ToolErrorRate        float64
    LastSuccessAt        time.Time
    LastFailureAt        time.Time
    CircuitState         CircuitState
}

type HealthRegistry struct { ... }
func NewHealthRegistry(cb *CircuitBreaker) *HealthRegistry
func (h *HealthRegistry) ObserveSuccess(provider, model string, latency time.Duration)
func (h *HealthRegistry) ObserveFailure(provider, model string, code ErrorCode)
func (h *HealthRegistry) ObserveTimeout / ObserveStreamStall / ObserveToolError
func (h *HealthRegistry) Score(provider, model string) float64   // 0..1 runtime reliability score
func (h *HealthRegistry) Status(provider, model string) ModelHealth
```
- Score dựa trên: timeouts, 429 rate, 5xx, stream stalls, tool-error rate, completion reliability. Đây là **runtime reliability score**, không phải benchmark intelligence.

### 3.3 RateLimitCoordinator (shared single-flight)
```go
type Cooldown struct { Provider, Model, Key string; Until time.Time }
type RateLimitCoordinator struct { ... }
func NewRateLimitCoordinator(shared *CooldownTracker or internal map) *RateLimitCoordinator
func (r *RateLimitCoordinator) CooldownFor(provider, model string) (time.Duration, bool)  // đã có cooldown?
func (r *RateLimitCoordinator) ShouldWait(provider, model string) (wait time.Duration)     // chờ để không retry storm
func (r *RateLimitCoordinator) Record429(provider, model string, retryAfter time.Duration)
func (r *RateLimitCoordinator) Waiters(provider, model string) int
```
- Đảm bảo 100 run cùng provider/model không đồng thời cùng retry sau 2s.

### 3.4 ReliabilityMetrics
- OTel meter: counters/histograms khi collect xong (khớp trace collector hiện tại):
  - `llm.requests_total`, `llm.success_total`, `llm.retries_total`, `llm.429_total` (rate_limited), `llm.5xx_total`, `llm.timeout_total`, `llm.latency_ms` (histogram), `llm.first_token_latency_ms`
  - `agent.recovered_total`, `agent.continued_total`, `agent.premature_completion_total`, `agent.loop_detected_total`
- Gắn vào nơi phù hợp (ví dụ gọi từ retry hook / loop callback nếu wiring, hoặc expose qua Collector). **Phạm vi phase này:** tạo package metrics với API recordX() gọn, **chưa** bắt buộc wiring vào mọi provider (để tránh phạm vi né nở), có test.
  - Liên kết với trace collector: reload và emit khi flush (nếu không tốn quá nhiều), hoặc đơn giản đứng độc lập.

## Files to create/modify

- Mới `internal/reliability/circuitbreaker.go`, `circuitbreaker_test.go`
- Mới `internal/reliability/health.go`, `health_test.go`
- Mới `internal/reliability/ratelimit.go`, `ratelimit_test.go`
- Mới `internal/reliability/metrics.go`, `metrics_test.go`
- Nếu cần, mở nhỏ `internal/tracing/collector.go` để hook metric emission (không bắt buộc).

## Review Fixes (2026-08-16)

Code review tìm thấy và đã fix:

1. **[Critical] `Wait` stale-waiter** — waiter cũ không còn xóa cooldown mới hơn: tách `maybeClearCooldown(k, until)` so deadline đang chờ (ratelimit.go).
2. **[Major] Half-open probe wedge** — thêm `CircuitOptions.ProbeTimeout` (default 30s); probe được Allow mà không resolve sẽ tự nhả slot, key không bị kẹt HalfOpen vĩnh viễn (circuitbreaker.go: `probeOutstanding`/`halfOpenSince`).
3. **[Major] Classification precedence** — check `net.Error.Timeout()` TRƯỚC `context.Canceled`: transport timeout wrap context.Canceled không bị xếp ErrRunCancelled (errors.go).
4. **[Major] Metrics race + lost increments** — `globalSink` → `atomic.Value` (boxed `sinkHolder` để cùng concrete type); `Flush` dùng per-counter `Swap(0)` qua `resetInto`, có `flushMu` serial hóa (metrics.go).
5. **[Major] Score đếm kép** — timeout/empty/premature đã phản ánh trong success ratio; chỉ còn penalty cho stream stall + tool error; xóa `maxInt` dead (health.go).

**Test mới (12):** ratelimit (Wait no-op/expired/match/stale/cancel), circuitbreaker (stale probe release/block-before-timeout), errors (timeout-beats-cancellation precedence), health (single-count timeout, stall+tool penalty), metrics (post-flush increments, concurrent record+flush).

## Implementation Steps

1. CircuitBreaker (state machine + interval-based cooldown), test.
2. HealthRegistry (observe + score), test.
3. RateLimitCoordinator (single-flight), test.
4. Metrics package (counters/histograms + safe no-op default), test.
5. Build/test toàn bộ trong Docker.

## Tests / Validation

- Unit tests cho state chuyển đổi (Open→HalfOpen→Healthy, ngưỡng failure).
- Test rate-limit coordinator tránh retry storm (2 run chờ chung cooldown).
- Test health score giảm khi failures tăng.
- `go build ./...` + `go test ./internal/reliability/...` pass trong Docker.

## Risks / Rollback

- **Not wired to providers** là risk: các cơ chế mới chưa chủ động. → Ghi rõ trong phase này là "nền tảng", phase sau wiring vào `providers/retry.go` + `loop` nếu user muốn. Không phá vỡ behavior hiện có.
- Public contracts không đổi. Rollback: xóa package hoặc để nguyên (không import từ nơi khác → không ảnh hưởng build).