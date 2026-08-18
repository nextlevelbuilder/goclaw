# Runbook: Vận hành Reliability Layer (GoClaw Gateway)

## 1. Tổng quan

Tài liệu này hướng dẫn vận hành tầng reliability của GoClaw Gateway: SLO / error budget,
cảnh báo (webhook + bgalert), dashboard Grafana, và các incident runbook phổ biến.

Các thành phần vận hành nằm ở:

- `internal/reliability/` — counters, circuit breaker, health registry, rate-limit coordinator,
  metrics sink (Phase 10: OTel).
- `internal/providers/` — failover, cooldown tracker, remote-health snapshot.
- `cmd/gateway_heartbeat.go` — stale-run sweep (standard path).
- `cmd/health_cmd.go` — `goclaw health` / `goclaw health --check`.
- `internal/tracing/collector.go` — trace collector (stale recovery đang vô hiệu).

Phase 10 bổ sung: OTel metrics sink (build-tag `otel`), SLO evaluator cấu hình qua
`reliability.slo.*`, webhook alert qua `reliability.alerts.*`, và wire stale-run sweep vào
managed/desktop path. Chi tiết scope: `plans/260815-2340-goclaw-repository-reliability/phase-10-production-hardening.md`.

---

## 2. SLO & Error Budget

### 2.1 Mục tiêu (§24 của reliability plan)

Nguồn: `plans/260815-2340-goclaw-repository-reliability/GoClaw_Upgrade_Improvement_Plan.md:1143-1158`.

| SLO | Target | Counter dùng để đo | Error budget (window 1h) |
|-----|--------|--------------------|--------------------------|
| Successful runs | `>= 99%` | `llm requests` / `llm successes` | <= 1% fail |
| Provider-induced recovery success | `>= 95%` | `agent recovered` / recovery attempts | <= 5% fail |
| False failure while backend still running | `= 0` | run bị đánh dấu failed nhưng loop vẫn chạy | 0 |
| Message duplication | `= 0` | số lần gửi trùng một message | 0 |
| Unhandled stream disconnect | `< 0.1%` | `stream stalls` / `llm requests` | <= 0.1% |
| 429-caused hard failures | `< 0.5%` | `rate_limited` là nguyên nhân fail run | <= 0.5% |

Counter tên gốc (OTel) có prefix `goclaw.reliability.` — xem `phase-10-production-hardening.md:145`.
Khi qua OTel Collector → Prometheus remote-write, tên chuyển thành underscore kèm `_total`
cho counter, ví dụ `goclaw.reliability.requests` → `goclaw_reliability_requests_total`.
Danh sách counter chốt ở `internal/reliability/otelmetrics_sink_otel.go`
(`requests`, `successes`, `retries`, `rate_limited`, `server_errors`, `timeouts`,
`stream_stalls`, `loop_detected`, `repeated_tool_calls`, `empty_outputs`,
`premature_completions`, `agent_recovered`, `agent_continued` + histogram `llm_latency_ms`).
Chi tiết tính toán: xem `docs/runbooks/reliability-slo.md`.

### 2.2 Đọc burn-rate và cách cảnh báo

- **Success rate** = `successes / requests × 100` (Snapshot fields `LLMSuccesses`, `LLMRequests` —
  `internal/reliability/metrics.go:108-126`).
- **Burn rate** = `(100 − observed%) / (100 − target%)`. Với target 99%: nếu observed 98.5%,
  burn rate = 1.5 / 1 = 1.5 → error budget bị đốt nhanh 1.5× so với dự định.
- **Window cảnh báo:** mặc định `window_seconds = 3600` (1h) qua `reliability.slo.window_seconds`.
  Khi `burn rate > 1` trong 1h, coi như budget đang cháy nhanh → cần can thiệp.
- **Ngưỡng cảnh báo:** webhook alert được gửi khi burn-rate vượt ngưỡng (Phase 10 — WS-B).
  Trên dashboard, stat "SLO burn-rate" đổi màu: `green >= 99%`, `yellow 90–99%`, `red < 90%`.
- **Không gửi alert trên idle:** evaluator chỉ chạy khi có snapshot delta (không có request =
  không tính) — tránh division-by-zero và false burn trên idle
  (`phase-10-production-hardening.md:140-141`).

> Các con số §24 là **mục tiêu ban đầu** — phải hiệu chỉnh bằng production telemetry
> (`GoClaw_Upgrade_Improvement_Plan.md:1158`). Xem quy trình ở `reliability-slo.md`.

---

## 3. Alerts

### 3.1 Bật alerts qua config

Thêm vào config JSON5 (biến môi trường `GOCLAW_CONFIG`):

