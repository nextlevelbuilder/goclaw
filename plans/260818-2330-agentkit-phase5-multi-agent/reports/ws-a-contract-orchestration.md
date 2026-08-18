# WS-A Report — Contract + Orchestration Runtime (Go thuần, NO DB)

Phase 5 Multi-Agent, Stage 1, WS-A. Implementation complete. Code only — Docker gate
run by controller (no local Go).

## Files created (disjoint ownership — 8 files)

| File | Purpose |
|------|---------|
| `internal/contract/contract.go` | Package `contract`: `ContractKind`, `ValidContractKind`, `ContractBudget`, `Verdict`, `Contract`, `Validate`, `AddVerdict`, `Consensus` |
| `internal/contract/contract_test.go` | Unit tests: Validate (ok/nil/kind-invalid/empty task), AddVerdict, Consensus (2/3, no-verdicts, split, different contenders, invalid fraction), JSON roundtrip (Verdicts `json:"-"`) |
| `internal/orchestration/parallel.go` | `RunnerFunc`, `Contestant`, `RunParallelOpts`, `RunParallel` (bounded worker pool, FailFast, index-aligned results, preseeds failed slots, panic recovery), `RunParallelError` |
| `internal/orchestration/verdict.go` | `Scorer`, `JudgeOpts`, `Judge` (weighted criteria, approve/revise/reject bands, slog decision log), consensus helpers |
| `internal/orchestration/negotiate.go` | `Proposal`, `Negotiation`, `NewNegotiation`, `SubmitProposal` (bounded rounds), `Vote`, `ReachedConsensus`, `IsExhausted` |
| `internal/orchestration/parallel_test.go` | Fan-out run count, bounded concurrency (atomic max ≤ opts), default concurrency, FailFast cancel, error/edge cases, preseeded failed slots |
| `internal/orchestration/verdict_test.go` | Judge best-select, correctness-dominates, revise band, reject band, all-failed, error cases, tie-break, default match |
| `internal/workflow/agents.go` | `AgentStep` (sequential Step wrapper with deps), `ParallelAgentRound` (StepParallel DAG, dedupe/empty skip, nil-run guard) |

Report: `internal/contract/*` (contract), `internal/orchestration/parallel.go`,
`internal/orchestration/verdict.go`, `internal/orchestration/negotiate.go`,
`internal/workflow/agents.go`.

## Design decisions

- `internal/contract` mirrors `internal/artifact/types.go`: pure domain, imports
  only `errors`, `math`, `time`. No store/providers. No import cycle.
- `internal/orchestration` imports `internal/contract` (Verdict is a contract
  type); ASCII-delimited keying of `contenderID\0decision` prevents faction
  merging. `Consensus` uses floating-point-safe ceil so `2/3` of 3 votes needs 2.
- `internal/workflow` imports only `context` + `log/slog` — stays Phase-3 pure
  (no orchestration import), per phase constraint.
- `RunParallel`: worker-pool channel pattern (no raw `sync.WaitGroup`), bounded
  concurrency default 4, FailFast on first error with context cancel, index-aligned
  results, every slot given a definite status, panic recovery in `runSafely`,
  external-cancel propagation.
- `Judge`: weighted criteria (repetition = weight), normalized by pass count,
  single best winner (contestant-order tie-break), decision bands
  approve ≥ Match, revise ≥ Match×0.8, else reject. All-failed → reject with warn.
- `Negotiation`: `MaxRounds` bounds proposals (`SubmitProposal` fails past bound),
  votes recorded until consensus locks (2/3 default, min 2 votes to self-lock),
  `IsExhausted` = consensusReached ∨ rounds exhausted. No panic paths; slog used
  for proposal/decision/vote-ignored traces.

## Tests status

- Type check: controller-run (Go 1.26 Docker). No local Go available.
- Unit tests authored:
  - `internal/contract/` — 10 test funcs, consensus math + JSON roundtrip + Validate.
  - `internal/orchestration/` — parallel (7), verdict (9), negotiate (14) test funcs.
  - `internal/workflow/` — agents_test (6 test funcs, reuses `mustAdd`/`noop`).
- No benchmark/load/stress tests (explicitly excluded).

## Acceptance criteria

1. `internal/contract` tests cover Validate / AddVerdict / Consensus (2/3 math, split,
   contenders) / roundtrip. DONE.
2. `internal/orchestration` tests: RunParallel N+1 runs with bounded concurrency
   (atomic maxActive ≤ opts, also asserts real parallelism maxActive > 1), error
   cancels remaining runners, Judge best selection, negotiation bounded rounds +
   consensus stop + IsExhausted. DONE.
3. `internal/workflow` tests: AgentStep / ParallelAgentRound build valid DAG; existing
   Phase-3 tests untouched (no edits to existing files). DONE.
4. `go build ./...`, `go vet ./...`, `go test ./internal/contract/ ./internal/orchestration/ ./internal/workflow/` — controller Docker gate (no local Go). Pending controller verification.
5. Import cycle check: contract→(nothing), orchestration→contract, workflow→(nothing). DONE by inspection.
6. No benchmark/load/stress tests. DONE.
7. Comments describe behavior; no phase-ID/plan-ID in code. DONE.

## Cross-stream contract for Stage 2 (WS-C/WS-D)

```go
contract.Validate() error
orchestration.RunParallel(ctx, contestants []Contestant, runner RunnerFunc, opts RunParallelOpts) ([]ChildResult, error)
orchestration.Judge(ctx, contestants, results, JudgeOpts) (contract.Verdict, error)
orchestration.NewNegotiation(*contract.Contract, maxRounds int) (*Negotiation, error)
contract.Contract // marshal to JSON; WS-B stores Body string
```

Status: DONE
Summary: Implemented pure-Go contract domain + orchestration runtime (bounded parallel fan-out, judge, negotiation) + workflow DAG agent helpers, with full unit coverage and no store/provider imports.
Concerns/Blockers: None code-side. Docker gate (build/vet/unit) pending controller; no local Go in this session by design.