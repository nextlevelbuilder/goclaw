# Controller Review — Phase 5 Multi-Agent (SHIPPED)

- Ngày: 2026-08-19
- Merge commits: PR #15 `0523bd11`, PR #16 `b878a174`, PR #17 `4c1a02f9`, PR #18 `c5c743c6`

## Tổng quan

Phase 5 (Multi-Agent) triển khai 4 khối năng lực theo vision `§26-28/§62-64`:

1. **Handoff contract** (WS-A, PR #15) — `internal/contract`: `Contract` type có task/context/constraints/artifacts/acceptance_criteria/deadline/budget, validator, `NewContract`/`RenderContract` helper.
2. **Competitive fan-out + judge + jury/consensus + negotiation runtime** (WS-A, PR #15) — `internal/orchestration`: parallel fan-out qua `DelegateRunFunc`, `Verdict` type, bounded negotiation (max 5 rounds, ≥2/3 consensus).
3. **Contract store** (WS-B, PR #16) — bảng `multi_agent_records` PG + SQLite, migration `000100`/`RequiredSchemaVersion` 100, SQLite patch 62→63/`SchemaVersion` 63.
4. **Jury/negotiate tools + dynamic formation + gateway wiring** (WS-C PR #17, WS-D PR #18) — `internal/tools/jury_tool.go` + `negotiate_tool.go` đăng ký gateway; `internal/teamworkclassify/formation.go` (4 formations deterministic); `internal/gateway/methods/jury.go` (`multiagent.formation/jury/negotiate`); protocol events + permissions + i18n.

## Import-cycle fix (WS-C)

Phát hiện cycle `tools → orchestration → pipeline → tools` do `internal/orchestration` import agent+pipeline qua các conversion funcs (`child_result.go`/`media_convert.go`). Controller verified 0 production callers của 4 conversion funcs → xóa, giữ nguyên pure `ChildResult` struct (chỉ import `time` + `bus`). Không vỡ public contract.

## Cross-PR dependency (WS-D)

`cmd/gateway.go` tham chiếu `pgStores.Contracts` (field thêm bởi WS-C). PR #18 ban đầu FAIL compile vì branch từ dev cũ. Fix: merge PR #17 trước → rebase WS-D lên dev mới (745d2548) → force-push → CI green (go/web/release-versioning) → squash merge `c5c743c6`.

## Verification (merged dev)

| Gate | Kết quả |
|------|---------|
| `go build ./...` | OK |
| `go vet ./...` | OK |
| `go build -tags sqliteonly ./...` | OK |
| unit: contract/orchestration/teamworkclassify/tools/gateway-methods | OK |
| PG store (`internal/store/pg`) | OK |
| SQLite store (`internal/store/sqlitestore`, -tags sqliteonly) | OK |
| integration (`tests/integration`, pgtest 5433) | OK (30.1s) |

## Acceptance criteria

10/10 ticked. Toàn bộ PR CI green trước merge. Regression xanh.

## Shares / lưu ý

- Không UI/web mới, không `/gc:team` command mới — giữ lightweight theo quyết định chốt.
- `MultiAgentFormationPayload.Override` dùng `json:"override,omitempty"` → test phải check `ok && v != false` thay vì so `!= false` trực tiếp (bool false bị omit).
- `internal/tools/jury_tool.go` / `negotiate_tool.go` dùng NUL-delimited `juryContestantTask(index, task)` marker cho multiple tasks từ một formation.
- Ví dụ formation: `debugger-only`, `planner+coder+tester`, `architect+...` — deterministic catalog `internal/teamworkclassify/formation.go`.

## Trạng thái

Status: DONE