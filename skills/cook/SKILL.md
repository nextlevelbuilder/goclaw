---
name: cook
description: Implement plans and features with mandatory verification before completion (architect /gc:cook)
license: Proprietary. Part of GoClaw bundled skills.
version: 1
inputs:
  - plan
  - issue
outputs:
  - implemented_code
  - verification_result
allowed-tools:
  - filesystem
  - shell
  - search
  - exec
quality-gates:
  - verification_passed
---

# Cook

Turn a plan or issue into verified, working code. Code generation is not
completion — the task completes only after verification passes.

## Purpose

`/gc:cook` executes an accepted plan (from `/gc:plan`) or a direct
implementation request. The workflow is instruction-driven: read the plan,
make changes, run the tests, repair failures, and verify. A feature is
delivered when its verification passes, never when the code is merely written.

## Operating Rules

- Read the plan or request first. Never start changing code without knowing the
  acceptance criteria.
- Work in small, mergeable steps. After each step, re-run the narrowest
  verification that applies.
- **Code generated is not task completed.** Do not end the run after writing
  code — run the tests, builds, and checks that prove the work.
- Repair, do not mask: when a test fails, diagnose the cause and fix it. Do not
  delete, weaken, or skip the test to make it pass.
- Preserve public contracts unless the plan explicitly changes them.

## Progress tracking

The agent loop records checkpoints in the run timeline and the plan. After each
phase (read → implement → test → repair → verify), update the progress record so
a crash or context loss does not lose the state. If the runtime does not yet
have a durable execution engine, keep the checkpoint in the run timeline and
step forward phase by phase.

## Workflow

Follow the loop in order. Do not skip verification.

### 1. Read the plan

Read the plan artifact (from `/gc:plan`) or the issue. Extract the Goal,
Acceptance Criteria, Files to Change, and Test Plan. If the acceptance criteria
are missing or ambiguous, state the assumption and proceed.

### 2. Create the checkpoint

Record the starting state in the run timeline: the plan being executed, the
branch, and the step list. This is the recovery point.

### 3. Modify the code

Implement one step at a time. Change only the files the plan names. Follow the
repository's existing patterns. Re-run the narrowest test after each step
before moving on.

### 4. Run the tests

Run the test plan from the plan artifact, starting with the narrowest suite
that covers the touched behavior, then broadening to the package, the build, and
lint/vet.

### 5. Inspect failures

When a test fails, collect the failure evidence first: the error message, the
stack, the exact input, and the diff. Do not guess. Diagnose the cause before
repairing.

### 6. Repair and re-run

Fix the root cause of the failure, then re-run the failing test and its suite.
Repeat until green. Do not weaken the test to pass.

### 7. Apply the quality gate

Verify every acceptance criterion from the plan against the implemented result.
Confirm the quality gate: **verification_passed** — all tests, builds, and
checks listed in the Test Plan pass.

### 8. Update the checkpoint and report

Mark the steps complete in the run timeline, and report what was implemented,
which files changed (`file:line`), the verification commands run, and their
results.

## Quality gates

Only one gate decides completion:

- **verification_passed** — the tests, builds, and checks from the plan's Test
  Plan pass, and every acceptance criterion is met.

Do not report the task complete until verification passes. If verification
cannot pass, report the failure with evidence instead of claiming completion.

## Output

Summarize to the user: the plan/issue addressed, the files changed, the
verification commands and results, and the remaining risks. If a step is
blocked, say what is blocked and what is needed to unblock it.