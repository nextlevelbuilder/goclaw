---
name: debug
description: Investigate bugs with a prove-the-cause pipeline before choosing a fix — frame, scout, diagnose, prove, fix, test (architect /gc:debug)
license: Proprietary. Part of GoClaw bundled skills.
version: 1
inputs:
  - bug_report
outputs:
  - root_cause
  - evidence_chain
  - fix_summary
allowed-tools:
  - shell
  - search
  - read_file
  - filesystem
  - exec
quality-gates:
  - cause_proven
  - evidence_located
  - fix_tested
---

# Debug

Investigate a bug to a proven root cause before touching any code. The
deliverable is the evidence chain from symptom to cause, then a minimal fix
with a regression test. Jumping to a fix without proof is how symptoms get
patched and the bug reships next week.

## Purpose

`/gc:debug` runs the bugfix workflow: frame the expected repaired behavior,
scout the relevant code, diagnose the actual mechanism, prove the cause, choose
a cause-aligned fix, implement, and test. Each stage gates the next — you may
stop after diagnosis if the task is read-only investigation, but you never fix
before proof.

## Operating Rules

- NEVER patch symptoms. Prove the cause before changing behavior.
- Read-only investigation may stop after scouting or diagnosis. Changing code
  requires a proven cause.
- Ground every observation in `file:line`. An unlocated observation is a guess.
- Keep the fix minimal and cause-aligned; preserve public contracts.
- Every fix ships with a regression test that fails on pre-fix code and passes
  on the fix.

## Workflow

Follow the stages in order. Do not skip to the fix.

### 1. Frame the outcome

State the expected repaired behavior and the safety boundary in one or two
sentences. What must the system do after the fix, and what must it not do? If
the frame is ambiguous, record the most reasonable reading and the assumption.

### 2. Scout

Read the bug report, then find the code that owns the behavior: the handler,
the store method, the pipeline stage, the component. Read the surrounding
tests and docs. Trace control flow, not just lines — identify every early
return and branch between the entry point and the observed symptom.

### 3. Diagnose

Form candidate causes consistent with the evidence. Rank them by how well they
explain all observations and how cheaply they can be tested. List every
early-return or condition that could gate the symptom — "X without Y" claims
must be verified against the actual control flow, not assumed.

### 4. Prove the cause

For the top candidate, design a discriminating check: a focused test, a log
line, a minimal reproduction, or a code-read proof. Run it. Discard hypotheses
the evidence refutes. The root cause is proven only when the evidence chain
from symptom to mechanism is complete and repeatable.

### 5. Choose the fix

Pick the minimal change that stops the mechanism. If the root cause reaches
beyond the reported symptom, scope the change to the cause and record the wider
finding rather than silently widening the diff. Prefer the simplest solution
that addresses the root cause directly.

### 6. Implement and test

Apply the fix, then run the regression test against the pre-fix and post-fix
code to prove both directions. Broaden to the touched package's suite, then to
lint, vet, and build.

## Quality gates

Confirm all three before claiming completion:

- **cause_proven** — the root cause is stated as a mechanism with a repeatable
  evidence chain, not an educated guess.
- **evidence_located** — every claim in the chain cites `file:line` from the
  actual code path.
- **fix_tested** — a regression test fails on pre-fix code and passes on the
  fix, and the relevant suites are green.

## Output

Summarize to the user: the framed outcome, the reproduced symptom, the evidence
chain, the root cause, the change made (`file:line`), the regression test, and
the verification results. If reproduction was partial, state exactly what was
and was not proven.
