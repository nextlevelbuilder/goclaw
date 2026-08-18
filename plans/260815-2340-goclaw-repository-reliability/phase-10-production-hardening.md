# Phase 10 — Production hardening

## Context

Reliability plan `GoClaw_Upgrade_Improvement_Plan.md` §1515–1522 (6 mục: SLOs, Alerts,
dashboards, Runbook, automatic stale-run recovery, migration docs). User chốt 4 quyết định
(2026-08-18):

1. **SLO scope:** Prometheus sink + config-driven SLO — counters export được, error-budget
   tính từ telemetry (§24: "hiệu chỉnh bằng production telemetry"), không phải docs-only.
2. **Export cơ chế:** OTel-metrics qua build-tag `otel` (tận dụng `-tags otel` sẵn có,
   kế thừa `telemetry.enabled/endpoint` config — KISS, khớp OTel stack hiện tại).
3. **Sweep coverage:** wire `runStaleRunsSweep` vào managed path (`gateway_managed.go`) —
   hiện chỉ standard path (gateway.go:1067 → `startCronAndHeartbeat`) gọi, managed/desktop
   không có periodic sweep.
4. **Alert channel:** Webhook + bgalert — config `alerts.webhook_url`, POST JSON khi SLO
   burn-rate vượt ngưỡng hoặc provider error; tận dụng `internal/bgalert` + HTTP client sẵn có.

### Scout facts (verified 2026-08-18)

| Claim | Evidence |
|-------|----------|
| Stale-run sweep đã có code + store impl | `runStaleRunsSweep` `cmd/gateway_heartbeat.go:29`; `RecoverStaleRuns` `internal/store/pg/run_timeline.go:321` + `sqlitestore/run_timeline.go:333`; `agent_runs` table cả PG + SQLite (schema.sql:639). |
| Standard path đã wire sweep | `cmd/gateway.go:1067` → `startCronAndHeartbeat` → `go runStaleRunsSweep(...)` `gateway_heartbeat.go:110-116`. Guard `pgStores.Runs != nil`. |
| Managed/desktop KHÔNG wire sweep | `grep startCronAndHeartbeat|runStaleRunsSweep|RecoverStaleRuns` trong `cmd/gateway_managed.go` = 0 match. `wireExtras` (gateway_managed.go:46) wire `RunsStore` vào resolver (line 293) nhưng không background sweep. |
| `reliability.Metrics` có `Sink`/`RegisterSink`/`Flush` | `internal/reliability/metrics.go:163-233`. KHÔNG có production caller nào gọi `RegisterSink` (chỉ test). Counter: requests/successes/retries/rate-limited/server_errors/timeouts/stalls/loop/repeated_tool/empty_output/premature/recovered/continued + latency. |
| Snapshot aggregator sẵn có | `providers.RemoteHealthMetricsAll()` `internal/providers/remote_health.go:95` + `goclaw health` CLI dump (cmd/health_cmd.go). |
| OTel trace exporter sẵn có (pattern để copy) | `internal/tracing/otelexport/exporter.go` + wiring `cmd/gateway_otel.go` (`//go:build otel`) / `gateway_otel_noop.go`. |
| `telemetry.*` config sẵn có | `TelemetryConfig` config.go:706 (enabled/endpoint/protocol/insecure/service_name/headers). |
| `bgalert` sẵn có | `internal/bgalert/report.go` — `ReportProviderError` lưu system_configs + WS event; sanitize error message. KHÔNG có HTTP webhook. |
| Trace stale recovery đang disabled | `collector.go:180` `// go c.staleRecoveryLoop()` — sweep theo `start_time` sẽ giết long-running hợp lệ; đã có `RecoverInterruptedRuns` startup (gateway_managed.go:215-221). Không đổi trong Phase 10 (sweep theo `heartbeat_at` của `agent_runs` mới đủ điều kiện). |
| go.mod chưa có otlpmetric dep | go.mod có `otel/metric v1.40.0` (indirect), `otel/sdk v1.40.0`, `otlptrace*`. THIẾU `go.opentelemetry.io/otel/exporters/otlp/otlpmetric/*` + `otel/sdk/metric` → WS-A phải `go get` (build-tag otel). |
| Sweep interval defaults | `config.go:130-134` DefaultRunsHeartbeatIntervalMs=10000, StaleAfterMs=60000, SweepIntervalMs=30000. |

### Non-goals

- KHÔNG thêm Prometheus HTTP client `client_golang` (user chọn OTel-metrics, câu 2).
- KHÔNG đổi trace collector stale recovery (`start_time`-based sweep vẫn disabled).
- KHÔNG thêm load/stress/benchmark tests (CLAUDE.md).
- KHÔNG đổi WS protocol / web UI.

