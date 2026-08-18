---
name: fix
description: Fix bugs and failures with mandatory root cause analysis before any change (architect /gc:fix)
license: Proprietary. Part of GoClaw bundled skills.
version: 1
inputs:
  - issue
outputs:
  - root_cause
  - regression_test
  - fix_summary
allowed-tools:
  - shell
  - filesystem
  - search
quality-gates:
  - root_cause_identified
  - regression_test_added
  - tests_pass
---

# Fix

Repair defects with proven root cause analysis. A fix is only acceptable when it
removes the cause, not the symptom.

## Purpose

`/gc:fix` turns a reported problem into a verified root cause and a minimal fix
that cannot regress silently. Symptom patches produce false confidence — they
mask the failure until the next occurrence.

## Operating Rules

- NEVER patch symptoms. Prove the cause before changing behavior.
- Keep the fix minimal: the smallest change that removes the root cause.
- Every fix ships with a regression test that fails before the fix and passes after.
- Do not touch unrelated behavior, formatting, or contracts.
- A fix is not complete until the regression test and the relevant suites pass.

## Modes

These flags are extracted by the parser and passed through. Adapt scope accordingly:

- `--fast` — shallow pass: reproduce, confirm an unambiguous cause, fix, test.
  Skip broad investigation when the failure mode is already clear.
- `--deep` — full RCA: work every stage below, including counter-evidence and
  alternate hypotheses.
- `--hard` — broader: treat the symptom as one instance of a class root cause;
  after fixing, search for co-located instances and add coverage for the class.

## RCA pipeline

Work the stages in order. Do not jump to the fix.

### 1. Reproduce

Produce the failure reliably and independently of the report. Record the exact
input, command, environment, and error. If reproduction is impossible, gather
the available evidence and state that reproduction is partial.

### 2. Collect evidence

Gather the facts: error messages, logs, traces, the failing test, and the exact
call path. Capture the state at the moment of failure. Quote `file:line` for
every observation.

### 3. Form hypotheses

List candidate root causes consistent with the evidence. Rank them by how well
they explain all observations and how cheaply they can be tested.

### 4. Test hypotheses

For each candidate, design a discriminating check: a focused test, a log line, a
minimal reproduction, or a code-read proof. Run it and discard hypotheses the
evidence refutes. Do not rely on reasoning alone where an experiment fits.

### 5. Identify the root cause

State the root cause as a mechanism, with the evidence chain from symptom back
to cause, and name the `file:line` where the wrong decision is made.

### 6. Write the minimal fix

Change the smallest surface that stops the mechanism. Preserve public contracts.
If the root cause reaches beyond the reported symptom, scope the change to the
cause and record the wider finding rather than silently widening the diff.

### 7. Add the regression test

Write a test that asserts the repaired behavior and fails on the pre-fix code.
Run it against the original code to prove it catches the bug, then against the
fix to prove it passes. Both directions matter.

### 8. Verify

Run the regression test plus the narrowest full suite for the touched package,
then broaden to lint, vet, build, and any shared-contract tests. Report the
commands and their results.

## Quality gates

Confirm all three before claiming completion:

- **root_cause_identified** — the root cause is stated with evidence and `file:line`.
- **regression_test_added** — a test exists that fails on pre-fix code and passes on the fix.
- **tests_pass** — the regression test and the relevant suites pass.

## Output

Summarize to the user: the reproduced symptom, the evidence chain, the root
cause, the change made (`file:line`), the regression test, and the verification
results. Do not end the run if any quality gate is unmet.