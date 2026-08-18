# Phase 5 — AgentKit Multi-Agent

Scope: `§26` dynamic teams, `§27` competitive parallel agents, `§28` agent jury, `§62` multi-agent negotiation, `§63` dynamic team formation, `§64` handoff contracts. Reuse hạ tầng hiện có; làm mới orchestration runtime + contract store + tools + formation routing.

## Context (scout 2026-08-18)

- **Đã có (reuse, read-only):**
  - `internal/tools/delegate_tool.go` — `DelegateRequest`/`DelegateResult`/`DelegateRunFunc` (injected bởi gateway `cmd/gateway_managed.go:463-504`), dual mode async/sync, artifact exchange staging, durable async ledger `SubagentTaskStore`.
  - `internal/childrun/admission.go` — `ChildRunAdmission` (processLimit/pendingLimit), `ChildRunConstraints{TenantID, RootAgentID, RootLimit, TaskID, ParentTaskID, ParentFanout, Depth, MaxDepth}`, `Lease.Continue()` depth-64 continuation. **Fan-out phải reuse root+parent constraints, không spawn ticket độc lập.**
  - `internal/orchestration/child_result.go` — `ChildResult{Content, Media, InputTokens, OutputTokens, Runtime, Iterations, Status}` + converters từ v2 `agent.RunResult` / v3 `pipeline.RunResult`. Đây là canonical child outcome cho aggregation.
  - `internal/orchestration/batch_queue.go` — `BatchQueue[T]` generic (Enqueue/Drain/TryFinish).
  - `internal/tools/delegation_artifacts*.go` — secure workspace exchange `<tenant-workspace>/collaboration/delegations/<id>/{inputs,outputs}`, SHA-256 manifest, staged/published.
  - `internal/tools/team_tasks_*.go` — 14 actions team task tool. **Lưu ý: reviewer path CHƯA active** (`team_tasks_lifecycle.go` comment "reviewer role not yet active").
  - `internal/teamworkclassify/{classifier,context}.go` — self vs team routing: embedding cosine + `ClassifyWithLLM` LLM arbiter, permission gate lead/member, `BuildInputFromStores` (agents/teams/agent_links store). **Chỉ one-hop routing, chưa có composition.**
  - `internal/workflow/*` (Phase 3) — `DAG/Step{sequential,parallel,conditional,retry}/RunCtx`, `Spec/ParseSpec`. **Chưa được agent loop dùng** — integration point SDK.
  - `internal/artifact/types.go` (Phase 3) — `TypeReview`/`TypeReport`/`TypeArchitecture`/`TypeADR` + version graph store.
  - `internal/agent/orchestration_mode.go` — `OrchestrationMode{spawn,delegate,team}`, `ResolveOrchestrationMode` (priority team > delegate > spawn).
  - `internal/agent/team_work_directive.go` — `TeamWorkDirective` force required tool + retry.
- **Gap (Phase 5 làm):**
  - Không có jury/verdict/consensus type anywhere (`grep jury|consensus|competitive|negotiation` chỉ thấy plan + skill docs).
  - Không có dynamic team formation (chỉ one-hop self-vs-team).
  - Không có structured handoff contract — `DelegateRequest` flat, task free-text.
  - Không có competitive parallel + judge/merge.
  - Không có negotiation (proposal/counter/vote/consensus).
  - Không có DB cho multi-agent records aggregation.
- **Risks đã xác định:**
  - `ChildRunAdmission` global limit (pendingLimit default 128) — fan-out phải scoped.
  - Publication ownership asymmetry: `ApplyDelegationArtifactResultPolicy` strip media inside artifact runs — jury cần media phải thread qua exchange.
  - `delegate` tool refuse async mode inside artifact run (validateDelegationChildRunMode requires sync) — orchestrator phải là top-level run.
  - Team workspace contract DB + tool coupled (`internal/tools/context_keys.go`) — **KHÔNG touch existing keys**; chỉ thêm key mới.
- **Migration context:** latest PG migration = `000099_artifacts`. `RequiredSchemaVersion uint = 99`. SQLite `SchemaVersion = 62`, last patch key `61:` → 62. Phase 5 mới = `000100_*`, `RequiredSchemaVersion` 100, SQLite patch `62:` → `SchemaVersion` 63.

