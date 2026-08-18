---
name: architect
description: Produce an architecture proposal — goal, current state, problem, proposed design, files, migration, risks, test plan, rollback, acceptance (architect /gc:architect)
license: Proprietary. Part of GoClaw bundled skills.
version: 1
inputs:
  - design_goal
outputs:
  - architecture_proposal
  - risk_register
  - acceptance_criteria
allowed-tools:
  - search
  - read_file
  - filesystem
quality-gates:
  - proposal_complete
  - risks_ranked
  - acceptance_checkable
---

# Architect

Turn a design goal into a decision-ready architecture proposal. A proposal is
the artifact a reviewer can accept or reject — it names the current state, the
gap, the proposed design with real boundaries, the files to change, the
migration path, the ranked risks, the test plan, the rollback, and the
acceptance criteria. No design decision is hidden inside prose.

## Purpose

`/gc:architect` produces the architecture section of a plan (or a standalone
proposal) for a change that is large, cross-module, or risky. It forces the
hard questions before code: what exists today, what actually breaks, what is
irreversible, and how do we know we are done.

## Operating Rules

- Ground the Current State in the actual code. Never infer architecture from
  names alone — grep the call sites, trace the control flow, cite `file:line`.
- Scope-check before adding state: if the design adds a field or a structure,
  state its lifetime and ownership. "Plausibly per-request" without a grep of
  construction and callers is a red flag.
- Preserve public contracts unless the proposal explicitly breaks them, and
  flag every intentional break with a rationale.
- Prefer the simplest solution that addresses the root cause directly. Explicit
  configuration over runtime heuristics.
- This skill writes a proposal document. Do not implement code.

## Proposal sections

Produce exactly these sections, in order.

### 1. Goal

One paragraph: what is being achieved and why. Name the outcome, not the
mechanism.

### 2. Context

Background, linked plans/docs, user decisions, assumptions, and any scope-affecting
constraints (e.g. a YAGNI request that cuts promised scope).

### 3. Current State

How the system behaves today, with `file:line` references. Name the module
boundaries the change crosses and the existing patterns that must be followed.

### 4. Problem

The concrete gap or defect the goal addresses. State what fails, when, and the
cost of leaving it unaddressed. Reference the production scenarios that matter:
high concurrency, multi-tenant isolation, failure cascades, long-running
sessions.

### 5. Proposed Design

The design after the change: new or modified components, their boundaries, the
data flow, and the interfaces between them. Name every new symbol the proposal
introduces and its lifetime/ownership.

### 6. Files to Change

A table: file path, modify/create/delete, purpose. Grep the whole repo for
every symbol the proposal touches — stubs often have references in routing,
catalogs, and switch cases that the grep of the definition misses.

### 7. Migration

Schema, data, or config migration steps, if any. Note which databases are
affected (PostgreSQL vs SQLite in this repo) and the lockstep requirements.
State what is reversible and what is not.

### 8. Risks

Ranked risks with mitigations and rollback notes. Rank by likelihood and
impact. Name the irreversible decisions explicitly.

### 9. Test Plan

The exact tests, builds, and manual checks that prove each acceptance criterion.
Narrowest first, broadened to shared-contract suites.

### 10. Rollback Plan

How the change is reverted if something goes wrong: revert the code, run the
down migration, restore the backup. State the blast radius of each step.

### 11. Acceptance Criteria

Checkable conditions that mark the work complete. Each must be falsifiable —
a reader can verify it without the author. No "works well" or "fast enough".

## Workflow

1. **State the goal** — write the Goal section and the non-goals.
2. **Scout** — read the touched modules, their tests, and the governing docs.
   Grep every symbol the design references.
3. **Trace semantics** — for existing code the design reuses, identify when
   each field or function mutates and under what conditions. Line-range citation
   without control-flow trace is how ports silently invert behavior.
4. **Draft the proposal** — write all 11 sections. Where evidence is missing,
   mark it as an open question rather than filling it with a guess.
5. **Verify** — re-grep the claims against live code. Re-count every path,
   endpoint, and number.
6. **Deliver** — write the proposal to the workspace (e.g. `plans/<timestamp>-<slug>/`
   convention when in use) and summarize the decisions for the reader.

## Quality gates

Confirm all three before finishing:

- **proposal_complete** — all 11 sections exist and each names real content,
  not placeholders.
- **risks_ranked** — risks are ordered by likelihood×impact and irreversible
  decisions are called out.
- **acceptance_checkable** — every acceptance criterion is falsifiable and
  verifiable without the author.

Do not claim the proposal complete until the gates pass. If the design is still
uncertain, deliver the proposal with the open questions clearly marked instead
of a confident guess.