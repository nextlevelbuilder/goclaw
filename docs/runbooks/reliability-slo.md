# SLO / Error Budget Reference — Reliability Layer

## 1. SLO targets (§24)

Nguồn: `plans/260815-2340-goclaw-repository-reliability/GoClaw_Upgrade_Improvement_Plan.md:1143-1158`
(mục "# 24. Error Budget / Reliability SLO").

| SLO ID | Mục tiêu | Ý nghĩa | Counter đo lường |
|--------|----------|---------|------------------|
| SLO-1 | `successful runs >= 99%` | Tỷ lệ run LLM thành công / tổng request | `requests` / `successes` |
| SLO-2 | `provider-induced recovery success >= 95%` | Run được recovery thành công sau lỗi provider | `agent_recovered` / recovery attempts |
| SLO-3 | `false failure while backend still running = 0` | Không đánh dấu failed khi backend còn chạy | (kiểm tra thủ công / audit) |
| SLO-4 | `message duplication = 0` | Không gửi trùng message | (audit phía channel) |
| SLO-5 | `unhandled stream disconnect < 0.1%` | Stream disconnect không xử lý được | `stream_stalls` / `requests` |
| SLO-6 | `429-caused hard failures < 0.5%` | 429 gây fail run thực sự (sau retry/cooldown) | `rate_limited` dẫn đến fail |

Các con số này là **mục tiêu ban đầu** — plan yêu cầu hiệu chỉnh bằng production telemetry
(`GoClaw_Upgrade_Improvement_Plan.md:1158`): *"Các con số trên là mục tiêu ban đầu, cần hiệu
chỉnh bằng production telemetry."*

Phase 10 đưa SLO vào cấu hình động: `reliability.slo.{enabled,target_percent,window_seconds}`
(`phase-10-production-hardening.md:44, 75`). Evaluator chỉ tính khi có snapshot delta — không có
request là không tính, tránh division-by-zero trên idle (`phase-10:140-141`).

---

## 2. Counter nguồn (`goclaw.reliability.*`)

Tất cả counter đọc từ `reliability.Snapshot`
(`internal/reliability/metrics.go:108-126`, phương thức `Take()` `:129-149`).
WS-A export qua OTel metrics sink (build-tag `otel`) với prefix `goclaw.reliability.`
(`phase-10-production-hardening.md:145-147`).

| Snapshot field (Go) | Counter (OTel, prefix `goclaw.reliability.`) | Ghi chú |
|---------------------|-----------------------------------------------|---------|
| `LLMRequests` | `requests` | Total request |
| `LLMSuccesses` | `successes` | Total success |
| `LLMRetries` | `retries` | Retry count |
| `LLMRateLimited` | `rate_limited` | 429 |
| `LLMServerErrors` | `server_errors` | 5xx |
| `LLMTimeouts` | `timeouts` | Timeout |
| `LLMStreamStalls` | `stream_stalls` | Stream stall |
| `LLMLoop` | `loop_detected` | Loop force-stopped (counter `RecordLLMLoop`) |
| `LLMRepeatedToolCalls` | `repeated_tool_calls` | Tool-loop warning |
| `LLMEmptyOutputs` | `empty_outputs` | Empty/fallback output |
| `LLMPrematureCompletions` | `premature_completions` | Premature completion (`RecordLLMPrematureCompletion`) |
| `AgentRecovered` | `agent_recovered` | Run recovery (SLO-2) |
| `AgentContinued` | `agent_continued` | Continuation gate |
| `LLMLatencyNanos` / `LLMLatencyCount` | `llm_latency_ms` (histogram, buckets ms) | Avg latency / request |

> Tên counter là contract chốt trong `internal/reliability/otelmetrics_sink_otel.go` (Phase 10).
> Khi OTel → Prometheus (remote-write), counter thành `goclaw_reliability_<tên>_total`
> (underscores, suffix `_total`); histogram thành `goclaw_reliability_llm_latency_ms_sum` /
> `goclaw_reliability_llm_latency_ms_count`. Dashboard `deploy/grafana/goclaw-reliability.json`
> đã viết theo tên thật này.

---

## 3. Công thức tính success rate & error budget

### 3.1 Success rate (process-wide)

```
success_rate = (successes_total / requests_total) * 100
```

Ví dụ PromQL trên Prometheus:

