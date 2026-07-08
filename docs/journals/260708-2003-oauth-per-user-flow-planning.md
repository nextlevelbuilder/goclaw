# Bitrix24 Per-User OAuth Flow: Planning Complete

**Date**: 2026-07-08 20:03
**Severity**: High
**Component**: Bitrix24 channel, OAuth, provisioning, MCP credential minting
**Status**: Pending Implementation (6-phase plan drafted, all decisions locked)

## What Happened

Bitrix24 support confirmed via official investigation (2026-07-08) that the `ONIMBOTMESSAGEADD` webhook no longer attaches `auth[access_token]` for new users — this is **expected behavior, not a regression**. Imbot subscriptions bind with `USER_ID=0` at registration time, so Bitrix never requests/includes a real OAuth token in the top-level `auth` block; only the bot's own token (`data[BOT][botID][AUTH]`) is guaranteed present.

**Proof**: goclaw's `mcp_user_credentials` table shows 3 users (614, 1, 610) successfully minted credentials between 2026-05-07 and 2026-05-25. Zero new users since. DB + support confirmation align perfectly.

**Consequence**: New staff users who message the bot cannot authorize CRM access via webhook token. goclaw must implement a self-serve OAuth re-auth flow: detect when a user has no `mcp_user_credentials` row OR their stored `refresh_token` is dead, then DM them a signed OAuth authorize link.

## The Brutal Truth

This is a hard block on goclaw's Bitrix integration expanding to new staff. We shipped assuming Bitrix would ship the user's token in every webhook event. It doesn't. The issue isn't a bug we can wait out — it's by design. We should have pressure-tested the webhook event schema against real multi-user tenants months ago instead of assuming Bitrix's imbot behavior matched simpler channel patterns.

The good news: the fix is **stateless**. We reuse nearly all existing infrastructure (provisioner checks, autoOnboard flow, state codec). ~230 LOC, no migration, no new tables.

## Technical Details

