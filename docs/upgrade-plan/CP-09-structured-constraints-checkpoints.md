# CP-09 Structured Constraints — Execution Checkpoints

This document translates [CP-09-structured-constraints-spec.md](./CP-09-structured-constraints-spec.md) into implementation checkpoints that can be executed and verified in code.

The rule is strict:

1. Tool emits typed constraints.
2. Runtime stores them as turn state.
3. Runtime blocks impossible retries before execution.
4. Runtime exposes active constraints to the model every iteration.
5. The model reroutes or closes out from evidence already gathered.

This avoids the old failure mode where GoClaw "noticed" a blocker but did not turn it into control state.

## CP-09.1 — Constraint Primitives and Turn State

**Goal**

- Introduce a first-class runtime constraint model.
- Keep constraint state inside the pipeline turn state, not hidden inside string heuristics.

**Files**

- `internal/pipeline/constraint.go`
- `internal/pipeline/constraint_store.go`
- `internal/pipeline/novelty_tracker.go`
- `internal/pipeline/substates.go`
- `internal/pipeline/run_state.go`
- `internal/pipeline/turn_state.go`

**Definition of done**

- `Constraint`, `ConstraintStore`, and `NoveltyTracker` exist.
- `RunState.Tool` always has a constraint store and novelty tracker.
- Turn closeout supports `needs_human` as an explicit runtime state.
- Constraint matching is deterministic and lives in one place.

**Verification**

- Store can add, merge, and format active constraints.
- Binary/tool-target matching works for `exec`, `spawn`, and file/web targets.
- Turn state can move to `needs_human` without abusing old string flags.

## CP-09.2 — Pre-call Runtime Enforcement

**Goal**

- Block impossible retries before tool execution.
- Count blocked retries as runtime evidence, not just model mistakes.

**Files**

- `internal/pipeline/tool_stage.go`
- `internal/pipeline/think_stage.go`

**Definition of done**

- Exact duplicate tool calls are skipped deterministically.
- Hard constraints block matching tool calls before execution.
- Two consecutive blocked attempts can force partial closeout.
- Active constraints are injected into the next LLM request as runtime context.

**Verification**

- A blocked tool increments tool-call budget and leaves an explanatory system note.
- A human-required constraint arms answer-only closeout for the next iteration.
- Constraint summaries appear in the request path without polluting the base system prompt builder.

## CP-09.3 — Tool Emission Path

**Goal**

- Make tools emit typed blockers instead of only returning free-text failures.

**Files**

- `internal/tools/result.go`
- `internal/tools/subagent_spawn.go`
- `internal/tools/subagent_spawn_tool.go`

**Definition of done**

- `tools.Result` can carry structured constraints.
- Spawn depth, concurrent, and per-parent child limits return typed capacity constraints.
- Spawn limit handling is based on typed errors, not string parsing.

**Verification**

- Hitting spawn child/concurrency/depth limits returns `capacity_exhausted` with the correct subject.
- The model receives an actionable error and the runtime can block the next `spawn`.

## CP-09.4 — Post-call Runtime Interpretation

**Goal**

- Convert decisive probe misses and low-signal repetition into constraints.

**Files**

- `internal/agent/exec_probe_recovery.go`
- `internal/agent/loop_pipeline_tool_callbacks.go`
- `internal/agent/loop_tracing.go`

**Definition of done**

- Read-only exec probe misses emit `binary_missing` on the first decisive miss.
- Novelty tracking emits `low_signal` and `repeated_failure` when the result stream stalls.
- Constraint emission happens before trace finalization so trace metadata can capture it.

**Verification**

- `which git && git --version` with a miss yields `binary_missing:git`.
- Repeated identical fetches register low-signal state for that target.
- Tool spans include emitted constraints in metadata.

## CP-09.5 — Regression Harness and Production Gates

**Goal**

- Lock the new behavior with tests that map directly to the production failure classes.

**Files**

- `internal/pipeline/constraint_store_test.go`
- `internal/pipeline/novelty_tracker_test.go`
- `internal/pipeline/turn_state_test.go`
- `internal/agent/exec_probe_recovery_test.go`
- `internal/tools/subagent_spawn_tool_test.go`

**Definition of done**

- Unit tests cover store semantics, novelty rules, transitions, and spawn/exec failure emission.
- The two real production classes are locked:
  - missing prerequisite loops
  - spawn quota loops

**Verification**

- After a missing-binary probe, the next matching `exec` call is blocked pre-call.
- After a spawn quota error, the next `spawn` is blocked pre-call.
- Existing pipeline tests still pass.

## Ship Gate

CP-09 is ready to ship only when all of the following are true:

- No subsequent `git` shell calls execute after `binary_missing:git` is registered.
- No subsequent `spawn` executes after capacity exhaustion is registered.
- The model sees active constraints on the very next reasoning step.
- The turn ends in `needs_human` or `partial` through runtime state, not prompt wording.
