# WS-D Report — Dynamic Team Formation Routing + Gateway Wiring

## Scope

Implemented dynamic team formation routing (¶63) for multi-agent collaboration:
a pure, deterministic formation catalog that routes a task + complexity hint to
a team shape (solo / debugger panel / planner-coder-tester / architect-review),
plus the `multiagent.*` WebSocket RPC surface (formation routing + jury and
negotiation read-only history, ¶28) with protocol events. Formation routing is
additive — the existing `Mode` enum, `Classify`, and `ClassifyWithLLM` are
untouched.

## Files Modified/Created

| File | Action | Notes |
|------|--------|-------|
| `internal/teamworkclassify/formation.go` | created | `Formation`, `FormationMode`, 4-entry deterministic `formationCatalog`, `SelectFormation`, `ModeFormation`, `FormationCategory`, `FormationModeFor`, `normalizeComplexity`, `ErrUnknownFormation` |
| `internal/teamworkclassify/formation_test.go` | created | 8 tests (7 named + 1 table subtests), pure package-level (no store/DB) |
| `internal/gateway/methods/jury.go` | created | `MultiAgentMethods` (formation/jury/negotiate handlers), nil-safe `broadcast`, `handleRecordList` shared list path |
| `internal/gateway/methods/jury_test.go` | created | 8 tests via `gateway.NewCapturingTestClient` + `stubContractStore` |
| `pkg/protocol/methods.go` | modified | `MethodMultiAgentFormation/Jury/Negotiate` constants |
| `pkg/protocol/events.go` | modified | `EventMultiAgentFormationSelected/Verdict/NegotiationState` constants |
| `pkg/protocol/team_events.go` | modified | `MultiAgentFormationPayload`, `MultiAgentVerdictPayload`, `MultiAgentNegotiationPayload` |
| `internal/permissions/policy.go` | modified | formation → write (operator+); jury/negotiate → read (viewer+) — required by `TestMethodRole_DriftCoverage_AllProtocolMethodsClassified` fail-closed drift test |
| `internal/i18n/keys.go` | modified | `MsgMultiAgentStoreUnavailable` |
| `internal/i18n/catalog_{en,vi,zh,ko,ru}.go` | modified | translations for the new key in all 5 catalogs |
| `cmd/gateway_methods.go` | modified | `NewMultiAgentMethods(contractStore, msgBus).Register(router)` + `contractStore` param + `protocol` import for slog |
| `cmd/gateway.go` | modified | `registerAllMethods` call site appends `pgStores.Contracts` |

## Design

- **Formation catalog (pure, closed):** `formationCatalog` maps 4 stable keys to
  agent roles + pipeline stages + a complexity tier. `SelectFormation(task,
  complexity, override)` is a pure function — no store, no LLM, no I/O. An
  explicit override always wins (case-insensitive, trimmed); an unknown override
  returns `ErrUnknownFormation`. Complexity is a soft hint bucketed by
  `normalizeComplexity` (high/complex/critical/hard → high, medium/moderate/normal
  → medium, build/feature/full/multi-role → build, everything else → low).
  Determinism guarantee: same inputs → same formation, verified by test.
- **Additive mode extension:** `FormationModeFor(base Mode, f)` joins the base
  mode with a `formation:` suffix (`ModeTeam` → `"team:formation:debugger-panel"`).
  Existing `Mode` enum values are never changed; `ModeFormation` is a separate
  additive helper.
- **Gateway surface:** `MultiAgentMethods.Register` wires
  `multiagent.formation` (operator+/write), `multiagent.jury`, and
  `multiagent.negotiate` (viewer+/read). `handleFormation` validates `task`
  (non-empty → `INVALID_REQUEST`), routes via `SelectFormation`, responds with
  the `MultiAgentFormationPayload`, and broadcasts
  `multiagent.formation_selected` (nil-safe publisher). Jury/negotiate share
  `handleRecordList` filtered by `store.ContractRecordJury` /
  `store.ContractRecordNegotiation`; a nil contract store responds
  `UNAVAILABLE` with the localized `MsgMultiAgentStoreUnavailable` instead of
  failing closed silently. Read-only history only — execution happens in the
  agent loop via WS-C's jury/negotiate tools.
- **RBAC drift:** the 3 new methods are classified in `permissions/policy.go`.
  `multiagent.formation` is write-classified because it emits a routing event;
  `multiagent.jury`/`multiagent.negotiate` are read-classified. Without this the
  fail-closed drift test (`TestMethodRole_DriftCoverage_AllProtocolMethodsClassified`)
  would fail.
