# Migration / Ops Notes — Phase 10 Production Hardening

## Tóm tắt

Phase 10 **KHÔNG có schema migration**. Toàn bộ thay đổi chỉ gồm: cấu hình (`reliability.slo.*`,
`reliability.alerts.*`), build-tag code (OTel metrics sink, `-tags otel`), và tài liệu vận hành.

Nguồn scope: `plans/260815-2340-goclaw-repository-reliability/phase-10-production-hardening.md:44-68`.

| Loại | Có trong Phase 10? | Ghi chú |
|------|--------------------|---------|
| PostgreSQL migration | Không | Không thêm file `migrations/*.sql`, không bump `RequiredSchemaVersion` |
| SQLite schema | Không | Không sửa `internal/store/sqlitestore/schema.sql` / `schema.go` |
| Config keys mới | Có | `reliability.slo.*`, `reliability.alerts.*` |
| Build-tag code | Có | `//go:build otel` / `!otel` cho metrics sink + wiring |
| Go dependency mới | Có (có điều kiện) | otlpmetric exporters (chỉ trong build-tag otel) |

---

## 1. KHÔNG có schema migration

Phase 10 là **config + build-tag code + docs**, không chạm database:

- Không thêm/hủy bảng, cột, index.
- `agent_runs`, `traces`, `system_configs` giữ nguyên schema hiện tại.
- Dự án tuân theo quy tắc dual-DB ("Always update both") nhưng điều đó chỉ áp dụng **khi có
  thay đổi schema** — Phase 10 không có, nên không cần chạm PG lẫn SQLite migration.

Nếu sau này cần theo dõi SLO bền vững hơn trong DB (không qua telemetry), đó là Phase khác.
Trong Phase 10, SLO status được tính bởi `internal/reliability/slo.go` (in-memory, window FIFO)
và export qua OTel — không lưu DB.

---

## 2. Config keys mới

Contract Phase 10 (`phase-10-production-hardening.md:44, 53`; implemented bởi WS-A trong
`internal/config/config.go` — block `ReliabilityConfig` hiện tại tại `:70-80`):

```json5
{
  reliability: {
    // SLO evaluator
    slo: {
      enabled: true,
      target_percent: 99,
      window_seconds: 3600
    },
    // Webhook alerts
    alerts: {
      enabled: true,
      webhook_url: "https://hooks.example.com/hooks/abc",
      min_interval_seconds: 60
    },
    // Các keys có sẵn từ Phase 3/4/5 (không đổi):
    runs: {
      heartbeat_interval_ms: 10000,
      stale_after_ms: 60000,
      sweep_interval_ms: 30000
    },
    circuit: {
      failure_threshold: 5,
      degraded_threshold: 2,
      cooldown_ms: 30000,
      half_open_max: 1,
      probe_timeout_ms: 30000
    },
    stream: {
      idle_timeout_ms: 60000,
      first_byte_timeout_ms: 0
    }
  }
}
```

Defaults hiện tại (`internal/config/config.go:130-148`):

| Key | Default |
|-----|---------|
| `reliability.runs.heartbeat_interval_ms` | 10000 |
| `reliability.runs.stale_after_ms` | 60000 |
| `reliability.runs.sweep_interval_ms` | 30000 |
| `reliability.circuit.failure_threshold` | 5 |
| `reliability.circuit.degraded_threshold` | 2 |
| `reliability.circuit.cooldown_ms` | 30000 |
| `reliability.circuit.half_open_max` | 1 |
| `reliability.circuit.probe_timeout_ms` | 30000 |
| `reliability.stream.idle_timeout_ms` | 60000 |
| `reliability.stream.first_byte_timeout_ms` | 0 |

> Xác nhận tên chính xác: WS-A report (`plans/.../reports/phase10-a.md`) là nguồn cuối.
> Theo mặc định phase file: `reliability.slo.{enabled,target_percent,window_seconds}` và
> `reliability.alerts.{enabled,webhook_url,min_interval_seconds}`.

---

## 3. Cấu hình telemetry (OTel metrics)

Metrics export được điều khiển bởi `telemetry.*` (sẵn có, `internal/config/config.go:706-714`):

