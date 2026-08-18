# WS-C Report — Jury + Negotiate Tools (Execution Surfaces)

## Scope

Implemented the two execution surfaces that connect the Stage 1 contract types
(WS-A `internal/contract` + `internal/orchestration`) and the durable contract
store (WS-B `store.ContractStore`/`ArtifactStore`) into the agent loop:
`jury` (competitive fan-out + judge) and `negotiate` (bounded proposal/vote
round). Both are `tools.Tool` implementations registered in
`cmd/gateway_managed.go`, sharing the gateway's injected `DelegateRunFunc`.

## Files Modified/Created

| File | Action | Notes |
|------|--------|-------|
| `internal/tools/jury_tool.go` | created | `JuryTool`: parse task/strategies/criteria → fan-out via injected `DelegateRunFunc` → judge (default content heuristic, pluggable via `SetJudge`) → persist competition record + optional `TypeReview` artifact |
| `internal/tools/negotiate_tool.go` | created | `NegotiateTool`: bounded proposal/vote round model, 2/3 consensus check after every round, persist closed-on-consensus/exhausted or draft record |
| `internal/tools/jury_tool_test.go` | created | 5 tests (in-memory `ContractStore`/`ArtifactStore` fixtures + fixed/mocked delegate runner) |
| `internal/tools/negotiate_tool_test.go` | created | 4 tests (shares `testContractStore` fixture) |
| `cmd/gateway_managed.go` | modified | Inside the existing `stores.AgentLinks != nil && stores.Agents != nil` block, after `delegateTool` wiring: `tools.NewJuryTool(delegateRunFn, stores.Contracts, stores.Artifacts)` + `tools.NewNegotiateTool(stores.Contracts)` + 2 `toolsReg.Register` calls. `delegateRunFn` and delegateTool wiring untouched. |
| `internal/store/stores.go` | modified | Added `Contracts ContractStore` + `Artifacts ArtifactStore` fields to `Stores` (authorized: WS-B deferred factory wiring; task allowed it if trivial) |
| `internal/store/pg/factory.go` | modified | Added `Contracts: NewPGContractStore(db)` + `Artifacts: NewPGArtifactStore(db)` |
| `internal/store/sqlitestore/factory.go` | modified | Added `Contracts: NewSQLiteContractStore(db)` + `Artifacts: NewSQLiteArtifactStore(db)` |

## Design

- **Delegate routing:** the gateway's `delegateRunFn` requires `ToAgentKey`
  (it looks the target up via `agentRouter.Get`). The jury tool supports a
  shared `agent` or a per-strategy `agents` list. Because
  `orchestration.RunParallel` hands each worker only a `task string`, each
  contestant's `Task` carries a NUL-delimited index marker
  (`"%d\x00%s"`); the runner decodes the marker, picks the matching target, and
  sets `req.ToAgentKey` + the stripped `req.Task`. `parseJuryContestantTask`
  validates the marker so an absent/invalid marker fails closed.
- **Authorization scope preserved:** `baseReq` captures `FromAgentID`,
  `FromAgentKey`, `UserID`, `SenderID`, `Role`, `TenantID`, `Channel`,
  `ChannelType`, `ChatID`, `PeerKind`, `SessionKey`, `OriginTraceID` from the
  context once and copies them onto every contender request.
- **Judging:** default `judgeRound` delegates to `orchestration.Judge` with a
  label-aware scorer set (`correctness` rewards substantive output, `simplest`
  rewards brevity, `performance`/`safest` fall back to correctness). A
  custom judge can be injected via `SetJudge(JuryFunc)` without coupling this
  package to providers.
- **Persistence:** `Contract.Verdicts` carries `json:"-"`, so both tools embed
  `"verdicts"` explicitly in the durable body alongside the contract. The jury
  record is always `ContractRecordClosed`; on `approve` + a configured artifact
  store, a `TypeReview`/`ArtifactStatusFinal` artifact is persisted (failures
  logged, never fatal). The negotiate record is `Closed` when consensus locks
  or the round bound is exhausted, else `Draft`.
- **Negotiation round model:** proposals are submitted one per round with the
  i-th caller-supplied vote attached, and `ReachedConsensus(2/3)` is checked
  after every round so the negotiation closes as soon as agreement locks,
  never exceeding the bound. Surplus votes are attached in a second loop within
  the same bound. All stores are nil-safe: with nil stores the round still runs
  and returns its outcome, persistence is just skipped.