```json5
{
  reliability: {
    slo: {
      enabled: true,
      target_percent: 99,
      window_seconds: 3600
    },
    alerts: {
      enabled: true,
      webhook_url: "https://hooks.example.com/hooks/abc",
      min_interval_seconds: 60
    }
  }
}
```

- `reliability.alerts.enabled` — bật/tắt webhook.
- `reliability.alerts.webhook_url` — endpoint nhận POST JSON.
- `reliability.alerts.min_interval_seconds` — cooldown tối thiểu giữa 2 lần gửi webhook
  (mặc định 60s) để tránh spam (`phase-10-production-hardening.md:138-139`).

Các key `reliability.slo.*` / `reliability.alerts.*` do WS-A đưa vào
`internal/config/config.go` `ReliabilityConfig` (block hiện tại: `:70-80`). Theo contract Phase 10:
`reliability.slo.{enabled,target_percent,window_seconds}` + `reliability.alerts.{enabled,webhook_url,min_interval_seconds}`.

### 3.2 Payload webhook

WS-B gửi POST JSON best-effort (timeout 5s, không block) qua `internal/bgalert/webhook.go`,
shape thực tế (`WebhookPayload`, snake_case):

```json
{
  "severity": "critical|warning",
  "title": "GoClaw background provider error",
  "message": "<sanitizeErrorMessage(err.Error())>",
  "worker": "<workerName>",
  "reason": "auth|billing|model_not_found|...",
  "timestamp": "2026-08-18T12:00:00Z",
  "meta": {}
}
```

- `severity`: `auth`, `auth_permanent`, `billing`, `model_not_found` → `critical`; còn lại → `warning`.
- `message` luôn qua `sanitizeErrorMessage` (che API key + cắt 200 rune).
- Cooldown: `reliability.alerts.min_interval_seconds` (mặc định 60s) giới hạn 1 lần gửi mỗi
  khoảng thời gian; transport failure không refresh cooldown (không hammer endpoint đang chết).

### 3.3 Provider error qua bgalert

- **Non-retryable reasons được alert:** `auth`, `auth_permanent`, `billing`, `model_not_found`
  (`internal/bgalert/report.go:36-41`).
- `ReportProviderError` lưu vào `system_configs` key `alert.background.provider_error`
  (`report.go:19`) và broadcast WS event (`protocol.EventBackgroundError`) (`report.go:88-95`).
- Message luôn được sanitize: che API key + cắt 200 rune (`report.go:112-120`).
- Cách xử lý theo reason:
  - `auth` / `auth_permanent` → kiểm tra API key / credential của provider, có thể cần cấp lại.
  - `billing` → kiểm tra số dư/quota tài khoản provider.
  - `model_not_found` → model bị xóa/deactivate; sửa `llm_providers` hoặc model registry.

---

## 4. Dashboards

### 4.1 Import

File: `deploy/grafana/goclaw-reliability.json`.

1. Grafana → **Dashboards → New → Import**, chọn file (hoặc dán JSON).
2. Chọn datasource (Prometheus — OTel Collector remote-write, hoặc OTel-native) từ dropdown
   `${datasource}`.
3. UID tự nhận diện `goclaw-reliability`, title "GoClaw Reliability".

Metric nguồn: OTel counters prefix `goclaw.reliability.` → Prometheus
`goclaw_reliability_*_{total|sum|count}`.

### 4.2 Ý nghĩa các panel

| Panel | Loại | Ý nghĩa | Cách đọc |
|-------|------|---------|----------|
| Requests | timeseries | LLM requests / giây (`llm.requests`) | Ton lên = tải tăng |
| Success rate % | timeseries | `successes / requests × 100` | < 99% → xem SLO §2 |
| SLO burn-rate | stat | success rate + threshold 99% | đỏ (<90%), vàng (90–99), xanh (>=99) |
| Retries | timeseries | `llm.retries` | Tăng = provider không ổn định |
| Rate limited | timeseries | `llm.rate_limited` (429) | Xem incident §5.1 |
| Server errors | timeseries | `llm.server_errors` (5xx) | Xem incident §5.5 |
| Timeouts | timeseries | `llm.timeouts` | Tăng = latency/window provider |
| Stream stalls | timeseries | `llm.stream_stalls` | Xem incident §5.2 |
| Agent recovered | stat | `agent.recovered` (`metrics.go:96`) | Recovery thành công |
| Agent continued | stat | `agent.continued` (`metrics.go:99`) | Continue sau premature |
| Loop detected | stat | `loop.detected` (`metrics.go:105`) | Tool-loop detector chặn |
| LLM latency avg | timeseries | `llm.latency` sum/count → ms | P95/P99 có nhiều hơn avg |