## 4 Workstream (disjoint, 2-stage dispatch)

### STAGE 1 (song song, độc lập compile)

#### WS-A — Contract + orchestration runtime (Go thuần, NO DB)

Files:
- `internal/contract/contract.go` (mới, package `contract`): `Contract` struct + `ContractKind` consts + `Validate()` + helpers.
- `internal/contract/*_test.go` (mới).
- `internal/orchestration/parallel.go` (mới): parallel fan-out chạy N strategy qua injected runner, thu `[]ChildResult`, judge chọn best.
- `internal/orchestration/verdict.go` (mới): `Verdict` type + consensus aggregation.
- `internal/orchestration/negotiate.go` (mới): proposal/counter-proposal/critique/vote round model, bounded.
- `internal/orchestration/*_test.go` (mới).
- `internal/workflow/agents.go` (mới): `StepParallel` integrations cho agent rounds (reuse Phase 3 DAG).

Contract design (kế thừa pattern `internal/artifact/types.go`):
```go
type ContractKind string
const (
  ContractHandoff ContractKind = "handoff"
  ContractJury    ContractKind = "jury"
  ContractCompetition ContractKind = "competition"
  ContractNegotiation ContractKind = "negotiation"
)

type Contract struct {
  ID           string            `json:"id"`
  Kind         ContractKind      `json:"kind"`
  Task         string            `json:"task"`
  Context      string            `json:"context,omitempty"`
  Constraints  []string          `json:"constraints,omitempty"`
  Artifacts    []string          `json:"artifacts,omitempty"` // relative paths hoặc artifact IDs
  Acceptance   []string          `json:"acceptance_criteria,omitempty"`
  Deadline     *time.Time        `json:"deadline,omitempty"`
  Budget       *ContractBudget   `json:"budget,omitempty"`
  AuthorAgent  string            `json:"author_agent,omitempty"`
  Verdicts     []Verdict         `json:"-"` // không ser, aggregation runtime-only
}

type ContractBudget struct {
  MaxCost     *float64 `json:"max_cost,omitempty"`
  MaxDuration *string  `json:"max_duration,omitempty"` // "15m"
  MaxToolCalls *int    `json:"max_tool_calls,omitempty"`
}
```

Verdict (trong `internal/orchestration` để tránh cycle? — quyết định: đặt verdict trong `internal/contract` vì contract dùng verdict; orchestration import contract):
```go
type Verdict struct {
  ContenderID string  `json:"contender_id"`
  Decision    string  `json:"decision"` // approve|reject|revise
  Score       float64 `json:"score"`
  Reason      string  `json:"reason"`
  Votes       int     `json:"votes"`
  JudgeAgent  string  `json:"judge_agent,omitempty"`
}
```

**Cross-stream contract cho Stage 2:**
- `contract.Validate()` trả `error` — kind required, task required.
- `orchestration.RunParallel(ctx, items []ParallelItem, runner RunFunc) ([]ChildResult, error)` — runner signature reuse `DelegateRunFunc` shape.
- `orchestration.Judge(ctx, results []ChildResult, criteria []string) (Verdict, error)`.

**Quy tắc:**
- Không import package `store` trong `internal/contract` (giữ pure domain, khớp `internal/artifact` pattern).
- Judge/verdict metrics: không benchmark/load tests.

#### WS-B — Contract store (DB, dual-DB lockstep)

Files:
- `internal/store/contract_store.go` (mới, package `store`): consts `ContractKind*/ContractRecordStatus*`, `ContractRecord` struct, `ContractStore` interface, helpers.
- `internal/store/pg/contract.go` (mới).
- `internal/store/pg/contract_test.go` (mới).
- `internal/store/sqlitestore/contract.go` (mới).
- `internal/store/sqlitestore/contract_test.go` (mới).
- `migrations/000100_multi_agent_records.up.sql` + `.down.sql` (mới).
- `internal/upgrade/version.go` — `RequiredSchemaVersion uint = 100`.
- `internal/store/sqlitestore/schema.go` — thêm patch `62:` (comment "Version 62 → 63: multi-agent records") + `SchemaVersion = 63`.
- `internal/store/sqlitestore/schema.sql` — append bảng `multi_agent_records`.