- **Logging:** `slog.Warn` for invalid overrides and store list failures — no
  panics anywhere.

## Tests (16 total, no benchmark/load)

Formation (`internal/teamworkclassify/formation_test.go`, 8):
1. `TestSelectFormation_ExplicitOverrideWins` — override beats complexity; agents/pipeline/mode-extension asserted.
2. `TestSelectFormation_OverrideCaseInsensitiveAndTrimmed` — `"  Planner-Coder-Tester  "` resolves.
3. `TestSelectFormation_UnknownOverrideErrors` — `ErrUnknownFormation` via `errors.Is`.
4. `TestSelectFormation_DefaultForEmptyTaskAndComplexity` — empty → solo-followup, category solo.
5. `TestSelectFormation_DeterministicByComplexity` — table: high/complex→architect-review, medium/moderate→debugger-panel, build/feature→planner-coder-tester, low/""/unknown→solo; double-call stability asserted.
6. `TestFormationModeFor_ExtendsModeAdditively` — base `ModeTeam` → `"team:formation:debugger-panel"`.
7. `TestFormationCategory_UnknownFallsBackToSolo`.
8. `TestSelectFormation_OverrideDoesNotChangeCatalog` — catalog map is read-only under override selection.

Gateway (`internal/gateway/methods/jury_test.go`, 8):
1. `TestMultiAgentFormation_RoutesToCatalog` — high complexity → architect-review-team, agents/pipeline/override=false in payload.
2. `TestMultiAgentFormation_OverrideWins` — debugger-panel override, override=true.
3. `TestMultiAgentFormation_EmptyTaskErrors` — `INVALID_REQUEST`.
4. `TestMultiAgentFormation_UnknownOverrideErrors` — `INVALID_REQUEST`.
5. `TestMultiAgentJury_ListRecords` — kind + runId filter forwarded to store, kind echoed in payload.
6. `TestMultiAgentJury_StoreUnavailable` — nil store → `UNAVAILABLE`.
7. `TestMultiAgentNegotiate_ListRecords` — filtered to `negotiation` kind.
8. `TestMultiAgentJury_ListStoreError` — store error → `INTERNAL`.

Reuses `stubEventPub` from `sessions_test.go` (same `methods` package) — no
duplicate type declaration.

## Cross-Surface Parity

- **API contract:** new `multiagent.*` methods + event names + payload types in
  `pkg/protocol` — consumers (UI) can render formations/verdicts/negotiations.
- **Web UI / CLI:** N/A for this workstream — UI/CLI consumption of these
  methods is a later phase; the RPC surface + events are the deliverable.
- **Gateway server:** methods wired in `cmd/gateway_methods.go` +
  `cmd/gateway.go` call-site.
- **Stores:** consumed `store.ContractStore` via the `pgStores.Contracts` field
  added by WS-B — no store files touched (WS-B ownership).

## Safety Constraints

- Did NOT touch `internal/tools/**`, `internal/contract/**`,
  `internal/store/**`, `internal/orchestration/**`, `internal/workflow/**`.
- Did NOT modify `classifier.go` / `context.go` — formation is additive
  (`formation.go` imports the existing `Mode` type only).
- `pkg/protocol/*.go` modified only to add constants/types — no existing
  contract changed.
- No phase IDs in code comments; comments describe behavior.
- i18n key present in all 5 catalogs (`git_keys_parity_test.go` covers RU).
- No background processes started; no shell/git/gh run (code-only, per task
  constraint).

## Validation Pending

No local Go toolchain available — the Docker gate (controller) must run:
`go build ./...`, `go vet ./...`, and
`go test ./internal/teamworkclassify/ ./internal/gateway/methods/`.

## Files Not Owned (Read-Only Reference)

Read-only references for patterns only: `teamworkclassify/classifier.go`,
`context.go`, `internal/store/contract_store.go`, `internal/store/stores.go`,
`internal/store/pg/factory.go`, `internal/store/sqlitestore/factory.go`,
`internal/gateway/router.go`, `client.go`, `client_testing.go`,
`internal/bus/types.go`, `sessions_test.go`, `internal/workflow/agents.go`,
`pkg/protocol/methods.go`, `events.go`, `team_events.go`.

Status: DONE_WITH_CONCERNS
Summary: Formation routing (deterministic 4-entry catalog + override + complexity bucketing) and the multiagent.* WS surface (formation/jury/negotiate) implemented with 16 deterministic tests, protocol events/payloads, RBAC classification, i18n in 5 locales, and gateway wiring.
Concerns/Blockers: Docker gate (go build/vet/test) not run locally — no Go toolchain on Windows; verification pending controller execution.