> Counter là **process-wide** (Snapshot không mang label provider/model —
> `internal/reliability/metrics.go:108-126`), nên các panel dùng `sum(...)` đơn, không `by(provider)`.
> Muốn phân theo provider: model, query trực tiếp `providers.RemoteHealthSnapshotAll()`
> (`internal/providers/remote_health.go:47-61`) qua `goclaw health` hoặc một exporter riêng.

---

## 5. Incident Runbooks

### 5.1 429 storm

**Cơ chế tự bảo vệ:**

- `RateLimitCoordinator` single-flight: một run nhận 429 → mọi run cùng provider:model chờ
  chung cooldown (`internal/reliability/ratelimit.go:42-50`, `ShouldWait` `:72`). Thiếu
  Retry-After → mặc định 30s (`ratelimit.go:43`).
- `CooldownTracker` theo reason: rate_limit 30s, overloaded 60s, billing 5m, auth 10m,
  model_not_found 1h (`internal/providers/cooldown.go:32-42`).
- Circuit breaker: 5 failures liên tiếp → open, cooldown 30s, half-open 1 probe
  (`internal/reliability/circuitbreaker.go:61-69`).

**Các bước:**

1. Xác nhận qua dashboard (Rate limited tăng) và `goclaw health` (cột `RATE-LIMITED-UNTIL`).
2. **Không restart gateway** — cooldown là in-memory, restart mất state.
3. Theo dõi: coordinator tự chờ Retry-After, breaker tự half-open probe.
4. Nếu profile rotatable (`isProfileRotatable` — `internal/providers/failover.go:57-65`),
   failover chuyển profile cho rate_limit.
5. Nếu kéo dài > 10 phút → kiểm tra account/plan provider. Nếu reason `billing`/`auth` sẽ có
   cảnh báo `alert.background.provider_error`.

### 5.2 Stream disconnect

**Cơ chế:**

- Watchdog stream: `reliability.stream.idle_timeout_ms` (mặc định 60s), `first_byte_timeout_ms`
  (mặc định 0 = tắt — transport ResponseHeaderTimeout là bảo hiểm)
  (`internal/config/config.go:88-99`, defaults `:144-148`).
- Cancelled stream **không giết run**: `agent_runs.heartbeat_at` được advance mỗi 10s
  (`RunsConfig.HeartbeatIntervalMs` default `config.go:131`), run sẽ retry hoặc recover
  (counter `agent_recovered`).

**Các bước:**

1. Dashboard Stream stalls tăng / `goclaw health` cột `STALLS` tăng.
2. Chạy `goclaw health --check` — case B xác nhận EOF / connection reset được phân loại
   retryable (`cmd/health_cmd.go:148-159`).
3. Kiểm tra mạng, proxy, idle timeout quá thấp → tăng `idle_timeout_ms`.
4. Nếu run không mất (heartbeat vẫn advance, `agent_runs.status != failed`) → không cần can thiệp.

### 5.3 Stale run

**Cơ chế:**

- Sweep cross-tenant: `runStaleRunsSweep` chạy mỗi `sweep_interval` (30s), mark failed run có
  heartbeat lag > `stale_after` (60s) (`cmd/gateway_heartbeat.go:29-54`).
- Defaults: heartbeat 10s / stale 60s / sweep 30s (`internal/config/config.go:130-134`).
- Standard path wire tại `cmd/gateway_heartbeat.go:110-116` (guard `pgStores.Runs != nil`).
- **Phase 10:** thêm wire vào managed/desktop path (`cmd/gateway_managed.go`, guard
  `stores.Runs != nil`) — `phase-10-production-hardening.md:66-68`.

**Chẩn đoán:**

- Log: `runs.stale_sweep_marked_failed` có `count`; lỗi: `runs.stale_sweep_failed`.
- Kiểm tra trạng thái run trong DB:
  `SELECT id, status, heartbeat_at FROM agent_runs WHERE status = 'running';`
- Run "running" lâu vẫn hợp lệ nếu heartbeat còn advance. Chỉ khi heartbeat không advance mới bị
  sweep. Long-running hợp lệ không bị giết vì sweep theo `heartbeat_at`, không theo `start_time`.

### 5.4 Trace stuck running

**Quan trọng — KHÔNG bật staleRecoveryLoop hiện tại.**

- `Collector.Start()` cố ý **không** khởi động `staleRecoveryLoop`: sweep theo `start_time`
  sẽ giết long-running hợp lệ (research chains, codegen lớn, shell lệnh dài > 10 phút).
  Xem note `internal/tracing/collector.go:166-186`, dòng `:180` (bị comment) và `:186`
  (giữ hàm reachable).
- Re-enable chỉ sau khi thêm cột `last_span_at` và gating theo "no activity for N minutes"
  (`collector.go:170-175`).
