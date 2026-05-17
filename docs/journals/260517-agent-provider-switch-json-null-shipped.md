# Agent Provider Switch JSON Null Fix

**Date**: 2026-05-17 21:52
**Severity**: High
**Component**: Agent persistence, provider/model switch flow, PG + SQLite stores
**Status**: Resolved

## What Happened

Saving an agent after switching provider or model failed because `chatgpt_oauth_routing:null` was preserved as a typed JSON nil/null and then written into config columns that are `NOT NULL`. The update path did not normalize empty or null-ish agent JSON config before hitting the store, so a routine UI action turned into a hard persistence failure.

## Assessment

This was a persistence-boundary bug. The UI action was routine, but the backend trusted a JSON shape that was not safe to persist. A provider/model switch could therefore write invalid config straight into the database. The fix belongs at the store boundary because HTTP, WebSocket, and future clients can all send equivalent null-ish JSON states.

## Technical Details

- Root trigger: `chatgpt_oauth_routing:null` survived as typed JSON null instead of being coerced away.
- Failure mode: store write hit `NOT NULL` JSON config columns in both PostgreSQL and SQLite.
- Fix: added store-level coercion for nil, empty, and JSON-null agent config updates in both PG and SQLite paths.
- Scope: persistence layer only; no product behavior change beyond accepting the update safely.

## What We Tried

1. Reproduced the failure in the runtime Docker stack to confirm it was not a UI-only issue.
2. Added focused store tests around nil/empty/JSON-null config updates.
3. Ran sqliteonly tests to catch the desktop path as well as PG.
4. Rebuilt Go targets and ran focused HTTP tests to verify the save flow end to end.

## Root Cause Analysis

The real mistake was letting typed JSON nulls flow through as if they were valid update payloads. The persistence layer assumed the caller had already normalized config state. That assumption was wrong for provider/model switch flows, where partial or empty config is normal and must be coerced before storage.

## Lessons Learned

1. Persisted JSON needs normalization at the store boundary, not just at the API boundary.
2. Nil and JSON null are not harmless variants when columns are `NOT NULL`.
3. PG and SQLite both need the same coercion logic or the bug just moves between editions.

## Next Steps

- Done: pushed branch `fix/agent-provider-switch-json-null`.
- Done: commits `324d9cf6` and `3da7ca51` included in the beta PR.
- Done: issue `#1148` linked for closure by the PR.
- Watch the next beta for any config-shape regressions around agent save flows.
