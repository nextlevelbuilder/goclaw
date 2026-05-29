# Webhook Agent Triggering — Ship Complete

**Date**: 2026-04-21 23:59
**Severity**: Medium
**Component**: HTTP webhooks (inbound) + callback worker (outbound)
**Status**: Resolved

## What Happened

Shipped HTTP webhook API (POST /v1/webhooks/message + /v1/webhooks/llm) with callback delivery. Feature enables external systems to trigger agents synchronously or asynchronously, with outbound result delivery to a caller-specified callback URL. Dual-database (PostgreSQL Standard + SQLite Lite). 48 files, 9376 insertions. Nine sequential phases. Branch: feat/webhook-agent-triggering, commit 19e0c679.

## The Brutal Truth

Red-team review found the plan was unexecutable as written. Two fabricated API methods (`Router.Invoke`, `Manager.SendToChannel` media overload), three wrong file anchors, and four unspecified design decisions (media dispatch scope, callback idempotency, tenant concurrency, i18n ordering) meant that handing this to a teammate would have burned 4+ hours on false starts. After rework (2 hours of planner fixes), the plan was sound and execution was linear. The lesson: "trust-but-verify between planner and live code" is not optional — it catches real bugs before implementation wastes cycles.

## Technical Details

### Shipped contracts

- **POST /v1/webhooks/message**: Send text + media to channel. HMAC-SHA256 auth (X-GoClaw-Signature t=,v1=) + bearer token. Rate limit: per-webhook bucket (token refill 10/sec) + per-tenant global bucket (100/sec). Returns `{webhook_id, call_id}` immediately.
- **POST /v1/webhooks/llm**: Sync (wait for response, 30s timeout) or async (return call_id, deliver result to callback_url). Request body capped 1 MB; metadata capped 8 KB. HMAC + tenant-admin auth gate.
- Callback delivery: exponential backoff [30s, 2m, 10m, 1h, 6h] ±10% jitter, 5 attempts max. Outbound headers carry `X-Webhook-Delivery-Id` (stable across retries) for receiver-side dedupe. Claim uses FOR UPDATE SKIP LOCKED (PG) / BEGIN IMMEDIATE (SQLite).

### Critical decisions

1. **Callback idempotency:** `delivery_id` UUID on `webhook_calls.delivery_id` stays constant across retries. `attempts` counter incremented AFTER send completion (not before), so crash-restart never creates duplicates — receiver sees same delivery_id on retry. This invariant required reversing initial design ("increment on claim").

2. **Media dispatch:** Phase 05a added `channels.SendMediaToChannel()` because reused `SendToChannel(content string)` couldn't carry attachments. Grep found 8 adapters (telegram, discord, whatsapp, feishu, slack, zalo, pancake, facebook) already support `bus.OutboundMessage.Media` — not a new pattern. Phase 05b gates /message on `channels.IsMediaCapable(type)` with 501 fallback if unsupported.

3. **Tenant concurrency:** Per-tenant semaphore (sync.Map keyed by tenant_id → `*semaphore.Weighted`) with 5-minute TTL eviction. Prevents single tenant's callbacks from starving others. Non-blocking `TryAcquire` leaves row unclaimed on failure (no DB busy-loop); next 2s poll retries naturally.

4. **i18n front-loading:** All 19 keys × 3 catalogs (en/vi/zh) added upfront in phase 03, before any handler code. Prevents late-discovery "key not found" crashes. Phase 08 verifies the front-load.

## What We Tried

1. **Initial plan:** Router.Invoke entry point doesn't exist. Real pattern is `Router.Get(ctx, agentID) → Agent.Run(ctx, RunRequest)`, verified at `internal/agent/router.go:93` + `internal/agent/types.go:18`.
2. **Media dispatch design:** Planner assumed Manager.SendToChannel could carry attachments. Grep audit found it only took `content string`. Rework added dedicated `SendMediaToChannel(ctx, channelName, chatID, content, []bus.MediaAttachment)` method.
3. **Auth helpers location:** Plan cited `internal/http/auth.go` which doesn't define `requireTenantAdmin` or `requireMasterScope`. Grep found them at `internal/http/tenant_auth_helpers.go:22,71`.
4. **Edition gating:** Plan referenced nonexistent `edition.Current().Standard` and `.HasChannels()` methods. Rework added `AllowsChannels()` helper at `internal/edition/edition.go`.

## Root Cause Analysis

**Why the plan failed initial audit:** Planner reused API names from pattern prose without grepping live code. "Reuse Router.Invoke" sounded plausible for an entry point; the actual pattern is two-step (Get + Run). "Manager.SendToChannel carries media" was inferred from method naming, not from examining the struct definition. Edition gating was copy-paste from an older codebase pattern that didn't exist here.

**Why we caught it:** Red-team enforced CLAUDE Plan Verification Rule #3 ("no fabricated identifiers") and Rule #1 ("verify factual claims against code"). Spot-checks of 15+ claims against grep/line references surfaced every fabrication before implementation.

**Why rework was surgical, not rewrite:** The architecture (phases, concurrency model, auth gates) was sound. Only the API anchors and medium-sized design decisions needed fixing. Fixes were: (1) cite real entry points, (2) add one new channel method, (3) fix three file paths, (4) resolve four design questions. Execution then followed the reworked plan linearly, no surprises.

## Lessons Learned

1. **Trust-but-verify is load-bearing.** When a planner says "reuse X", don't delegate without a grep audit. Plausible-sounding APIs are the easiest to hallucinate. A 2-hour red-team pass caught what would have been 8+ hours of teammate confusion and rework.

2. **Crash-restart safety via immutable idempotency tokens is non-negotiable for async work.** Original design incremented attempts on claim; rework deferred it to post-send. This single decision eliminates the entire class of duplicate-delivery bugs on worker restart.

3. **Tenant isolation primitives (semaphores, TTL eviction, non-blocking acquire) scale better than ad-hoc limits.** Per-tenant semaphore with idle eviction is more complex than a simple global cap, but prevents the single-tenant-starves-others DoS and works at arbitrary scale.

4. **i18n keys as a blocker step, not a chore.** Front-loading all keys before handler code prevents runtime "key not found" crashes and makes phase dependencies explicit. Ordering matters more than scope.

5. **Anchoring API references is mechanical, not intuitive.** The plan correctly described what needed to be done (webhook auth, callback delivery, rate limiting) but cited wrong files/methods. Grep-by-symbol before writing. "Reuse X" must cite `file:line` and include a short signature snippet.

## Next Steps

1. Merge branch feat/webhook-agent-triggering → dev when CI green (currently in progress).
2. Monitor webhook_calls table cardinality and callback latency in first week post-deploy. Alert if p50 delivery time > 1 min (indicates tenant sem contention or stale reclaim pile-up).
3. v2 scope (deferred): /v1/webhooks/task (trigger workflows with task metadata), admin UI (web + desktop), callback secret rotation with grace window, observability dashboard for webhook metrics.
4. Document webhook integration pattern in `docs/webhooks.md` + provide client library examples (curl, Python, Go) for external systems.