Table design:
```sql
CREATE TABLE multi_agent_records (
  id         UUID PRIMARY KEY DEFAULT uuid_generate_v7(),
  tenant_id  UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
  run_id     TEXT,
  kind       VARCHAR(40) NOT NULL,          -- handoff|jury|competition|negotiation
  body       JSONB NOT NULL DEFAULT '{}',    -- contract + verdicts (full payload)
  status     VARCHAR(40) NOT NULL DEFAULT 'draft', -- draft|active|closed
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX idx_multi_agent_records_tenant_created ON multi_agent_records(tenant_id, created_at DESC);
CREATE INDEX idx_multi_agent_records_run ON multi_agent_records(run_id);
CREATE INDEX idx_multi_agent_records_tenant_kind ON multi_agent_records(tenant_id, kind);
```

Interface:
```go
type ContractStore interface {
  CreateContractRecord(ctx context.Context, rec *ContractRecord) error
  GetContractRecord(ctx context.Context, id uuid.UUID) (*ContractRecord, error)
  ListContractRecords(ctx context.Context, opts ContractRecordListOpts) ([]ContractRecord, error)
  UpdateContractRecordStatus(ctx context.Context, id uuid.UUID, status string) error
}
```
`ContractRecordListOpts{RunID, Kind, Status, Limit, Offset}` — tenant-scoped qua context (khớp `ArtifactListOpts` pattern).

**Cross-stream contract cho Stage 2:**
- `store.ContractRecord` public struct + `store.ContractStore` interface — WS-C tools gọi qua store.
- `Body string` (JSON-encoded) — WS-C marshal `contract.Contract` vào body. WS-B không import `internal/contract`.

**Quy tắc:**
- JSONB cho PG, TEXT cho SQLite body. Tenant-scope fail-closed (`WHERE 1=0` khi không tenant ctx — khớp `pg/artifact.go`).
- Migration tên không chứa phase ID.
- **Không đụng** `internal/store/subagent_store.go`/`artifact_store.go`.

### STAGE 2 (sau Stage 1 merge)

#### WS-C — Tools jury + negotiate (execution surfaces)

Files:
- `internal/tools/jury_tool.go` (mới): tool `jury` — nhận `contract` JSON + criteria, chạy parallel contend (reuse `DelegateRunFunc` static), aggregate verdict, persist qua `ContractStore`, trả `Verdict`.
- `internal/tools/negotiate_tool.go` (mới): tool `negotiate` — proposal/counter-proposal round model, bounded 5 rounds, persist.
- `internal/tools/jury_tool_test.go` + `negotiate_tool_test.go` (mới).
- `cmd/gateway_managed.go` — register 2 tools (chỉ thêm Register call, không đụng delegateRunFn).

**Quy tắc:**
- Tools đọc stores: `ContractStore`, `ArtifactStore` (persist `TypeReview` artifact khi verdict), agent loop hooks (`loop` interface).
- Không touch `internal/tools/delegate_tool.go`, `internal/tools/context_keys.go`, `internal/tools/team_tasks_*.go`.
- Lite edition: nếu gating, mở rộng `TeamActionPolicy` — nhưng `jury`/`negotiate` mới không cần lite gate (không ghi vào team task).

#### WS-D — Formation routing + gateway wiring + protocol

Files:
- `internal/teamworkclassify/formation.go` (mới): mở rộng `Mode`/`Result` — thêm formation schema `Formation{Agents []string, Pipeline []string}`; hàm `SelectFormation(ctx, task, complexity)`.
- `internal/teamworkclassify/formation_test.go` (mới).
- `internal/gateway/methods/jury.go` (mới): WS method `multiagent.jury`/`multiagent.negotiate` (nếu WS-C tools cần protocol).

Đọc trước: `internal/teamworkclassify/{classifier,context}.go` để giữ signature cũ (Race: WS-D sở hữu `formation.go` mới, KHÔNG sửa `classifier.go` enum hiện có — thêm giá trị mới qua mở rộng struct).

