# Claude CLI Collaborative Audit Matrix

## Purpose

This matrix is the Phase 2 working artifact for issue `#596`. It exists to convert the Claude CLI hardening discussion from intuition into reproducible evidence.

## Scope

- Collaborative repository workflows in GoClaw
- Claude CLI provider behavior under shared-repo pressure
- Session, workspace, auth, and routing boundaries that affect correctness and safety

## Method

Each scenario should record:

1. Setup details
2. Reproduction steps
3. Expected invariant
4. Actual observed behavior
5. Severity and disposition

## Scenario Matrix

| ID | Scenario | Focus | Expected invariant | Evidence to capture | Severity | Status |
|----|----------|-------|--------------------|---------------------|----------|--------|
| A1 | Single user, one repository, one Claude CLI session | Baseline behavior | Session state stays isolated to the selected repo and user intent | Request log, workspace path, session identifiers, tool access footprint | pending | pending |
| A2 | Group chat or multi-user collaboration on one repository | Shared-repo safety | One user must not silently inherit another user's Claude CLI state, auth, or unintended filesystem scope | Prompt transcript, session routing path, workspace sharing markers, repo diff | pending | pending |
| A3 | Concurrent Claude CLI runs against the same repository | Concurrency | Concurrent work must serialize or fail safely without corrupting session history or tool state | Timeline, locks or queues, output interleaving, resulting repo state | pending | pending |
| A4 | Separate repositories on the same host | Cross-repo isolation | Repo A state must never leak into Repo B through session files, MCP bridge, or workspace reuse | Workspace roots, session storage locations, tool call targets, repo diff | pending | pending |
| A5 | Auth loss during active work | Account health | GoClaw must detect degraded auth and stop sending unsafe work or route it per policy | Auth status transition, user-visible state, retry behavior, queued work outcome | pending | pending |
| A6 | Resume, reset, and restart behavior | Lifecycle safety | Resume/reset must preserve intended isolation boundaries and remove stale session coupling | Restart steps, restored session metadata, post-reset behavior | pending | pending |
| A7 | Per-user credential override or future pool-member override | Credential routing | Credential selection rules must be explicit, deterministic, and observable | Config source, selected account, fallback path, user-visible status | pending | pending |
| A8 | MCP bridge and tool boundary review | Effective isolation | Session files alone are not enough; bridge and workspace/tool permissions must align with repo scope | Bridge config, workspace mounts, tool permission decisions, filesystem reach | pending | pending |
| A9 | Failure mode when eligibility is unknown | No-send rule | Ambiguous account or routing eligibility should degrade to a no-send or blocked state, not silent best effort | Eligibility check path, user-visible error, absence of outbound request | pending | pending |

## Severity Guide

- `critical`: user data or repo integrity can cross tenants, users, or repositories
- `high`: unsafe or misleading behavior under normal collaborative usage
- `medium`: recoverable but still harmful ambiguity or operator burden
- `low`: documentation, observability, or UX gap with limited safety impact

## Notes

- Do not close `#596` without filling the Evidence column for every applicable scenario.
- If one scenario reveals multiple failure modes, add a follow-up row instead of overloading the original entry.
