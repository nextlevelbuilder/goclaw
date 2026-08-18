# Phase 3 — AgentKit Native Layer

Scope: `§105` Phase 3 (Kit, Skill, Agent, Workflow, Hook, Artifact, Import/sync, Version lock). Backend-native duy nhất; Hook System reuse `internal/hooks/` (không làm mới).

## Context (scout 2026-08-18)

- Phase 1 `/gc:` Foundation SHIPPED: `internal/commands/gc/{parser,registry,executor}.go` (kinds plan/fix/cook/review), `internal/skills/loader.go` (extended metadata Inputs/Outputs/AllowedTools/QualityGates), kit `skills/{plan,fix,cook,review}/SKILL.md`.
- Phase 2 Durable Runtime SHIPPED: `agent_runs.checkpoint` JSONB (PG `000097` L16, SQLite `schema.sql:650`, patch 59) + `Loop.ResumeRun` + WS/HTTP resume.
- `internal/hooks/` đã có dispatcher/matcher/tracing/audit cho event bus — **không trùng** Phase 3 §48.
- `internal/tools/delegation_artifacts*.go` — artifact cho subagent đã có (file exchange), Phase 3 mở thêm store + version graph.
- Dual-DB: PG `migrations/` + `RequiredSchemaVersion` (`internal/upgrade/version.go`); SQLite `schema.sql` + `schema.go` migrations map + `SchemaVersion` const.
- No local Go → validate trong Docker (`golang:1.26.0`, mount `C:/Users/DORA/Downloads/goclaw-mod:/src`, volume `goclaw-gomodcache`).

## 4 Workstream (disjoint file ownership)

### WS-A — Workflow runtime (Go mới, no DB)

Files: `internal/workflow/{dag,graph,executor,types,parse}.go` + test.

- DAG: `Step{ID, Name, Type(seq/parallel/cond/retry), Run func(ctx, *RunCtx) error, Deps []string, While, Retry *RetryPolicy, Timeout, OnError, OnComplete}`.
- Executor: topo-sort + run dependencies; parallel fan-out (waitgroup); conditional gate; retry (max+backoff); timeout (context); on_error/on_complete hooks stub.
- Không lưu DB. Reuse `internal/orchestration/BatchQueue[T]` nếu phù hợp.
- Test: DAG topo, parallel, retry, timeout, conditional, cycle detection.
- KISS: workflow step = `func(ctx, *RunCtx) error` thay vì plugin registry.

### WS-B — Artifact store (DB)

Files: `internal/artifact/{type,metadata,version}.go` (structs + logic) + `internal/store/artifact_store.go` (interface) + `internal/store/pg/artifact.go` + `internal/store/sqlitestore/artifact.go` + `migrations/0000NN_artifacts.up/down.sql` + `internal/upgrade/version.go` (bump) + `internal/store/sqlitestore/schema.go` (patch) + `schema.sql` (fresh schema) + `internal/store/artifact_store_test.go`.

- Table `artifacts`: id (uuid PK), run_id, version int, author_agent, type varchar, status, checksum, parent_id (self-FK cho version graph), created_at, tenant_id.
- Interface: `CreateArtifact`, `GetArtifact`, `ListArtifacts(runID|tenant)`, `GetVersionGraph(parentID)`, `MarkStatus`.
- **Dual-DB lockstep MANDATORY**: PG migration mới + bump `RequiredSchemaVersion`; SQLite `schema.sql` (full) + `schema.go` migrations map (incremental patch) + bump `SchemaVersion`.
- Test: create/get/version-graph roundtrip PG + SQLite, tenant scope, checksum.
- KISS: 1 bảng, version = increment theo parent, không graph DB phức tạp.
- **Chú ý**: không đặt phase ID trong migration name hoặc comment; mô tả invariant/behavior.

### WS-C — Kit version + sync (loader)

Files: `internal/skills/kit_manager.go` + `internal/skills/kit_manager_test.go`. Không đụng `loader.go` trừ khi thật cần (nếu thêm hàm phải giữ signature cũ).

- Manifest: `skills/go-claw-engineer/kit.yaml` — name, version pin, slug set, checksum (SHA-256).
- `KitManager`: `Inspect(slug)`, `List()`, `Version()`, `VerifyChecksum() (bool, error)`, `RenderedManifest() string`, dry-run report.
- `kit.yaml` mới: ghi version hiện tại + checksum từ files hệ thống (không hardcode checksum — tính runtime).
- Test: parse manifest, checksum detect change, list/inspect.
- KISS: manifest đọc từ disk; không lock DB; không command `/gc:kit` (WS-D sẽ map nếu cần).