```json5
{
  telemetry: {
    enabled: true,
    endpoint: "localhost:4317",        // OTLP gRPC
    // hoặc "https://otel.example.com:4318" với protocol: "http"
    protocol: "grpc",                   // "grpc" (default) | "http"
    insecure: true,                     // local/dev; prod nên false + TLS
    service_name: "goclaw-gateway",
    headers: {}                         // auth token nếu cloud backend
  }
}
```

- Metrics sink được compile **chỉ với build-tag `otel`**.
- Nếu build không có tag `otel`, `gateway_reliability_metrics_noop.go` dùng noop sink
  (`phase-10:87-91`) — binary không export nhưng vẫn hoạt động bình thường.

---

## 4. Dependency Go mới (có điều kiện) & build cả 2 mode

Phase 10 thêm dep OTel metrics (chỉ build-tag otel):

- `go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetricgrpc`
- `go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetrichttp`
- promote `go.opentelemetry.io/otel/sdk/metric` từ indirect → direct
  (`phase-10:32, 47-52, 77`).

**Sau khi WS-A thêm dep, bắt buộc:**

```bash
go mod tidy
go build ./...                 # PG, non-otel — noop sink path compile
go build -tags otel ./...      # OTel build-gate — otlpmetric compile
go build -tags sqliteonly ./... # desktop (Lite) compile
go vet ./...
go vet -tags otel ./...
go test ./internal/reliability/... ./internal/config/... ./internal/bgalert/... ./cmd/...
```

**Lưu ý:**

- `go mod tidy` phải chạy ở workspace root (không phải trong UI directory).
- Build-tag split race: mỗi cặp file otel/noop phải tồn tại đủ cả 2 — thiếu 1 file là build
  lỗi 1 mode. Rollback: xóa file sai, verify CẢ 2 mode (`phase-10:134-135`).
- Trong container build, gomodcache volume cần giữ state để tải modul mới (`phase-10:132-133`).
- Môi trường dev (máy này): CI làm, không cần chạy thủ công trừ khi được yêu cầu.

---

## 5. Ops changes

1. **Wire stale-run sweep vào managed/desktop path** (`cmd/gateway_managed.go`, guard
   `stores.Runs != nil`, dùng `EffectiveStaleAfter()/EffectiveSweepInterval()`) —
   `phase-10:66-68`. Trước Phase 10 chỉ standard path có sweep
   (`cmd/gateway_heartbeat.go:110-116`); manage/desktop không có periodic sweep
   (`phase-10:25`).
2. **Webhook alert trong bgalert** — `internal/bgalert/report.go` + `webhook.go` (WS-B).
   Cert provider errors `auth`/`billing`/`model_not_found` sẽ POST webhook
   (`internal/bgalert/report.go:36-41`, `phase-10:53-57`).
3. **Grafana dashboard** `deploy/grafana/goclaw-reliability.json` — import bằng tay hoặc qua
   provisioning (`docs/runbooks/reliability-ops.md` §4).

---

## 6. Rollback

| Thay đổi | Rollback |
|----------|----------|
| Config SLO/alerts | Xóa key khỏi `GOCLAW_CONFIG`, restart. Zero value = disable (`config.go:70-73`) |
| Metrics export | `telemetry.enabled: false` + restart; hoặc build binary không tag `otel` |
| Dep otlpmetric | Revert `go.mod`/`go.sum` via git; build 2 mode verify (`phase-10:132-134`) |
| Sweep managed path | Revert `gateway_managed.go` change (Phase trước chỉ standard-path sweep) |

Không có migration down nào được chạy — không có thay đổi schema.

---

## 7. Checklist finalize

- [ ] Build cả 3 mode: `go build ./...`, `go build -tags otel ./...`, `go build -tags sqliteonly ./...`
- [ ] `go vet ./...` + `go vet -tags otel ./...`
- [ ] Không có file migration mới / schema chạm vào
- [ ] Config keys khớp report WS-A (`phase10-a.md`)
- [ ] Fallback CI (`Makefile`) không bị lỗi tag otel
- [ ] Dashboard `deploy/grafana/goclaw-reliability.json` là JSON hợp lệ