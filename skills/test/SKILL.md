---
name: test
description: Plan and execute structured testing — unit, integration, chaos — with Docker gates and narrowest-first ordering (architect /gc:test)
license: Proprietary. Part of GoClaw bundled skills.
version: 1
inputs:
  - change_or_system
  - test_scope
outputs:
  - test_plan
  - test_results
  - coverage_gap_list
allowed-tools:
  - shell
  - filesystem
  - search
  - read_file
  - exec
quality-gates:
  - narrowest_first
  - failures_triaged
  - verification_reported
---

# Test

Produce a structured test strategy and execute it. Testing is a pyramid, not a
pile: prove the narrowest layer first, broaden only when shared behavior or
public contracts change, and finish with a written result — never a silent
"looks green".

## Purpose

`/gc:test` turns a change or a system into a test plan and executes it against
the repo's layered suites. It decides what to test, at which layer, in which
order, and what each layer must prove. The gate for completion is evidence, not
intent.

## Operating Rules

- Start with the narrowest useful test for the touched behavior, then broaden.
- Do not hide failing tests, lint, type, build, or syntax errors. A failing
  suite is evidence, not an excuse to stop.
- Repair, do not mask: when a test fails, diagnose the root cause and fix it.
  Never delete, weaken, or skip a test to make it pass.
- Preserve public contracts; a test that asserts current broken behavior is a
  characterization test only if deliberately labeled and reviewed.
- No load/stress/benchmark tests for routine work — they flake on shared CI
  runners and rarely catch real bugs. Add them only when explicitly requested.

## Test layers

Match the suite to the layer. Each layer has a purpose and a gate.

### 1. Unit tests

- Scope: a package, a function, a pure decision.
- Gate: `go test ./internal/<pkg>/` for Go; `pnpm test` for UI packages.
- Prove: the behavior of the changed logic in isolation, happy path and key
  failure cases.

### 2. Integration tests

- Scope: cross-package contracts — stores, RPC handlers, pipeline stages.
- Gate: `-tags integration ./tests/integration/` against a real PG fixture.
- Prove: the wiring between components behaves as the contract says.

### 3. Chaos / reliability tests

- Scope: failure injection — stream disconnects, stale runs, weak models.
- Gate: the dedicated chaos suites under `tests/` wired to CI (e.g. `make
  test-phase9` style reliability suites).
- Prove: the system degrades, recovers, and reports failures instead of
  corrupting state.

### 4. Build and static gates

- Scope: the whole module, both build tags.
- Gate: `go build ./...`, `go build -tags sqliteonly ./...`, `go vet ./...`,
  `go fix ./...`, and the web build (`pnpm build`).
- Prove: everything compiles, vets, and is formatted on both backends and the UI.

## Docker gate pattern

When the environment has no local toolchain, run the Go gates in a container:

```sh
MSYS_NO_PATHCONV=1 docker run --rm \
  -v "<repo>:/src" \
  -v goclaw-gomodcache:/go/pkg/mod \
  -v goclaw-gomodcache:/root/.cache/go-build \
  -w /src golang:1.26.0 /bin/sh -c \
  'go build ./... && go vet ./... && go test ./internal/<pkg>/'
```

For a broadened gate, replace the trailing test path with the package list the
change touches, then the full module. Use `go test -race` for suites that
exercise concurrency.

## Workflow

1. **Read the change** — read the diff, the touched packages, and their
   existing tests. Identify what behavior is new or changed.
2. **Write the test plan** — for each behavior: layer, suite name, and what it
   must prove. List the exact commands in the order to run them (narrowest
   first).
3. **Add tests** — for new logic, add unit tests covering the happy path and
   key failure cases. Regression tests must fail on pre-change code.
4. **Run narrowest first** — run the single package/component test for the
   touched behavior. Fix failures before broadening.
5. **Broaden** — run the package suite, then the integration suite, then build
   + vet + static gates, then the full module when shared contracts changed.
6. **Triage failures** — collect the error message, the stack, the exact input,
   and the diff. Diagnose before fixing. Re-run after each fix.
7. **Report** — write the result: the commands run, pass/fail per suite, the
   failures triaged with root causes, and any coverage gaps left for follow-up.

## Quality gates

Confirm all three before finishing:

- **narrowest_first** — the execution order started at the narrowest layer that
  covers the touched behavior, then broadened.
- **failures_triaged** — every failing test was diagnosed to a root cause and
  fixed (or explicitly reported with evidence, never hidden).
- **verification_reported** — the final report lists the exact commands run and
  their results; no unverified "looks green" claim.

Do not report the task complete until the gates pass or the blocker is stated
with evidence.