**Root cause verification:**
- `provisioner.go:222` queries `GetUserCredentials(userID)` on every message — results show `existing == nil` for new users.
- Bitrix24 support (agent Aleksei, 2026-07-08): imbot subscription (not user-initiated auth) binds to `USER_ID=0`; OAuth token only appears in webhook if user logs in separately (not the case here).
- Imbot events carry `data[BOT][botID][AUTH]` (bot's own token) but never a user `auth[access_token]` at the top level.

**Implementation shape (per `design.md` v2):**
- **Trigger v2 (precise):** Changed from "auth is empty" → `existing == nil` (no prior credential row). Separate branch for existing-but-dead tokens (`invalid_grant`/`expired_token`/`NO_AUTH_FOUND` in `APIError.Code`).
- **Delivery channel (locked):** DM user 1-1 (`imbot.v2.Chat.Message.send` with `dialogId = userID`); Bitrix auto-opens private dialog. Optional hint in original group chat if message came from group. Fallback to group if DM send fails.
- **State storage:** Stateless HMAC-signed state (base64url JSON + hex SHA256 signature). No `oauth_pending_states` table. 10-min TTL + 1-time-use.
- **Token lifecycle:** User clicks link → OAuth callback verifies identity (`token.user_id == state.user_id`) → upserts `mcp_user_credentials` (reuses `SetUserCredentials` via `autoOnboard` path). Existing rows are overwritten, not deleted — `mcp_user_credentials` unique constraint `(server_id, user_id, tenant_id)` makes upsert safe.

**Decisions finalized (§9, design.md):**
1. Delivery channel: DM (1-1), not group reply ✓
2. TTL: 10 minutes ✓
3. Debounce: 5 min/user, separate map from `notifyUserOfMCPIssueOnce` ✓
4. OAuth scope: Not a decision — Bitrix `/oauth/authorize/` doesn't accept `scope` param; portal app's registered scope applies in full ✓
5. Callback route: Public (`GET /bitrix24/oauth/user/callback`), same security pattern as `/bitrix24/install`, stateless HMAC+TTL makes it safe ✓

## What We Tried

1. **Initial assumption (hypothesis):** Bitrix webhook shipping empty token = transient regression. **Failed** — support confirmed it's by design.
2. **Escalation to Bitrix24 support (2026-07-08):** Raised ticket with DB evidence (3 users, cutoff 2026-05-25). Support replied with 4-point technical explanation. Investigation was correctproof.
3. **Parallel design brainstorm (multiple rounds, 2026-07-08):** Workshopped v1 design (weak on trigger condition, reply in group) → v2 refactor (precise trigger via `existing == nil`, DM-only delivery, error classification). User feedback corrected two major UX decisions: (a) "delete then recreate" credential row → revealed as unnecessary churn (upsert handles it), (b) "reply in original dialog" → switched to DM for privacy.
4. **Scope verification (design.md §9 Q4):** Checked official Bitrix OAuth docs (`Complete OAuth 2.0 Authorization Protocol` via `b24-dev-mcp`). Confirmed: `/oauth/authorize/` takes only `client_id`, `response_type`, `state`, `redirect_uri`; no `scope` param. Portal scope is fixed per app registration. Eliminated that as a planning decision.
5. **i18n thread discovery:** Initially drafted Phase 5 as catalog integration (en/vi/zh) per root CLAUDE.md convention. **Verified against code** — bitrix24 channel doesn't thread locale (`provisioner.go:472-486`, documented in comment). Corrected plan to hardcoded Vietnamese string matching existing `mcpUserNotifyMessage` pattern, avoiding unplanned i18n infrastructure.

## Root Cause Analysis

The blocking issue is a **design-webhook mismatch**:
- goclaw designed for "user-centric OAuth" (assume webhook carries user token every event).
- Bitrix imbot designed for "app-centric OAuth" (bot requests OAuth once at install; per-user tokens require separate auth flow).

This gap surfaced only when real multi-user tenants started messaging. Single-user dev/staging never caught it.

**Why it hurt:** We iterated on CRM features (agent loops, deal sync) assuming provisioning would just work for all staff once one user got it working. It doesn't. This is a hard floor blocking the entire "staff collaborate via bot" thesis.

## Lessons Learned

1. **Pressure-test assumptions against external APIs early.** Bitrix's OAuth model (app-centric vs user-centric) should have been verified against real webhook captures in Q2, not discovered mid-Q3 via support ticket.

2. **Stateless state codec > DB state table.** HMAC-signed state + TTL eliminates schema complexity and janitor jobs. We should apply this pattern more aggressively.

3. **Separate error paths by root cause, not just HTTP code.** `APIError.Code` (already present in code) lets us distinguish "token genuinely dead" (`invalid_grant`) from "network hiccup" (5xx). This precision unlocks targeted UX (re-auth vs retry).

4. **UX beats infrastructure.** The initial design's "delete + recreate" credential row looked clean (full reset), but user insight that it's wasteful churn forced the right call (upsert). Code quality often takes a back seat to user friction.

5. **i18n threading is infrastructure debt.** The false start on adding locale-threading to a channel that doesn't use it nearly bloated the scope. Verify existing infrastructure before reaching for general-purpose solutions.

## Next Steps

**Pending tasks:** 6 phases, dependency-chained, all tasked out:

1. **Phase 01: OAuth State Codec** — implement HMAC-signed state with TTL (`oauth_state_codec.go` + tests). Deliverable: stateless encode/decode with security guarantees.
2. **Phase 02: OAuth Callback Handler** — implement `/bitrix24/oauth/user/callback` route, code+token exchange, identity validation (`oauth_user_flow.go` + tests). Deliverable: secure exchange logic reusing `Portal.Exchange` + identity guard.
3. **Phase 03: Provisioner Trigger Branches** — split `provisioner.go::provisionIfMissing` at line ~280. Case 1: `existing == nil` (first-time) → build authorize URL. Case 2: existing but `selfRefreshUserCreds` fails with `invalid_grant`/`expired_token`/`NO_AUTH_FOUND` → also trigger re-auth (same flow, no row delete). Deliverable: precise two-path branching with error classification.
4. **Phase 04: Handler DM Delivery** — catch `ErrUserAuthRequired` in `handle.go`, send DM via `imbot.v2.Chat.Message.send` (dialogId=userID), fallback to group on error, debounce 5 min/user. Deliverable: end-to-end user notification with fallback.
5. **Phase 05: Message Strings** — hardcoded Vietnamese strings matching existing channel pattern (no i18n threading). Deliverable: finalized copy for authorize link prompt + rejection/expiry messages.
6. **Phase 06: Tests** — unit coverage (state codec round-trip, HMAC, TTL expiry, identity mismatch rejection), integration (full flow end-to-end with mocked Bitrix OAuth endpoints). Deliverable: >85% coverage, no integration-only gaps.

**Blockers:** None. All decisions locked. Code can begin immediately after this planning entry.

**Timeline estimate:** 10 hours (phases + review + merge). Phases 1–2 can parallelize; 3–4 depend on 1–2; 5–6 span all.

**Files touched:** ~6 (2 new, 4 modified). No migrations. No external API changes.
