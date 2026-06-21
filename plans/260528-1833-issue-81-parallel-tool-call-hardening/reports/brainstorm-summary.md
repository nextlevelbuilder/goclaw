---
title: "Issue 81 Parallel Tool Call Hardening Brainstorm Summary"
status: approved
source: ck:brainstorm
issue: 81
---

# Issue 81 Parallel Tool Call Hardening Brainstorm Summary

## Problem

GoClaw should reduce latency by running independent tool calls concurrently. Current `dev` already has a parallel path, but it is too broad: any multi-tool batch except `wait` can parallelize, sync `PreToolUse` hooks are skipped, and there is no fixed raw-execution concurrency cap.

## Requirements

- Preserve sequential execution when calls depend on earlier results.
- Run safe read-only multi-tool batches concurrently.
- Keep deterministic result order in transcript/UI.
- Respect tool budget, rate limits, hooks, and cancellation.
- Avoid parallelizing destructive operations by default.
- Make behavior debuggable in logs/traces.

## Approved Decisions

- Harden existing implementation, do not rewrite.
- Parallel eligibility is read-only tools only.
- `exec`, `bash`, `wait`, mutating tools, async tools, MCP/unknown tools stay sequential by default.
- Use a fixed default concurrency limit now.
- Web UI/configurability for concurrency is deferred until user demand exists.

## Evaluated Approaches

| Approach | Verdict | Reason |
|---|---|---|
| Eligibility guard only | Reject | Too narrow; leaves hook and cap gaps. |
| Hardened existing executor | Accept | Smallest change that satisfies safety and acceptance criteria. |
| Full dependency planner | Reject | Over-engineered; dependency inference from tool args is unreliable. |

## Recommended Solution

Keep `ToolStage` two-phase design:

1. Preflight sync hooks sequentially.
2. Block or rewrite calls before scheduling.
3. Check every remaining call is parallel-eligible.
4. Enforce budget before launch.
5. Run raw I/O with fixed semaphore limit.
6. Process results sequentially in original order.

## Risks

- Some tools inferred read-only may still mutate internal caches. Mitigate by conservative eligibility and explicit exclusions.
- Concurrency tests may flake. Mitigate with controlled channels and atomic peak counters.
- Logs could leak data. Mitigate by logging counts, names, ids, durations, and `args_len`, not raw args.

## Validation Criteria

- Read-only multi-tool batch proves concurrent raw execution.
- Mutating/exec/wait/MCP/unknown batches prove sequential fallback.
- Hook block/rewrite proves no raw execution happens before sync hook approval.
- Result order stays stable.
- Fixed concurrency cap is enforced.
- Focused tests and build checks pass.

## Unresolved Questions

None.
