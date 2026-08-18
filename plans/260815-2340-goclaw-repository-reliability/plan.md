# Plan: goclaw repo setup + Phase 0-2 Reliability Upgrade

**Created:** 2026-08-15
**Status:** ✅ DONE — Phase 01-03 all implemented, code-reviewed (1 critical + 4 major fixed, 12 tests), race clean, docs updated. Code có trong repo public `qkhalk/goclaw`, pushed lên `dev` (commits `5100087d`, `def1a9e6`), source đã un-nest về repo root, CI/CD disabled qua `.github.disabled`. Plan được lưu tại `plans/260815-2340-goclaw-repository-reliability/`.
**Broken ground by:** scout của codebase hiện tại (đã xác nhận nhiều cơ chế đã tồn tại sẵn trong code).

## Mục tiêu

1. **Repo:** Xóa repo PRIVATE cũ `qkhalk/goclaw`, tạo lại **public** `goclaw` từ toàn bộ code hiện tại, code được đặt trong folder **`goclaw-mod/`** (module path giữ nguyên, CI/CD root bị vô hiệu tự nhiên). Thiết lập remote `upstream = nextlevelbuilder/goclaw` để anh **tự merge thủ công** khi thấy upstream có thay đổi hay. **Không** auto-sync, **không** để CI/CD chạy thẳng main.
2. **Upgrade (Phase 0-2 P0):** Nhắm đúng các khoảng trống thực tế đã scout:

| Mục | Nội dung | File liên quan |
|---|---|---|
| **P0** | Error Taxonomy thống nhất (code + retryability + severity) | mới `internal/reliability/errors.go` |
| **P0** | Circuit breaker state machine | mới `internal/reliability/circuitbreaker.go` |
| **P0** | Provider/Model Health Registry + scoring | mới `internal/reliability/health.go` |
| **P0** | Shared rate-limit coordinator (single-flight cooldown) | mới `internal/reliability/ratelimit.go` |
| **P0** | Baseline instrumentation: LLM attempt métadata + counters | mới `internal/reliability/metrics.go` |

Ngoài NATURAL: retry (đã có), failover (đã có), cooldown (đã có), error classifier (đã có), run timeline (đã có). Không re-implement những phần đã có.

## Phases

- [x] [phase-01-repo-setup.md](phase-01-repo-setup.md) — repo public + goclaw-mod + upstream
- [x] [phase-02-error-taxonomy.md](phase-02-error-taxonomy.md) — error taxonomy thống nhất
- [x] [phase-03-reliability-layer.md](phase-03-reliability-layer.md) — circuit breaker + health registry + rate-limit coordinator + metrics
- [x] phase-04-streaming-reliability.md — streaming watchdog + disconnect recovery
- [x] phase-05-weak-model-resilience.md — malformed output + tool-loop detection + repair
- [x] phase-06-intelligent-fallback.md — capability profiles + health_order policy + diagnostics
- [x] phase-07-tool-reliability.md — tool retry/cooldown + loop detection
- [x] phase-08-context-optimization.md — token budgets + compaction + budget telemetry
- [x] phase-09-testing-suite.md — provider chaos + weak-model chaos + run-lifecycle PG tests
- [ ] [phase-10-production-hardening.md](phase-10-production-hardening.md) — SLOs, Alerts, dashboards, Runbook, managed stale-run sweep, migration docs

## Dependencies

- phase-01 độc lập (repo setup, không đụng code).
- phase-02 trước phase-03 (taxonomy là nền cho circuit breaker/health/ratelimit dùng error classes).

## Acceptance Criteria (tổng thể)

1. Repo `github.com/qkhalk/goclaw` **public**, code nằm trong `goclaw-mod/`, remote `upstream` trỏ về `nextlevelbuilder/goclaw`.
2. `goclaw-mod` build và test pass trong Docker `golang:1.22-alpine` (Go 1.26.0 trong go.mod — container sẽ hiển thị version thực; nếu 1.26 chưa có image, dùng `golang:1.26-alpine` khi có hoặc hạ xuống nếu build lỗi).
3. CI/CD hiện tại của repo upstream **không tự chạy** khi đẩy lên `qkhalk/goclaw` (code trong `goclaw-mod/`, workflow ở root không tìm thấy Go workspace → không kích hoạt deploy).
4. Error taxonomy có unit tests pass.
5. Reliability layer (circuit breaker / health / ratelimit / metrics) có unit + integration tests pass trong Docker.
6. Không phá vỡ behavior hiện có: provider retry/failover/cooldown vẫn hoạt động; public contracts (Provider interface, Store interfaces, RunTimelineRecorder) không đổi.

## Note thực thi

- Máy local **không có Go** → build/test bằng Docker.
- Repo cũ `qkhalk/goclaw` cần **xóa** (decision của user) → thao tác phá hủy, sẽ xác nhận lần cuối ở bước duyệt.