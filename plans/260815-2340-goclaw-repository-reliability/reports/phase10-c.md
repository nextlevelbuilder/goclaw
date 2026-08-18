# Phase 10-C Report — Reliability Runbooks, SLO Reference, Migration/Ops Notes, Grafana Dashboard

## Scope

WS-C (docs-only, không chạm Go code). Tạo 4 file ownership độc quyền cho reliability
observability Phase 10.

## Files created

1. `docs/runbooks/reliability-ops.md` — Runbook vận hành (tiếng Việt): SLO/error budget §24,
   alerts config + webhook payload shape, dashboard import + ý nghĩa panel, 5 incident runbooks
   (429 storm, stream disconnect, stale run, trace stuck running, provider outage), checklist
   pre/post deploy OTel build, bảng tham chiếu source file:line.
2. `docs/runbooks/reliability-slo.md` — SLO/error-budget reference: bảng mục tiêu §24, công thức
   success rate / burn-rate từ counters, quy trình hiệu chỉnh target bằng production telemetry,
   bảng config keys + defaults.
3. `docs/migrations-reliability-phase10.md` — Migration/ops notes: KHÔNG có schema migration
   (Phase 10 = config + build-tag code + docs); config keys mới + defaults; telemetry config;
   build 3 mode checklist + rollback.
4. `deploy/grafana/goclaw-reliability.json` — Grafana dashboard JSON: 12 panels, UID
   `goclaw-reliability`, title "GoClaw Reliability", datasource variable `${datasource}`
   (prometheus/otel), schemaVersion 39 (v13+).

## Metric names dùng (contract WS-A prefix `goclaw.reliability.`)

OTel → Prometheus thường thành underscore + `_total`:

| Nghĩa | Counter (Prometheus dạng) |
|-------|----------------------------|
| Total LLM requests | `goclaw_reliability_llm_requests_total` |
| LLM successes | `goclaw_reliability_llm_successes_total` |
| Retries | `goclaw_reliability_llm_retries_total` |
| 429 / rate limited | `goclaw_reliability_llm_rate_limited_total` |
| 5xx server errors | `goclaw_reliability_llm_server_errors_total` |
| Timeouts | `goclaw_reliability_llm_timeouts_total` |
| Stream stalls | `goclaw_reliability_llm_stream_stalls_total` |
| Agent recovered | `goclaw_reliability_agent_recovered_total` |
| Agent continued | `goclaw_reliability_agent_continued_total` |
| Loop detected | `goclaw_reliability_loop_detected_total` |
| Latency (histogram-ish) | `goclaw_reliability_llm_latency_sum_total` / `_count_total` |

> Note: OTel counter convention là `<name>_total` trong Prometheus text format; nếu metric name
> WS-A đặt là `goclaw.reliability.agent.recovered` → Prometheus `goclaw_reliability_agent_recovered_total`.
> Dashboard docs đều note rằng tên chính xác cuối cùng là contract WS-A
> (`internal/reliability/otelmetrics_sink_otel.go`) và cần sync nếu đổi.

## Config keys dùng (default theo phase file)

- `reliability.slo.{enabled,target_percent,window_seconds}` (target 99, window 3600s)
- `reliability.alerts.{enabled,webhook_url,min_interval_seconds}` (min interval 60s)
- Có tham chiếu các key cũ: `reliability.runs.*`, `reliability.circuit.*`, `reliability.stream.*`
  (webhook payload: `{severity,title,message,timestamp,meta}` theo phase-10:55)

## Tham chiếu nguồn (đã verify trực tiếp source)

| Claim | Source |
|-------|--------|
| Snapshot counters + Sink/Flush | `internal/reliability/metrics.go:108-126, 163-233` |
| staleRecoveryLoop disabled + note | `internal/tracing/collector.go:166-186` (dòng 180 comment, 186 reachable) |
| ReliabilityConfig / RunsConfig / StreamConfig / CircuitConfig | `internal/config/config.go:70-149` |
| Defaults runs/circuit/stream | `config.go:130-148` |
| TelemetryConfig fields | `config.go:706-714` |
| Stale-run sweep + logs | `cmd/gateway_heartbeat.go:29-54`; wire standard `:110-116` |
| RecoverInterruptedRuns startup | `cmd/gateway_managed.go:215-221` |
| goclaw health dump + checks | `cmd/health_cmd.go:40-104, 124-174` |
| Circuit breaker defaults | `internal/reliability/circuitbreaker.go:61-69` |
| Rate-limit coordinator (429 cooldown 30s default) | `internal/reliability/ratelimit.go:42-50, 72` |
| Cooldown durations theo reason | `internal/providers/cooldown.go:32-42` |
| FailoverReason taxonomy | `internal/providers/error_classify.go:13-23` |
| health_order / MinAttemptsForHealth | `internal/providers/model_fallback.go:20-25, 278-283, 373-376` |
| bgalert alertable reasons (auth/billing/model_not_found) | `internal/bgalert/report.go:36-41, 45-54` |
| Sanitize message | `internal/bgalert/report.go:107-120` |
| §24 SLO targets | `plans/.../GoClaw_Upgrade_Improvement_Plan.md:1143-1158` |
| Phase 10 metric prefix + config contract | `plans/.../phase-10-production-hardening.md:44-52, 145-147` |

## Verify notes

- Grafana JSON: parse bằng PowerShell `ConvertFrom-Json` → **JSON VALID**, `title=GoClaw
  Reliability`, `uid=goclaw-reliability`, `schemaVersion=39`, 12 panels, **no gridPos overlaps**.
- Datasource variable type `datasource` (prometheus/otel), mọi panel `datasource: ${datasource}`.
- Không đụng file Go: chỉ tạo 4 file docs/JSON ở paths ownership WS-C.
- `deploy/` directory không tồn tại trước đó — tạo mới `deploy/grafana/goclaw-reliability.json`.
- Dùng PowerShell chỉ để validate JSON + check thư mục (không chạy build/test/commit; không git stash).

Status: DONE
Summary: Tạo 4 file WS-C (2 runbook docs tiếng Việt, migration/ops notes, Grafana dashboard JSON
12 panels) với file:line tham chiếu chống source; JSON đã validate hợp lệ, không overlap, không
đụng Go code.
Concerns/Blockers: Metric name PromQL trong dashboard dùng convention `_total` suffix dự kiến —
tên counter cuối cùng phụ thuộc contract WS-A (`otelmetrics_sink_otel.go`); docs đã ghi rõ cần
sync nếu WS-A đổi tên. Config keys theo mặc định phase file; xác nhận cuối khi WS-A report
(`phase10-a.md`) merge.