- Thay thế hiện đang dùng: `RecoverInterruptedRuns` ở startup — đánh dấu failed các run
  của process bị kill (không bao giờ emit terminal `run.status`)
  (`cmd/gateway_managed.go:215-221`).

**Các bước:**

1. Nếu thấy trace `running` kẹt sau crash: restart gateway → startup recovery đánh dấu failed.
2. KHÔNG chạy SQL xóa / update tùy tiện; xác nhận run thật sự chết (không còn process).
3. Nếu muốn cải thiện: theo dõi issue `last_span_at` — đừng bật `staleRecoveryLoop` khi chưa có.

### 5.5 Provider outage

**Cơ chế:**

- Circuit breaker: 5 failures liên tiếp → open; 30s cooldown → half-open 1 probe; probe thành
  công → đóng, fail → tiếp tục open (`circuitbreaker.go:61-69`, `Allow` `:120`).
- Fallback policy `health_order`: xếp hạng candidate theo health score; gate tối thiểu 5
  attempts trước khi re-order (`FallbackStrategyHealth = "health_order"`,
  `internal/providers/model_fallback.go:20-25, 278-283, 373-376`).

**Các bước:**

1. `goclaw health` — cột `CIRCUIT = open` (không cho request), `SCORE` giảm.
2. Dashboard Server errors / Timeouts tăng.
3. Kiểm tra provider status bên ngoài; chờ breaker half-open probe tự phục hồi.
4. Nếu `auth`/`billing`/`model_not_found` → xử lý tài khoản (mục §3.3).

---

## 6. Checklist trước/sau deploy (OTel build)

### Trước deploy (Pre)

1. Config có `telemetry.enabled: true` + `telemetry.endpoint`
   (`internal/config/config.go:706-714` — enabled/endpoint/protocol/insecure/service_name/headers).
2. Build với tag `otel`: `go build -tags otel ./...`. Binary build **không** có tag `otel`
   (default) bỏ qua metrics export — noop sink (`phase-10:87-91`).
3. `go vet -tags otel ./...`.
4. OTel Collector / Prometheus đã nhận được metric tại endpoint.
5. Các key `reliability.slo.*` / `reliability.alerts.*` viết đúng.

### Sau deploy (Post)

1. Gateway log: `OpenTelemetry OTLP export enabled` (`cmd/gateway_otel.go:38`).
2. `goclaw health` hiển thị counters (`cmd/health_cmd.go:70-85`).
3. Grafana: refresh dashboard, Requests / Success rate có dữ liệu trong ~5 phút.
4. Gửi 1 test request → counter tăng (Requests +1, successes tương ứng).
5. Test webhook: tạo 1 lỗi non-retryable (auth/billing) → nhận POST.
6. Nếu `reliability.slo.enabled=true` → kiểm tra SLOStatus / burn-rate panel có giá trị.

### Rollback

- Set `telemetry.enabled: false` → restart (metrics export ngừng, phần còn lại vẫn chạy).
- Hoặc build lại binary không tag `otel`; revert `go.mod`/`go.sum` nếu cần
  (`phase-10:132-134`).

---

## 7. Tham chiếu source

| Nội dung | Tham chiếu |
|----------|-----------|
| Snapshot counters | `internal/reliability/metrics.go:108-126`, `Take` `:129-149` |
| RegisterSink / Flush | `internal/reliability/metrics.go:163-233` |
| Recovery / continuation counters | `metrics.go:96-105` |
| Trace stale recovery disabled | `internal/tracing/collector.go:166-186` |
| RecoverInterruptedRuns startup | `cmd/gateway_managed.go:215-221` |
| ReliabilityConfig (runs/circuit/stream) | `internal/config/config.go:70-80`, defaults `:130-148` |
| TelemetryConfig | `internal/config/config.go:706-714` |
| Stream watchdog timeouts | `config.go:88-99` |
| Stale-run sweep + logs | `cmd/gateway_heartbeat.go:29-54`, wire `:110-116` |
| Remote health snapshot | `internal/providers/remote_health.go:47-114` |
| Cooldown durations | `internal/providers/cooldown.go:32-42` |
| FailoverReason taxonomy | `internal/providers/error_classify.go:13-23` |
| bgalert reasons + payload | `internal/bgalert/report.go:19-54` |
| Sanitize message | `internal/bgalert/report.go:107-120` |
| `goclaw health` dump / check | `cmd/health_cmd.go:40-104` / `:124-174` |
| Circuit breaker defaults | `internal/reliability/circuitbreaker.go:61-69` |
| Rate-limit coordinator | `internal/reliability/ratelimit.go:42-72` |
| SLO targets §24 | `plans/.../GoClaw_Upgrade_Improvement_Plan.md:1143-1158` |
| Phase 10 scope / config keys | `plans/.../phase-10-production-hardening.md:44-68, 138-147` |