### WS-D — Skill expansion (`ui-ux-pro-max` + gc kinds)

Files: `internal/commands/gc/{parser,registry,executor}.go` + test + `skills/<new>/SKILL.md` mới.

- Skills mới trong kit engineer (mỗi skill 1 folder `skills/<name>/SKILL.md`, frontmatter metadata extended):
  - `ui-ux-pro-max` — UI/UX review & mobile-rule checklist (h-dvh, text-base md:text-sm, safe areas, touch targets, overflow-x-auto, dialogs, i18n).
  - `test` — test strategy + gates.
  - `debug` — bug investigation workflow.
  - `docs` — documentation management.
  - `architect` — architecture proposal.
  - (tối thiểu 4 skill ngoài ui-ux-pro-max nếu user yêu cầu "nhiều skill"; quyết định gent)
- Mở rộng parser: thêm kinds `debug`, `test`, `docs`, `architect`, `uiux` (→ registry map skill). Giữ kinds cũ (plan/fix/cook/review) + thêm vào `knownKinds` deterministic.
- Registry: map kind mới → skill slug. Executor: giữ nguyên (generic).
- Test: parse new kinds, executor resolves new skill content, khớp frontmatter.

## Cross-workstream contracts

- WS-D `parser.go` kinds + `registry.go` map — **WS-D sở hữu duy nhất**; WS-A/B/C không đụng.
- WS-B `internal/upgrade/version.go` — **WS-B sở hữu duy nhất** bump; WS-C không đụng (kit manifest đọc từ skills dir).
- WS-D `skills/` — **WS-D sở hữu duy nhất** folder mới; WS-C đọc `skills/go-claw-engineer/` (chỉ đọc, không ghi) cho manifest checksum. Conflict risk: WS-C ghi `kit.yaml` → WS-D thêm SKILLs → checksum thay đổi. **Giải quyết:** WS-C manifest tính checksum runtime (không hardcode SHA trong file committed) — WS-D thêm skill chỉ cập nhật list slug trong kit.yaml (WS-D sở hữu kit.yaml, WS-C chỉ đọc".

## Implementation steps

1. **WS-D scount trước** — xác định list skill cuối (scope), parser kinds, rồi 4 WS chạy song song.
2. Từng WS: implement → Docker gate (build/vet/unit) → guard/self-review.
3. Self review + controller review (4 PR).
4. Merge theo dependency: WS-C (kit.yaml read-only, không conflict) → WS-A/B/D (độc lập). Order cuối: **WS-A → WS-B → WS-D → WS-C** (hoặc theo tuyến CI).
5. Plan tick + report.

## Validation

- Mỗi WS: `go build ./...`, `go vet ./...`, `go test ./internal/<pkg>/` trong Docker.
- WS-B: `-tags sqliteonly` build + SQLite store test; PG unit test via testdb fixture.
- Integration nếu cần (WS-B artifact CRUD qua store interface).
- CI/CD: mỗi agent mở PR riêng, theo dõi `go`/`web`/`release-versioning` green.

## Risks & rollback

- **Conflict kit.yaml (WS-C vs WS-D)**: WS-C chỉ đọc manifest, checksum tính runtime; WS-D sở hữu ghi. Rollback: revert skill folder.
- **Migration (WS-B)**: thêm bảng artifacts — không đổi cột cũ; rollback = down migration.
- **Workflow runtime (WS-A)**: package mới, không touch existing — an toàn.
- **gc kinds mở rộng (WS-D)**: parser thêm knownKinds — giữ kinds cũ priority; test parse regression.

## Files mới tổng

- `internal/workflow/*.go` + test (WS-A)
- `internal/artifact/*.go`, `internal/store/{artifact_store,pg/artifact,sqlitestore/artifact}.go`, `migrations/0000NN_*` (WS-B)
- `internal/skills/{kit_manager.go,kit_manager_test.go}`, `skills/go-claw-engineer/kit.yaml` (WS-C)
- `internal/commands/gc/*` update + test, `skills/{ui-ux-pro-max,test,debug,docs,architect}/SKILL.md` (WS-D)

## Ownership

- WS-A: `internal/workflow/**`
- WS-B: `internal/artifact/**`, `internal/store/artifact_store.go`, `internal/store/pg/artifact.go`, `internal/store/sqlitestore/artifact.go`, `migrations/*artifacts*`, `internal/upgrade/version.go`, `internal/store/sqlitestore/schema.go`+`schema.sql` (chỉ artifact section)
- WS-C: `internal/skills/kit_manager.go`+test, `skills/go-claw-engineer/kit.yaml` (read + render)
- WS-D: `internal/commands/gc/**`, `skills/<new>/**`