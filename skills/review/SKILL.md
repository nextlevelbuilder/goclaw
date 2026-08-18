---
name: review
description: Review code or changes against nine dimensions with severity and a written report (architect /gc:review)
license: Proprietary. Part of GoClaw bundled skills.
version: 1
inputs:
  - change
outputs:
  - review_report
allowed-tools:
  - search
  - read_file
  - filesystem
quality-gates:
  - severity_assigned
  - report_written
---

# Review

Audit code or a change against nine dimensions and produce a written report
with severities. A review that finds nothing is still a review — it must state
what was examined and why nothing was flagged.

## Purpose

`/gc:review` evaluates a change, a file set, or a design against a fixed set of
dimensions. Every finding gets a severity so the reader can triage. The output
is a `review-report.md` in the workspace.

## Operating Rules

- Read the change and its context first: the diff, the touched files, their
  tests, and the contracts they implement.
- Ground every finding in `file:line`. A finding without a location is an
  impression, not a review finding.
- Assign exactly one severity per finding. Do not hedge with a severity range.
- Write the report. An oral summary is not the deliverable.

## Review dimensions

Assess the change across all nine dimensions. Mark each dimension as checked
even when nothing is found.

1. **Correctness** — does the code do what it claims, including edge cases and
   error paths?
2. **Security** — injection, secrets, authorization boundaries, unsafe
   deserialization, path traversal, multi-tenant isolation.
3. **Performance** — avoidable allocations, N+1 queries, unbounded work, hot
   paths, missing indexes.
4. **Concurrency** — races, shared-state mutation, lock ordering, deadlocks,
   goroutine leaks.
5. **Reliability** — error handling, retries, idempotency, crash recovery,
   partial-failure handling.
6. **Maintainability** — naming, structure, duplication, complexity, adherence
   to existing patterns.
7. **API compatibility** — public contracts, request/response shapes, breaking
   changes, deprecations.
8. **Tests** — does the change ship tests that would fail without it? Are the
   assertions meaningful?
9. **Observability** — logging, metrics, tracing, error reporting for the new
   behavior.

## Severity scale

- **BLOCKER** — must not merge; the change is wrong, insecure, or unsafe.
- **CRITICAL** — must fix before merge; a real defect or contract break.
- **HIGH** — should fix before merge; likely to cause problems in practice.
- **MEDIUM** — fix when convenient; real but bounded concern.
- **LOW** — nice to have; minor quality or style issue.
- **INFO** — observation, not a defect; worth recording for later.

## Workflow

1. **Scope** — identify the change under review and its boundaries. State what
   is in scope and what is not.
2. **Read** — read the diff, the touched files, and their tests. Trace the
   control flow, not just the changed lines.
3. **Assess** — walk the nine dimensions. For each, decide whether the change
   introduces a finding.
4. **Findings** — for each finding record: `file:line`, a description of the
   defect, the failure scenario (when does it break?), and the suggested fix.
5. **Report** — write `review-report.md` with the scope, the dimension-by-
   dimension assessment, and the findings sorted by severity.

## Quality gates

Confirm both before finishing:

- **severity_assigned** — every finding has exactly one severity from the scale.
- **report_written** — `review-report.md` exists in the workspace and records
  the scope, the dimensions assessed, and the findings with `file:line`.

## Output

Report the summary to the user: the scope reviewed, the number of findings by
severity, and the location of the report. Do not end the review without the
report file.