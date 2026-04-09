# Microsoft Teams Channel Implementation

**Date**: 2026-04-06 13:04
**Severity**: Medium
**Component**: Channels (Microsoft Teams)
**Status**: Resolved
**PR**: nextlevelbuilder/goclaw#717

## What Happened

Implemented native Microsoft Teams channel for GoClaw following the Feishu WebhookChannel pattern. Shipped 5 Go files, 19 unit tests, full E2E validation, 2 adversarial security reviews, and architecture docs update.

## The Brutal Truth

This was the cleanest channel implementation we've done because we reused the WebhookChannel pattern — no new architecture, just disciplined execution. But we found 4 runtime bugs only during live Bot Framework testing, which means our test doubles didn't catch real Azure behavior. Took 8 hours to implement + security harden when it should have taken 5 if we'd E2E tested sooner.

## Technical Details

**Deliverables:**
- `internal/channels/teams/{auth, send, webhook, channel, types}.go` (5 files, 600 LOC)
- JWT RS256 validation, JWKS endpoint fallback, token expiry floor
- Config wiring + frontend schemas (TypeTeams constant, TeamsConfig struct)
- 19 unit tests, E2E verified against live Azure Bot Service

**Bugs caught at E2E time:**
1. `tid` claim missing from Bot Framework Connector tokens → skipped validation
2. `*.trafficmanager.net` too broad for serviceURL → restricted to `smba.trafficmanager.net`
3. Config-based channels skipped when `instanceLoader` active → extracted `registerTeamsChannel()`
4. DMPolicy/GroupPolicy never enforced → added `CheckPolicy()` at message time

**Security hardened (2 review rounds):**
- SSRF: serviceURL domain allowlist + JWKS URI validation
- DoS: MaxBytesReader 1MB, HTTP fetch timeouts, JWKS cooldown
- Auth: JWT RS256 only, policy enforcement, BotType enum validation

## What We Tried

1. Reused Feishu WebhookChannel pattern → worked perfectly, saved design time
2. Skipped Bot Framework SDK (too heavy) → REST + JWT only (200 LOC vs 2000+)
3. Text-only MVP (Adaptive Cards deferred) → scope win, unblocked release
4. Config-only registration (no DB InstanceLoader yet) → acceptable tech debt

## Root Cause Analysis

The bugs weren't architectural mistakes — they were assumptions about Azure Bot Framework that only lived services reveal:
- Real tokens omit optional claims (our mock always had them)
- serviceURL domain blocks are more restrictive than docs suggest
- Config vs DB channels interact in unexpected ways when both active

We should have done one live E2E run during implementation, not waiting until final validation.

## Lessons Learned

1. **Test doubles are fragile**: Unit tests passed 100%, E2E found 4 bugs. Live service behavior is the real spec.
2. **Domain allowlists beat wildcards**: `smba.trafficmanager.net` is tighter than intended docs suggest.
3. **Policy enforcement must be explicit**: "CheckPolicy at send time" shouldn't be inferred — document it in channel contract.
4. **Config + DB channels need careful routing**: When both active, registration order matters. Extract named functions for clarity.

## Next Steps

1. Reply path full E2E (blocked on agent bootstrap state, unrelated)
2. serviceURLs sync.Map needs LRU eviction (unbounded memory, deferred)
3. DB InstanceLoader support (larger channels architecture, lower priority)
4. Document Azure-specific gotchas in channels/README

**Owner**: Implementation complete. PR merged as feat/channels-teams. Next PR: reply path E2E.
