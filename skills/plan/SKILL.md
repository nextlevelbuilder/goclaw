---
name: plan
description: Create an implementation plan from a user request (architect /gc:plan)
license: Proprietary. Part of GoClaw bundled skills.
version: 1
inputs:
  - user_request
outputs:
  - plan_artifact
allowed-tools:
  - filesystem
  - search
  - read_file
quality-gates:
  - plan_has_goal
  - plan_has_acceptance_criteria
  - plan_has_rollback
---

# Plan

Turn a user request into an actionable implementation plan before any code is
written.

## Purpose

`/gc:plan` produces an implementation plan artifact. A plan is the contract that
makes a later implementation verifiable — it is not a promise to implement.
Plan for any change that is large, cross-module, or risky. If the request is a
trivial single-file edit, say so and keep the plan short.

## Constraints

- Use read-only tools only: `filesystem`, `search`, `read_file`. Do not modify files.
- Ground every claim in the repository. Never infer architecture from names alone.
- The artifact is written under `plans/` in the workspace.

## Workflow

Follow the pipeline in order.

### 1. Understand the request

Read the user request and restate the goal, the out-of-scope items, and the
acceptance criteria in your own words before planning. If the goal is
ambiguous, pick the most reasonable reading and record the assumption in the
Context section.

### 2. Inspect the repository

Read the files the change touches, their tests, and the docs that govern them.
Use `search` to find every call site of a symbol you plan to change. Verify what
exists before proposing anything new.

### 3. Identify the architecture

Locate the module boundaries the change crosses and name the current design in
the Current Architecture section. Do not invent a design that is not in the code.

### 4. Identify constraints

List the constraints that bound the solution: public contracts, backward
compatibility, performance, security, multi-tenant isolation, and existing
patterns that must be followed.

### 5. Dependency analysis

Determine what the change depends on (packages, migrations, services, other
features) and what depends on the change. Note order-sensitive and
version-sensitive dependencies.

### 6. Risk analysis

Enumerate the failure modes: what can break, what is irreversible, and what is
hard to roll back. Rank by likelihood and impact.

### 7. Write the implementation plan

Break the work into ordered, mergeable steps. Each step must name the exact
files to change, create, or delete, and the order they must land in.

### 8. Write the verification plan

Specify the tests, builds, and manual checks that prove each step meets the
Acceptance Criteria.

### 9. Create the artifact

Write the plan to `plans/<timestamp>-<slug>.md` under the workspace, using a
`YYYYMMDD-HHMM` timestamp and a short kebab-case slug of the topic. This
repository keeps its plans under `plans/<timestamp>-<slug>/` (the current
`plans/260818-0915-agentkit-gc-foundation/plan.md` is a real example of the
format), so when that convention is in use, place the plan at
`plans/<timestamp>-<slug>/plan.md` instead.

## Output contract

The plan artifact MUST contain exactly these sections, in order:

1. **Goal** — one paragraph: what is being achieved, and why.
2. **Context** — background, linked plans/docs, user decisions, assumptions.
3. **Current Architecture** — how the system behaves today, with file references.
4. **Problem** — the gap or defect the goal addresses.
5. **Proposed Architecture** — the design after the change, with boundaries.
6. **Files to Change** — a table: file path, modify/create/delete, purpose.
7. **Migration** — schema, data, or config migration steps, if any.
8. **Risks** — ranked risks with mitigations and rollback notes.
9. **Test Plan** — the exact tests, builds, and manual checks to run.
10. **Rollback Plan** — how the change is reverted if something goes wrong.
11. **Acceptance Criteria** — checkable conditions that mark the work complete.

## Quality gates

Before you finish, confirm all three:

- **plan_has_goal**: the Goal section states a real, specific outcome.
- **plan_has_acceptance_criteria**: the Acceptance Criteria are checkable, not vague.
- **plan_has_rollback**: the Rollback Plan describes a concrete reversal path.

Do not claim the plan is complete until all three gates pass.