```promql
sum(rate(goclaw_reliability_successes_total[5m]))
/
sum(rate(goclaw_reliability_requests_total[5m]))
* 100
```

### 3.2 Error budget

```
budget_remaining = 1 - (100 - success_rate_percent) / 100 * window  # tương đối
```

Với target 99% trên window 1h:
- Budget cả giờ = `1%` của requests (tức được phép fail 1% tổng request).
- Dùng tiêu: mỗi giờ được đốt 1% / 24 ≈ 0.0417%.
- Burn-rate = `(100 - observed) / (100 - target) = (100 - observed) / 1`.

Bảng burn-rate với target 99%:

| Observed success rate | Burn rate | Trạng thái |
|-----------------------|-----------|------------|
| >= 99.0% | <= 1.0 | Xanh (trong budget) |
| 98.0% | 2.0 | Vàng (cháy 2× dự định) — can thiệp |
| 97.0% | 3.0 | Đỏ (cháy 3×) — incident |
| < 90.0% | > 10.0 | Nghiêm trọng — SLO mất mà không upstream |

### 3.3 Các SLO đặc biệt

- **SLO-5 (stream disconnect):** `stream_stalls / requests`.
  Target `< 0.1%` → ngưỡng cảnh báo khi tỷ lệ vượt 0.1%. Trong PromQL:
  `sum(rate(goclaw_reliability_stream_stalls_total[1h])) / sum(rate(goclaw_reliability_requests_total[1h])) * 100 < 0.1`
- **SLO-6 (429 hard fail):** `rate_limited` dẫn tới fail run thực sự (chứ không phải mọi 429 —
  429 được xử lý bởi cooldown/retry không tính là hard failure). Counter `rate_limited` chỉ là
  biến phụ; hard-failure cần metric riêng hoặc audit `agent_runs.status`.

---

## 4. Khi nào hiệu chỉnh target bằng production telemetry

Quy trình hiệu chỉnh (theo §24, `GoClaw_Upgrade_Improvement_Plan.md:1158`):

1. **Thu thập ≥ 30 ngày telemetry** từ OTel sink / Prometheus.
2. Tính distribution thực tế của `success_rate`, `stream_stalls/requests`, `rate_limited`,
   `agent_recovered/recovery attempts`.
3. Đặt target tại **p90 của phân phối tốt**, không phải median:
   - If p90 `success_rate = 98.6%` → đặt target 98% (không quá chặt, không quá lỏng). 99% chỉ
     giữ nếu p95 ≥ 99.5%.
   - Nếu `false failure` hoặc `message duplication` xuất hiện → giữ target = 0, không nới lỏng.
4. Ghi lại lý do hiệu chỉnh trong journal/plan documents; cập nhật `reliability.slo.target_percent`.
5. Re-evaluate sau mỗi version release hoặc sau thay đổi provider lớn.

**Nguyên tắc giữ cứng (không hiệu chỉnh xuống bằng telemetry):**

- SLO-3 `false failure = 0` — đây là invariant chất lượng, không phải thống kê.
- SLO-4 `message duplication = 0` — tương tự, không dung sai.

---

## 5. Vị trí cấu hình và mặc định

| Key | Mặc định | Ghi chú |
|-----|----------|---------|
| `reliability.slo.enabled` | false | Bật evaluator SLO |
| `reliability.slo.target_percent` | 99 | Target success rate (%) |
| `reliability.slo.window_seconds` | 3600 | Window cảnh báo (1h) |
| `reliability.alerts.enabled` | false | Bật webhook alert |
| `reliability.alerts.webhook_url` | "" | URL nhận POST JSON |
| `reliability.alerts.min_interval_seconds` | 60 | Cooldown gửi webhook |

Config keys do WS-A thêm vào `ReliabilityConfig` (`internal/config/config.go:70-80`).
Xem chi tiết alert trong `docs/runbooks/reliability-ops.md` §3.

---

## 6. Tham chiếu

- §24: `plans/260815-2340-goclaw-repository-reliability/GoClaw_Upgrade_Improvement_Plan.md:1143-1158`
- Snapshot struct: `internal/reliability/metrics.go:108-126`
- Phase 10 contract metric prefix: `plans/260815-2340-goclaw-repository-reliability/phase-10-production-hardening.md:44-52, 145-147`
- Dashboard: `deploy/grafana/goclaw-reliability.json`