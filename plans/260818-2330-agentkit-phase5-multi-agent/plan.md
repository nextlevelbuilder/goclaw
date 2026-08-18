# AgentKit Deep Integration — Phase 5 Multi-Agent

Nguồn vision: `plans/260815-2340-goclaw-repository-reliability/GoClaw_AgentKit_2026_Deep_Integration_Plan.md`
(`§26` dynamic teams; `§27` competitive parallel agents; `§28` agent jury; `§62` multi-agent negotiation; `§63` dynamic team formation; `§64` handoff contracts).

## Status

- **Phase 5 (Multi-Agent):** `[x]` — SHIPPED (2026-08-19, PR #15–18).
- Phase 1 `/gc:` Foundation `[x]` SHIPPED (PR #8). Phase 2 Durable Runtime `[x]` (PR #10). Phase 3 Native Layer `[x]` (PR #11-14). Phase 4 Reliability = trùng reliability plan `[x]` (không làm lại).

## Quyết định đã chốt (controller scout, 2026-08-18)

1. **Reuse hạ tầng hiện có**, không làm lại: delegate tool, subagent spawn, `ChildRunAdmission`, artifact exchange, team_tasks, teamworkclassify (one-hop routing), workflow DAG (Phase 3), artifact store (Phase 3).
2. **2-stage dispatch thay vì 4 song song** (do dependency compile):
   - **Stage 1** (song song): **WS-A** contract + orchestration runtime (Go thuần, no DB), **WS-B** contract store (body JSONB generic → không phụ thuộc Go types WS-A).
   - **Stage 2** (sau A+B merge): **WS-C** jury/negotiate tools (phụ thuộc contract types A + store interface B), **WS-D** formation routing + gateway wiring (phụ thuộc tools C + types A).
3. **Public contract mới cần chọn từ đầu**: plan §26-28/§62-64 là vision không có RPC/UI sẵn → Phase 5 định nghĩa mới.
4. **`internal/artifact` đã có `TypeReview`** — jury/consensus output dùng artifact `TypeReview`/`TypeReport` thay vì tạo type mới.
5. **Giữ nhẹ**: không UI/web mới, không `/gc:team` command mới (nếu cần thêm sau). Chỉ backend runtime + WS methods + protocol events + tools.
6. **Hook System (§48)** đã có trong Phase 3 quyết định — không làm.

## Phases

| Phase | Nội dung | Trạng thái |
|-------|----------|------------|
| 5 | Multi-Agent (handoff contract + competitive/jury + negotiation + dynamic formation) | `[x]` SHIPPED (PR #15-18) |

## Acceptance criteria (Phase 5)

- [x] **Handoff contract** (WS-A): `Contract` type có task/context/constraints/artifacts/acceptance_criteria/deadline/budget; validator; `NewContract`/`RenderContract` helper. Test pass.
- [x] **Competitive fan-out + judge** (WS-A): `parallel.go` — chạy N strategy song song qua `DelegateRunFunc`, thu `[]ChildResult`, judge chọn best theo scoring criteria (correctness/perf/complexity/security). Test pass.
- [x] **Jury/consensus** (WS-A): `Verdict` type (approve/reject/revise + score + reason + votes); consensus aggregation (≥2/3 match). Test pass.
- [x] **Negotiation** (WS-A): proposal/counter-proposal/critique/vote round model, bounded rounds (max 5), quit khi đạt consensus. Test pass.
- [x] **Contract store** (WS-B): bảng `multi_agent_records` (id/tenant_id/run_id/kind/body JSONB/status/created_at) PG+SQLite, migration `000100` + `RequiredSchemaVersion` 100 + SQLite patch 62→63 + `SchemaVersion` 63. Test roundtrip + tenant scope.
- [x] **Verdict/negotiate tools** (WS-C): tools `jury` + `negotiate` đăng ký gateway; chạy qua `teamworkclassify`/directive; test.
- [x] **Dynamic formation** (WS-D): teamworkclassify mở rộng `Mode`/`Result` thêm formation (debugger-only / planner+coder+tester / architect+... ); gateway wiring; protocol events. Test.
- [x] Dual-DB lockstep: PG migration + bump; SQLite schema.sql + patch + bump.
- [x] Mỗi workstream: test → tự review → PR riêng → CI green.
- [x] Regression: `go build ./...`, `go vet ./...`, `go build -tags sqliteonly ./...`, unit + integration xanh.

## Workstreams (disjoint, 2-stage)

| Stage | WS | Nội dung | Files (exclusive) | Kiểu |
|-------|----|----------|-------------------|------|
| 1 | **A** | Handoff contract + parallel fan-out/judge + verdict/negotiation runtime (Go thuần, no DB) | `internal/contract/**` (mới) + `internal/orchestration/{parallel,verdict,negotiate}.go` + `internal/orchestration/*_test.go` + `internal/workflow/agents.go` | Go mới |
| 1 | **B** | Contract store — durable persist cho multi-agent records | `internal/store/contract_store.go` (interface) + `internal/store/pg/contract.go` + `internal/store/sqlitestore/contract.go` + `migrations/000100_*.up/down.sql` + `internal/upgrade/version.go` + `schema.go` + `schema.sql` + `*_test.go` | DB |
| 2 | **C** | Tools `jury` + `negotiate` — execution surfaces | `internal/tools/jury_tool.go` + `internal/tools/negotiate_tool.go` + `*_test.go` + `cmd/gateway_managed.go` (register) | Tools |
| 2 | **D** | Formation routing + gateway wiring + protocol | `internal/teamworkclassify/formation.go` + `internal/teamworkclassify/*_test.go` + `internal/gateway/methods/jury.go` + `pkg/protocol/*.go` (events) | Routing + protocol |

## Execution detail

- Phase file: [`phase-05-multi-agent.md`](phase-05-multi-agent.md)
- Reports: [`reports/`](reports/)