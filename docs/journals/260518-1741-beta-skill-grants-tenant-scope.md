# Beta Skill Grants Tenant-Scope Hardening

**Date**: 2026-05-18 17:41
**Severity**: High
**Component**: Skills grants, tenant isolation, skill management UI/API
**Status**: Resolved

## What Happened

This beta ship started from PR #14, which added per-agent skill manage grants so an authorized agent can edit/delete skills even when `owner_id` no longer matches its current actor identity. The review caught the ugly part: the grant path could mutate skill visibility or grant records across tenant boundaries, and the UI/API exposed `owner_id` in skill responses where clients did not need it. Issue digitopvn/goclaw#15 tracks the hardening fallout.

## The Brutal Truth

This was a permission feature shipped close to a tenant-isolation fault line. That is always where mistakes hurt most. The exhausting part is that the baseline feature was useful, but the first version trusted grant inputs too much. A beta is exactly where this should be caught, but it still feels bad because cross-tenant mutation risk is not a cosmetic bug.

## Technical Details

- PG now validates grant scope before insert/update in `internal/store/pg/skills_grants.go:19` and `verifySkillGrantScope` rejects mismatched agent tenants as `agent not found`.
- SQLite mirrors the same check in `internal/store/sqlitestore/skills_grants.go:25`.
- Cleanup migration `migrations/000067_skill_agent_grants_scope_cleanup.up.sql:1` deletes `skill_agent_grants` rows where `sag.tenant_id <> a.tenant_id` or non-system skill tenant differs.
- `internal/store/skill_store.go:19` hides `OwnerID` with `json:"-"`, stopping owner IDs from leaking through skill responses/UI.
- Changelog updated under `CHANGELOG.md`.

## What We Tried

- Kept PR #14's per-agent `can_manage` model because it directly solves agent-owned skill maintenance without pretending `owner_id` is stable agent identity.
- Rejected UI-only filtering because it would not protect HTTP, WebSocket, import, or future callers.
- Added store-layer verification in both PG and SQLite because that is the point every grant mutation must cross.

## Root Cause Analysis

We shipped the first feature around ownership semantics without enforcing the tenant relationship at the same boundary as the write. The fundamental mistake was treating authorization and scope as already resolved by callers. In a multi-tenant system, that assumption is how one tenant ends up mutating another tenant's visibility state.

## Lessons Learned

Grant writes must prove both sides of the relationship: the skill and the agent. Do not rely on caller discipline. Also, response structs should not expose security-sensitive ownership fields just because the database row has them.

## Next Steps

- Owner: maintainers. Monitor digitopvn/goclaw#15 through beta.
- Owner: release lead. Verify beta users receive migration `000067` and no stale cross-tenant rows survive upgrade.
- Owner: reviewers. Treat future grant/import/export changes as tenant-scope sensitive by default.
- Tests/build are passing for this ship; keep PG, SQLite, and Web UI build checks required before promoting beyond beta.
