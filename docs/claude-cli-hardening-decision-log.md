# Claude CLI Hardening Decision Log

## Purpose

This log is the maintainer record for the `#595` to `#599` series. Each entry should capture what was decided, why it was decided, and what evidence still needs to be collected.

## Current Stream

- Branch: `kai/feat/claude-cli-hardening-595-series`
- Worktree: `/Users/kaitran/worktrees/goclaw-kai-feat-claude-cli-hardening-595-series`
- Base: local `main`, fast-forwarded to `upstream/main` at `1a2d5789`
- Series: `#595`, `#596`, `#597`, `#598`, `#599`

## Environment Snapshot

| Field | Value |
|-------|-------|
| Repo root | `/Users/kaitran/worktrees/goclaw-kai-feat-claude-cli-hardening-595-series` |
| Branch | `kai/feat/claude-cli-hardening-595-series` |
| Base commit | `1a2d5789` |
| Local env file | `.env` copied from `.env.example` during worktree creation |
| Go toolchain | `go1.26.0 darwin/arm64` |
| Claude CLI version | `2.1.90 (Claude Code)` |
| Claude auth state | pending capture |
| Runtime mode under test | local worktree on macOS (`darwin/arm64`) |
| Docker mode under test | Docker CLI unavailable on this host (`command not found`) |

## Decisions

### 2026-04-01: Treat `#595` as one serial, not five parallel branches

- Decision: Use one dedicated branch and one dedicated worktree for `#595` through `#599`.
- Why: The child issues are dependency-linked. Splitting them would create artificial boundaries, duplicated audit work, and merge churn.
- Evidence: Issue-series analysis completed on 2026-04-01 and the worktree was created before implementation started.
- Follow-up: Keep child issues as checklist items in one draft PR until the baseline is stable.

### 2026-04-01: Audit before hardening

- Decision: Start with an explicit audit matrix before code changes.
- Why: The main uncertainty is current behavior under collaborative repo usage, not the existence of possible fixes.
- Evidence: `docs/claude-cli-collab-audit-matrix.md` created in this branch as the controlling Phase 2 artifact.
- Follow-up: Fill every scenario row with real evidence before deciding whether isolation, health, or pooling work changes code.

### 2026-04-01: Default stance is no native Claude account pooling in the first rollout

- Decision: Assume no built-in pooling for the first production baseline unless the audit proves one-account-per-instance is the blocking bottleneck.
- Why: Pooling increases routing, health, and observability complexity. It should be justified by measured pressure, not intuition.
- Evidence: Prior repo analysis showed Claude CLI does not currently have the Codex/OpenAI-style routing vocabulary needed for a safe pool.
- Follow-up: Revisit in `#599` after the audit, isolation, and failover model are concrete.

## Open Questions

- What exact filesystem and MCP-bridge boundaries are in effect for Claude CLI sessions today?
- Is auth status global, per provider, or effectively per backing account in all relevant paths?
- What operator-visible signal should represent degraded or ambiguous Claude eligibility?
- Which parts of the isolation contract belong in docs, runtime validation, and tests?