## Cross-workstream contracts

- **WS-A** sở hữu `internal/contract/**` + `internal/orchestration/{parallel,verdict,negotiate}.go` + `internal/workflow/agents.go`.
- **WS-B** sở hữu `internal/store/contract_store.go` + `internal/store/{pg,sqlitestore}/contract*.go` + `migrations/000100_*` + `internal/upgrade/version.go` + `schema.go` + `schema.sql` (chỉ contract section). **WS-B sở hữu duy nhất `version.go`** (khớp Phase 3).
- **WS-C** sở hữu `internal/tools/{jury_tool,negotiate_tool}.go` + test + `cmd/gateway_managed.go` (chỉ add Register).
- **WS-D** sở hữu `internal/teamworkclassify/formation.go` + test + `internal/gateway/methods/jury.go` + `pkg/protocol/*.go` (events).
- WS-C/WS-D **đọc** `internal/contract` + `internal/store/contract_store.go` — KHÔNG ghi.
- WS-D mở rộng `teamworkclassify` — KHÔNG sửa `classifier.go`/`context.go` existing (WS-D chỉ thêm file mới `formation.go`).

## Implementation steps

1. **Stage 1 dispatch**: WS-A + WS-B song song (disjoint files, tránh edit cùng file).
2. Stage 1 mỗi WS: implement → Docker gate (build/vet/unit) → self-review → PR.
3. Controller review Stage 1 (2 PR) → merge (dependency: WS-A rồi WS-B, hoặc độc lập theo tuyến — WS-B không import WS-A nên merge order linh hoạt).
4. **Stage 2 dispatch**: WS-C + WS-D song song (sau khi A+B merged vào dev).
5. Stage 2 mỗi WS: implement → Docker gate → self-review → PR.
6. Controller review Stage 2 (2 PR) → merge.
7. Final verify: full `go build ./...` + `go vet ./...` + `-tags sqliteonly` + unit + integration.
8. Plan tick + report.

## Validation

- Mỗi WS: `go build ./...`, `go vet ./...`, `go test ./internal/<pkg>/` trong Docker (`golang:1.26.0`, mount `C:/Users/DORA/Downloads/goclaw-mod:/src`, volume `goclaw-gomodcache`).
- WS-B: `-tags sqliteonly` build + SQLite store test + PG test (testdb fixture `hooksTestDB` trên port 5433).
- Integration: WS-C verifies run qua `DelegateRunFunc` mocked runner.
- CI/CD: mỗi workstream PR riêng, theo dõi `go`/`web`/`release-versioning` green.

## Risks & rollback

- **ChildRunAdmission limit**: fan-out dùng root+parent constraints, không spawn độc lập. Rollback: giảm fan-out.
- **Reviewer path dormant** (team_tasks): WS-C không activation team_tasks reviewer — dùng jury riêng, không đụng.
- **Delegate async trong artifact run**: orchestrator phải top-level run; jury tool dùng sync mode.
- **Migration**: thêm bảng mới multi_agent_records — không đổi cột cũ; rollback = down migration.
- **Context keys**: không touch existing `internal/tools/context_keys.go`; WS-D thêm key mới nếu cần (formation directive).
- **i18n**: nếu tool error message user-facing → thêm key `internal/i18n/keys.go` + 3 catalogs (en/vi/zh).

## Ownership

- WS-A: `internal/contract/**`, `internal/orchestration/{parallel,verdict,negotiate}.go`, `internal/workflow/agents.go`
- WS-B: `internal/store/contract_store.go`, `internal/store/pg/contract*.go`, `internal/store/sqlitestore/contract*.go`, `migrations/000100_*`, `internal/upgrade/version.go`, `internal/store/sqlitestore/{schema.go,schema.sql}` (chỉ contract section)
- WS-C: `internal/tools/{jury_tool,negotiate_tool}.go`, `cmd/gateway_managed.go` (add Register)
- WS-D: `internal/teamworkclassify/formation.go`, `internal/gateway/methods/jury.go`, `pkg/protocol/*.go`