## Requirements

1. **SLOs (config-driven):** `ReliabilityConfig.SLO` = `{enabled, target_percent, window_seconds}`.
   Evaluator trong `internal/reliability/slo.go` tính success-rate + error-budget từ snapshot
   deltas (FIFO window), expose `SLOStatus`. Unit tests (non-otel build).
2. **Metrics export (OTel, build-tag otel):** `internal/reliability/otelmetrics_sink*` —
   sink nhận `Snapshot` từ `reliability.RegisterSink`, record qua OTel meters (counter +
   histogram latency). Gateway wiring file `cmd/*reliability*metrics{,_noop}.go` (pattern
   `gateway_otel.go`). Qua `otlpmetricgrpc|http` exporter + `telemetry.enabled/endpoint`.
   Need `go get go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetricgrpc (v1.40.0)`
   (+ `otlpmetrichttp`) + promote `otel/sdk/metric`.
3. **Alerts (Webhook + bgalert):** `AlertingConfig` = `{enabled, webhook_url}` (thêm vào
   `ReliabilityConfig`). `internal/bgalert/report.go` mở rộng: khi `ReportProviderError` xảy
   ra hoặc SLO burn-rate exceed → POST JSON (`{severity, title, message, timestamp, meta}`)
   tới webhook URL. Bounded retry (best-effort, timeout 5s, không block). Sanitize error
   message (tận dụng `sanitizeErrorMessage`). Test webhook sender (httptest server).
4. **Dashboards:** Grafana dashboard JSON mẫu (`deploy/grafana/` hoặc `docs/`) mapping từ
   OTel metric names → panels (requests, success rate, retries, rate-limited, stalls,
   provider health) + SLO burn-rate panel.
5. **Runbook:** `docs/runbooks/reliability-ops.md` — SLO định nghĩa, alert rules + webhook
   setup, dashboard import, incident runbook (429 storm, stream disconnect, stale runs,
   trace stuck running — tham chiếu `collector.go` note).
6. **Migration docs:** entry vào `docs/` chỉ rõ không có schema migration Phase 10 (chỉ
   config + build-tag code + docs); nếu thêm dep → `go mod tidy` + OTel build-gate.
7. **Automatic stale-run recovery parity:** wire `go runStaleRunsSweep(stores.Runs, ...)`
   ở managed path (`cmd/gateway_managed.go` — guard `stores.Runs != nil`), dùng
   `cfg.Reliability.Runs.EffectiveStaleAfter()/EffectiveSweepInterval()`.

## Files

### Modify
| File | Change | Owner |
|------|--------|-------|
| `internal/config/config.go` | Thêm `SLOConfig` + `AlertingConfig` vào `ReliabilityConfig` (:72-80 block). Defaults gần `RunsConfig` (:130). | WS-A |
| `internal/config/config_load.go` | (`nếu cần`) defaults cho SLO/Alerts trong `Default()`. | WS-A |
| `go.mod` / `go.sum` | `go get` otlpmetricgrpc/http + otel/sdk/metric (`-tags otel`). | WS-A |
| `cmd/gateway.go` | Wire OTel metrics exporter + flush loop cạnh `reliability.Configure` (:259). | WS-A |
| `internal/bgalert/report.go` | `ReportProviderError` gọi webhook sender khi `AlertingConfig.Enabled`. | WS-B |
| `cmd/gateway_managed.go` | Wire `go runStaleRunsSweep` cạnh `go runEvolutionCron` (:659). | WS-B |

### Create
| File | Change | Owner |
|------|--------|-------|
| `internal/reliability/slo.go` | SLO evaluator (target %, window, burn-rate, `SLOStatus`). | WS-A |
| `internal/reliability/slo_test.go` | Unit tests evaluator. | WS-A |
| `internal/reliability/otelmetrics_sink_otel.go` (`//go:build otel`) | Partial sink: RegisterSink OTel, Flusher, otlpmetric exporters. | WS-A |
| `internal/reliability/otelmetrics_sink_noop.go` (`//go:build !otel`) | Noop sink/flusher (KHÔNG break default build). | WS-A |
| `internal/config/slo_alert_config_test.go` | Config defaults/parse tests. | WS-A |
| `cmd/gateway_reliability_metrics.go` (`//go:build otel`) | Wire metrics exporter + flush (pattern gateway_otel.go). | WS-A |
| `cmd/gateway_reliability_metrics_noop.go` (`//go:build !otel`) | Noop wiring. | WS-A |
| `internal/bgalert/webhook.go` | HTTP webhook sender (httptest-able, best-effort, sanitize). | WS-B |
| `internal/bgalert/webhook_test.go` | Test sender (httptest server). | WS-B |
| `deploy/grafana/goclaw-reliability.json` | Grafana dashboard JSON (OTel datasource, SLO burn-rate). | WS-C |
| `docs/runbooks/reliability-ops.md` | Runbook: SLO, alerts, dashboard, incidents. | WS-C |
| `docs/runbooks/reliability-slo.md` | SLO/error-budget reference (§24 targets). | WS-C |
| `docs/migrations-reliability-phase10.md` | Migration/ops notes (không CSV migration). | WS-C |
| Reports: `plans/260815-2340-goclaw-repository-reliability/reports/phase10-{a,b,c}.md` | WS-C + controller. | — |