- **Fan-out safety:** `RunParallel` gives bounded concurrency, FailFast, and
  index-aligned results; failed contenders score 0 and cannot be approved.

## Tests (9 total)

`internal/tools/jury_tool_test.go`:
1. `TestJuryTool_RunsFanOutAndApprove` — 2 contenders, verbose content both →
   approve; persisted closed `competition` record; exactly 1 review artifact
   with type `review` and status `final`.
2. `TestJuryTool_RejectsWhenNoContenderScores` — empty content → reject verdict,
   closed record, no artifact.
3. `TestJuryTool_ReturnsErrorWithoutTask` — missing task; nil delegate runner
   fails closed with no persisted record.
4. `TestJuryTool_ScoringRespectsLabels` — criterion `simplest` picks the
   concise contender-0 over verbose contender-1.
5. `TestJuryTool_CustomJudge` — injected judge forces contender-1 approve,
   overriding the default heuristic.

`internal/tools/negotiate_tool_test.go`:
1. `TestNegotiateTool_ConsensusClosesRecord` — 2 proposals + approve votes →
   consensus at round 1, closed record, 1 verdict persisted in body.
2. `TestNegotiateTool_BoundedRoundsClosesExhausted` — maxRounds 2 with 4
   proposals → rounds 2, closed (exhausted), exactly 2 proposals persisted.
3. `TestNegotiateTool_NoConsensusStaysDraft` — 1 proposal, no votes,
   unexhausted → draft record.
4. `TestNegotiateTool_ValidationErrors` — missing task; malformed proposals;
   no persisted record for invalid execution.

Fixtures: in-memory `testContractStore`/`testArtifactStore` implementing the
full `store.ContractStore`/`store.ArtifactStore` interfaces (no DB needed),
plus `fixedDelegateRunner` for deterministic fan-out content.

## i18n Decision

No i18n keys added. Tool error messages are agent-loop-internal (rendered into
LLM context, not shown to end users), matching the existing `delegate_tool.go`
precedent which returns plain-English `ErrorResult(...)` strings. Per the phase
file risk note, i18n keys are only required when a tool error is
user-facing — neither jury nor negotiate messages are.

## Safety Constraints

- Did NOT touch `internal/contract/**`, `internal/orchestration/**`,
  `internal/gateway/**`, `pkg/protocol/**`, `internal/store/**` (aside from the
  explicitly authorized `stores.go` + both `factory.go` additions),
  `internal/tools/delegate_tool.go`, `internal/tools/context_keys.go`, or
  `internal/tools/team_tasks_*.go`.
- No import cycle: `tools → orchestration → contract` and `tools → tracing →
  store` are acyclic; the store layer never imports `internal/contract`.
- No phase IDs in code comments; comments describe behavior.
- No benchmark/load tests.
- `defaultJuryMatch`/`defaultJuryConcurrency`/`consensusMatch` package-level
  consts verified unique within `package tools` (no collision with
  `delegate_tool.go` or existing tools).
- No background processes started; no shell/git/gh run (code-only, per task
  constraint).

## Validation Pending

No local Go toolchain available — the Docker gate (controller) must run:
`go build ./...`, `go vet ./...`, `go test ./internal/tools/`.

## Files Not Owned (Read-Only Reference)

Read-only references used for patterns only: `internal/tools/delegate_tool.go`,
`internal/tools/registry.go`, `internal/orchestration/{parallel,verdict,negotiate,child_result}.go`,
`internal/contract/contract.go`, `internal/store/{contract_store,artifact_store,stores}.go`,
`internal/store/{pg,sqlitestore}/{contract,artifact,factory}.go`, `internal/tracing/context.go`,
`internal/bus`.

Status: DONE
Summary: Jury + negotiate tools implemented with 9 tests and wired into cmd/gateway_managed.go; stores.go + PG/SQLite factories got the Contracts/Artifacts fields WS-B deferred. Docker gate (build/vet/test) pending controller verification.
Concerns/Blockers: None. No local Go available to self-verify; factory additions were authorized by the task since WS-B deferred wiring.
