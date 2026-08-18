---
name: docs
description: Maintain project documentation — update only when behavior or contracts change, read before editing, verify every claim (architect /gc:docs)
license: Proprietary. Part of GoClaw bundled skills.
version: 1
inputs:
  - doc_change_or_outage
outputs:
  - doc_update
  - claim_verification
allowed-tools:
  - search
  - read_file
  - filesystem
quality-gates:
  - smallest_surface_updated
  - claims_verified
  - links_validated
---

# Docs

Maintain documentation the way it decays least: update the smallest owning
surface, only when the work changes user-visible behavior or durable decisions,
and verify every claim against source before writing it down. Documentation
that contradicts the code is worse than no documentation.

## Purpose

`/gc:docs` produces or repairs documentation. It decides what warrants an
update, finds the smallest surface that owns the change, reads the target
before touching it, and verifies claims against source, tests, scripts, or live
state. Internal edits and phase-completion churn do not warrant evergreen docs
updates.

## Operating Rules

- Update docs only when the work affects user-visible behavior, setup,
  commands, configuration, architecture, security, public contracts,
  machine-readable contracts, or durable maintainer decisions.
- Do not add changelog noise for purely internal edits unless the repo already
  requires it.
- Discover the target through repository instructions, the root README, and the
  existing docs navigation. Do not assume a fixed filename list or docs tree.
- Before updating a document, read it. After updating, verify links and claims
  against source, tests, scripts, artifacts, or live state.
- Link to machine-owned scripts, manifests, schemas, or generated references
  instead of copying their details into prose.
- Plans, reports, and audit results are stateful records. They do not become
  evergreen product authority merely because a phase completed.

## When to update

Update when any of these changed and are user-facing or contract-bearing:

- Commands, flags, config keys, env vars, or setup steps
- API contracts, request/response shapes, WebSocket methods
- Architecture decisions, module boundaries, data flow
- Security posture, auth boundaries, isolation guarantees
- Build, release, CI/CD, or deployment workflow

Do NOT update for: cosmetic rewording, internal refactors with no observable
change, or phase bookkeeping that the repo's plan index already records.

## Workflow

1. **Scope the trigger** — identify the change (feature, fix, config, contract)
   and list every affected surface. State which surfaces are NOT affected and
   why, if the change could plausibly touch them.
2. **Find the smallest owning surface** — locate the doc(s) that own the
   affected behavior: README, `docs/`, module-level docs, API reference,
   migration notes. Prefer the smallest set that covers the change.
3. **Read before editing** — read the target document in full first. Never
   patch a doc you have not read.
4. **Verify claims** — for every fact you write, trace it to source: a code
   path, a test, a script, a manifest, or live state. Do not copy a claim from
   another doc unless that doc's claim is itself verified. Grep and re-count
   paths, endpoints, and numbers.
5. **Update** — make the minimal edit on the owning surface. Link to generated
   or machine-owned references instead of duplicating them.
6. **Validate** — check every link you touched resolves, every path exists,
   every command in the doc runs, and every number still matches source.

## Quality gates

Confirm all three before finishing:

- **smallest_surface_updated** — the edit landed on the smallest owning
  surface; no broader doc was churned for the same change.
- **claims_verified** — every claim in the change is traced to source, not
  copied from another unverified doc.
- **links_validated** — all links and paths in the edited section resolve.

## Output

Report the trigger, the surfaces assessed, the docs updated (with the exact
edits), the verification performed, and any surfaces left unchanged with the
reason. Do not claim the update complete until the gates pass.