## Implementation steps

1. **WS-A (metrics/SLO/otel):** add otlpmetric deps → `slo.go` + tests → `otelmetrics_sink*
   ` (OTel sink nhận Snapshot → meter counters; latency → histogram) → config SLO/Alerts →
   `cmd/gateway_reliability_metrics{,_noop}.go` wire exporter + `go reliabilityFlushLoop`
   (5s flush → RegisterSink). Verify: `go build -tags otel ./...` + `go vet -tags otel ./...`
   + `go test ./internal/reliability/... ./internal/config/...`.
2. **WS-B (managed sweep + webhook):** wire sweep vào managed path (scout caller wireExtras/
   managed startup, guard `stores.Runs != nil`) → `bgalert/webhook.go` (sender) → extend
   `ReportProviderError` (đọc AlertingConfig từ deps). Test webhook sender. Verify:
   `go build ./...` (cast below), `go build -tags sqliteonly ./...`, `go test ./internal/bgalert/... ./cmd/...`.
3. **WS-C (docs/dashboards):** Grafana JSON + runbook + SLO doc + migration doc. KHÔNG code.
   Verify links/claims chống source (metric names khớp `otelmetrics_sink_otel.go`, config
   keys khớp `ReliabilityConfig`).
4. **Cleanup/verify pass:** controller grep Strategy 8 (symbol refs), Strategy 17 (verify pass),
   chạy `go build ./...` cả 2 build modes + `go vet ./...` + `go test -race ./tests/integration/`
   nếu PG sẵn sàng.

## Validation

- `go build ./...` (PG, non-otel) — noop sink path compile.
- `go build -tags otel ./...` — otel sink + otlpmetric compile (Docker gate).
- `go build -tags sqliteonly ./...` — desktop (Lite) compile.
- `go vet ./...` + `go vet -tags otel ./...`.
- `go test ./internal/reliability/... ./internal/config/... ./internal/bgalert/...
  ./cmd/...`.
- Manual (nếu PG): `goclaw health` hiện SLO status; gửi `/gc:*`; quan sát sweep logs
  `runs.stale_sweep_marked_failed`.
- Regression: `go test -race ./tests/integration/` (nếu PG sẵn sàng).

## Risks & rollback

- **OTel dep mới**: otlpmetric cần download module trong container (gomodcache volume giữ
  state). Rollback: revert go.mod/go.sum + files build-tag.
- **Build-tag split race**: một trong hai file (otel/noop) thiếu → build lỗi một mode.
  Rollback: xóa file sai; verify CẢ 2 mode.
- **Sweep trùng**: standard + managed cùng wire -> chạy 2 sweep. Đây là 2 process khác nhau
  (standard runGateway vs managed desktop) nên không trùng trong 1 tiến trình. Không issue.
- **Alert webhook noise**: best-effort + cooldown (min interval giữa 2 webhook gửi, default
  60s) để tránh spam. Config `alerts.min_interval_seconds`.
- **SLO window drift**: evaluator chỉ chạy khi có snapshot delta (không có requests = không
  tính) — tránh division-by-zero + false burn trên idle.

## Notes

- Metric names (contract cho WS-C dashboard): prefix `goclaw.reliability.` — xem
  `otelmetrics_sink_otel.go` để lấy tên chính xác. Dashboard dùng OTel datasource
  (OTel Collector → Prometheus remote-write hoặc Tempo/Grafana native).
- `goclaw health` CLI là surface "dashboards nhẹ" (in-process snapshot) — giữ nguyên, WS-A
  có thể bổ sung SLO status vào dump nếu rẻ.
- **Quyết định chốt (user):** (1) Prometheus sink + config-driven SLO, (2) OTel-metrics qua
  build-tag otel, (3) wire sweep managed path, (4) Webhook + bgalert. Lệnh user: "xong rồi
  làm nốt phase 10 nhé".