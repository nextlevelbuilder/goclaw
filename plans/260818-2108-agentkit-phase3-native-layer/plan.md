# AgentKit Deep Integration — Phase 3 Native Layer

Nguồn vision: `plans/260815-2340-goclaw-repository-reliability/GoClaw_AgentKit_2026_Deep_Integration_Plan.md`
(`§104` acceptance; `§105` Phase 3: Kit / Skill / Agent / Workflow / Hook / Artifact / Import-sync / Version lock).

## Status

- **Phase 3 (Native Layer):** `[x]` SHIPPED (2026-08-18) — PR #11/#12/#13/#14 merged to dev (9b085803).
- Phase 1 `/gc:` Foundation: `[x]` SHIPPED (PR #8). Phase 2 Durable Runtime: `[x]` SHIPPED (PR #10).
- Phase 4 Reliability: trùng reliability plan — không làm lại. Phase 5-7: deferred.

## Quyết định đã chốt (user, 2026-08-18)

1. **Full 4 workstream song song** — disjoint file ownership, tránh parallel edits cùng file (orchestration-protocol).
2. **Mỗi agent tự**: implement → test (Docker gate) → tự code-review (`/code-review`) → commit + push branch riêng → mở PR → **theo dõi CI/CD phần mình** → báo controller.
3. **Controller (em)**: review từng PR diff, resolve conflicts, merge tuần tự vào dev theo dependency order.
4. **Bổ sung skill mới** (WS-D): `ui-ux-pro-max` + nhiều skill trong kit engineer.
5. **Hook System** (§48) KHÔNG làm — `internal/hooks/` đã tồn tại cho event bus. Phase 3 chỉ thêm runtime → agent-loop hooks sau.
6. **Artifact Giữ nhẹ**: artifact metadata (types) + version graph + persist PG/SQLite. Không UX mới.

## Phases

| Phase | Nội dung | Files chính | Deps | Trạng thái |
|-------|----------|-------------|------|------------|
| 3 | Native Layer (Workflow runtime + Artifact store + Kit version/sync + Skill expansion) | `internal/workflow/`, `internal/artifact/`, `internal/skills/`, `internal/commands/gc/`, `skills/` | Phase 1, 2 | `[x]` SHIPPED |

## Acceptance criteria (Phase 3)

- [x] **Workflow runtime** (WS-A): DAG steps (sequential/parallel/conditional/retry/timeout), chạy trong GoClaw native; test pass.
- [x] **Artifact store** (WS-B): artifact types + metadata (id/run_id/version/author/hierarchy/checksum/status) + version graph + PG/SQLite persist; migration + version bump lockstep.
- [x] **Kit version + sync** (WS-C): version pin + checksum cho kit; `kit.yaml` (manifest); inspect/list/dry-run.
- [x] **Skill expansion** (WS-D): `ui-ux-pro-max` + ≥4 skill mới trong kit engineer; `/gc:` kinds mở rộng (map skill mới).
- [x] Dual-DB lockstep: PG migration `000099_artifacts` + `RequiredSchemaVersion` 99; SQLite `schema.sql` + `schema.go` patch 61→62 + `SchemaVersion` 62.
- [x] Mỗi agent: test → tự code-review → PR riêng → CI green cho phần mình (4/4 PR 3/3 green).
- [x] Regression: `go build ./...`, `go vet ./...`, `go build -tags sqliteonly ./...`, unit + integration tests xanh (CI go job 7m23s pass).

## Workstreams (disjoint, parallel)

| WS | Nội dung | Files (exclusive) | Kiểu |
|----|----------|-------------------|------|
| **A** | Workflow runtime — DAG executor (sequential/parallel/conditional/retry/timeout/rollback-compensation-stub) | `internal/workflow/**` mới + `internal/workflow/**/*_test.go` | Go mới |
| **B** | Artifact store — types + metadata + version graph + PG/SQLite | `internal/artifact/**` mới + `internal/store/pg/artifact*.go` + `internal/store/sqlitestore/artifact*.go` + `internal/store/artifact_store.go` + `migrations/0000NN_*.up/down.sql` + `internal/upgrade/version.go` + `internal/store/sqlitestore/schema.go` + `schema.sql` | DB |
| **C** | Kit version + sync — `kit.yaml` manifest, version pin, checksum, inspect/list | `internal/skills/kit_manager.go` mới + `internal/skills/kit_manager_test.go` | Loader |
| **D** | Skill expansion — `ui-ux-pro-max` + ≥4 skills + gc kinds mở rộng | `internal/commands/gc/{parser,registry,executor}.go` + `internal/commands/gc/*_test.go` + `skills/**/SKILL.md` mới | GC + skills |

## Execution detail

- Phase file: [`phase-03-native-layer.md`](phase-03-native-layer.md)
- Reports: [`reports/`](reports/)