---
phase: 5
title: "Validation and Issue Handoff"
status: complete
priority: P1
effort: "0.5d"
dependencies: [1, 2, 3, 4]
---

# Phase 5: Validation and Issue Handoff

## Context Links

- Plan overview: `plans/260528-1804-archived-run-timeline/plan.md`
- GitHub issue: `digitopvn/goclaw#76`
- Related issue boundary: `digitopvn/goclaw#67`

## Overview

Run compile/test gates, verify issue #76 acceptance criteria, and update the GitHub issue with the final implementation summary.

## Key Insights

- This is a cross-surface feature: store, agent loop, HTTP, WS, and UI all need verification.
- Lite/SQLite must be explicitly checked because schema drift can break desktop startup.
- #67 behavior must remain absent.

## Requirements

- Functional: verify backend persistence, HTTP API, WS RPC, and web UI behavior.
- Functional: confirm no #67 behavior was implemented accidentally.
- Non-functional: run both PostgreSQL and SQLite/Lite compile gates.
- Non-functional: update issue #76 with plan/implementation result and filepath.

## Architecture

Validation focuses on product behavior:
- timeline stored as product data.
- trace/span IDs linked for debug only.
- archive UI under session detail.
- previews only in Phase 1.

## Related Code Files

- Modify: `docs/18-http-api.md`
- Modify: `docs/19-websocket-rpc.md`
- Modify: `docs/project-changelog.md` if implementation lands.
- Read: `plans/260528-1804-archived-run-timeline/plan.md`

## Implementation Steps

1. Run focused backend tests:
   - stores, HTTP handler, WS method, agent recorder.
2. Run SQLite-specific tests and build.
3. Run full Go compile/vet gates:
   - `go build ./...`
   - `go build -tags sqliteonly ./...`
   - `go vet ./...`
4. Run web tests/build:
   - `cd ui/web && pnpm test -- --run`
   - `cd ui/web && pnpm build`
5. Run `git diff --check`.
6. Manually verify a real or fixture run timeline:
   - block reply appears before tool call/result where emitted.
   - final answer appears once.
   - tool preview only.
   - trace link exists when trace IDs are available.
7. Reply to GitHub issue #76 with summary, validation results, and plan/implementation filepath.

## Success Criteria

- [x] All planned acceptance criteria are checked against tests or manual verification.
- [x] Both HTTP and WS API paths are verified.
- [x] No quick ack or message splitting behavior from #67 is present.
- [x] GitHub issue #76 has a concise update with filepath.
- [x] Any unresolved questions are listed at the end.

## Todo List

- [x] Run focused backend tests.
- [x] Run SQLite/Lite tests and build.
- [x] Run Go build/vet gates.
- [x] Run web tests/build.
- [x] Run `git diff --check`.
- [x] Manually verify timeline behavior.
- [x] Reply to issue #76.

## Risk Assessment

Main risk: validation misses Lite/SQLite regression. Mitigation: treat `go build -tags sqliteonly ./...` and SQLite store tests as blocking.

## Security Considerations

Validation must include permission boundary tests and preview-only assertions. Do not paste sensitive tool payloads into GitHub issue updates.

## Next Steps

After implementation validation, ship via normal branch/PR flow and update issue #76 with commit/PR references.
