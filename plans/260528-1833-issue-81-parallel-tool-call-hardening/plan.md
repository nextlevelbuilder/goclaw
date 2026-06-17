---
title: "Issue 81 Parallel Tool Call Hardening"
description: "Harden existing multi-tool parallel execution so only safe independent read-only calls run concurrently."
status: in_progress
priority: P1
issue: 81
branch: "codex/issue-81-parallel-tool-calls"
tags: [tools, pipeline, parallel, tdd, issue-81]
blockedBy: []
blocks: []
created: "2026-05-28T11:33:10.426Z"
createdBy: "ck:plan"
source: skill
---

# Issue 81 Parallel Tool Call Hardening

## Overview

Issue #81 asks GoClaw to execute independent tool calls in parallel while preserving deterministic transcript order, rate limits, safety, and observability.

Current `dev` already has a parallel path in `internal/pipeline/tool_stage.go`: multi-tool batches call `ExecuteToolRaw` concurrently, then process results sequentially by original index. That is the right architecture. The plan is to harden it, not rewrite it.

Approved decisions:
- Keep the existing two-phase parallel path.
- Parallelize read-only tools only.
- Keep `exec`, `bash`, `wait`, mutating tools, async tools, MCP-bridged tools, and unknown tools sequential by default.
- Use a fixed in-code concurrency default now. Web/UI configurability is out of scope until users need it.

Main gaps to close:
- Parallel path currently skips synchronous `PreToolUse` hooks.
- Any non-`wait` multi-tool batch can enter the parallel path, including mutating tools.
- No fixed batch concurrency cap.
- `MaxToolCalls` is checked per sequential call, but parallel scheduling can overshoot unless gated before launch.

Design summary: [reports/brainstorm-summary.md](./reports/brainstorm-summary.md)

## Phases

| Phase | Name | Status |
|-------|------|--------|
| 1 | [Characterize Current Parallel Semantics](./phase-01-characterize-current-parallel-semantics.md) | Complete |
| 2 | [Harden Eligibility Hooks and Budgets](./phase-02-harden-eligibility-hooks-and-budgets.md) | Complete |
| 3 | [Add Concurrency Cap and Observability](./phase-03-add-concurrency-cap-and-observability.md) | Complete |
| 4 | [Validate Document and Ship](./phase-04-validate-document-and-ship.md) | In Progress |

## Dependencies

- GitHub issue: digitopvn/goclaw#81
- Existing executor: `internal/pipeline/tool_stage.go`
- Pipeline dependency contract: `internal/pipeline/deps.go`
- Loop wiring: `internal/agent/loop_pipeline_adapter.go`
- Tool metadata: `internal/tools/capability.go`, `internal/tools/registry.go`
- Existing regression tests: `internal/pipeline/stages_test.go`

No unfinished project plan currently blocks this plan.

## Success Criteria

- [x] Read-only multi-tool batches execute concurrently with stable result order.
- [x] Mutating, async, MCP/unknown, `exec`/`bash`, and `wait` batches stay sequential by default.
- [x] Sync `PreToolUse` hooks run before any parallel I/O and can block or rewrite arguments.
- [x] Tool-call budget is respected before scheduling a parallel batch.
- [x] Parallel I/O uses a fixed bounded concurrency default.
- [x] Logs/traces expose enough batch metadata to debug concurrent execution.
- [x] Focused unit tests prove both parallel and sequential safety paths.
- [x] No production behavior change for single-tool calls.

## Validation

- `go test ./internal/pipeline`
- `go test ./internal/agent`
- `go test ./internal/tools`
- `go build ./...`
- `go build -tags sqliteonly ./...`
