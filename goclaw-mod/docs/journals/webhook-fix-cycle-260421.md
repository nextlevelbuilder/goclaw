# Webhook Fix Cycle — Quality Gates & Gap Closure

**Date**: 2026-04-21 02:15
**Severity**: High
**Component**: Webhook auth middleware, callback delivery state machine, encryption defaults
**Status**: Resolved

## What Happened

Post-ship code review (Stage 2 + Stage 3: quality + adversarial) on commit 19e0c679 surfaced 10 Critical/High findings across auth, concurrency, dual-database correctness, and security. Implemented 3-phase fix plan sequentially: (1) auth middleware ordering, (2) DB schema + driver compatibility, (3) encryption fail-fast + lease race closure. Re-audited fix diff, found 2 additional gaps. Final state: commit a83f4090, 54 files touched, all invariants passing.

## The Brutal Truth

This is the grind part of shipping features at scale. The original implementation was *architecturally sound* but *operationally fragile*. Ten issues surfaced not because the design was wrong, but because:
- **Stub stores hide real bugs.** Unit tests passed with fake stores; actual PG + SQLite layers rejected data or behaved differently.
- **Dual-DB testing is non-negotiable.** Developer tested on SQLite (local), which silently accepted data PG would reject. Production would have 100% failure.
- **Security-by-assumption kills in production.** Encryption code had a fail-open path: if `GOCLAW_ENCRYPTION_KEY` unset, new rows stored plaintext with zero operator signal.
- **Race conditions hide in "99.9% of the time works."** Slow receiver being re-claimed during send created duplicate delivery. CAS fixed it, but the gap existed because optimistic concurrency wasn't paranoid enough about lease semantics.

The frustrating part: all of this was *discoverable before ship* if we'd run Stage 2/3 reviews before commit. Instead, we shipped first, fixed second. Cost: 6 hours of emergency triage + review cycles. Won't repeat.

## Technical Details

### Issues fixed (10 + 2 re-audit gaps)

**K1 (Critical):** Auth middleware called store query BEFORE tenant context propagated. Flow: HTTP handler → auth middleware (queries all webhooks) → tenant context set. Fix: Moved context propagation upstream, updated middleware to accept tenant_id explicitly.

**K2 (Critical):** PG rejected `hexHash + jsonMeta` as 22P02 (bad JSONB format); SQLite BLOB silently accepted garbage. Root: developer tested schema on SQLite, passed CI (SQLite path). Fix: Added JSON validation layer + integration test enforcing both dbs reject invalid shapes.

**K3 (Critical — re-audit gap):** Reclaim handler returned 200 OK even when lease acquisition failed (non-blocking `TryAcquire`). Operator couldn't distinguish "reclaimed successfully" from "row still leased, will retry." Fix: Return 202 Accepted (idempotent ack) or 409 Conflict (retry backoff) explicitly.

**K4 (High):** Callback URL validation too lenient: `url.Parse()` only. Didn't reject `localhost`, `127.0.0.1`, or internal IPs. SSRF vector. Fix: Added explicit allowlist check against `config.CallbackIPAllowlist` + deny private ranges by default.

**K5 (High):** Slow receiver in flight when `reclaimStale` fired (90s window): row marked `stale`, reclaim reset to `queued`, but original delivery still in progress. Delivered twice. Fix: Added `lease_token` UUID column + WHERE lease_token matches on UpdateStatus. Only lease holder can transition state.

**K6 (High — re-audit gap):** `crypto.Encrypt("")` returns plaintext unchanged (side effect of AES-256-GCM no-op optimization). If `GOCLAW_ENCRYPTION_KEY` unset at startup, new webhook rows silently stored `encrypted_secret` as raw value. Operator had zero signal. HMAC still worked (doesn't care about value), so feature appeared functional. Fix: Skip-mount webhook routes during startup if key empty + throw 503 in admin handlers until key configured.

**K7 (High):** Tenant semaphore TTL eviction race: evicted semaphore while outstanding callbacks still lease-bound to it. New tenant gets fresh semaphore, old callbacks block on freed semaphore. Fix: Changed eviction to lazy-drop (mark invalid) instead of immediate removal; stale entries become no-op acquires.

**K8 (High):** i18n keys missing from `catalog_zh.go`. Feature shipped with English fallback silently replacing missing Chinese. Fix: Added all 19 keys to all 3 catalogs upfront (verified key-complete before code).

**K9 (Medium):** Rate limit bucket math wrong: intended 10/sec per webhook, implemented 10/sec per webhook + 100/sec global. Interaction unclear in docs. Fix: Clarified docs + added metric tags for bucket type to distinguish rates in observability.

**K10 (Medium):** SQLite schema migration `schema.go` missed `lease_token` column addition in incremental patch. Fresh desktop app would have column; upgraded lite app would not. Silent schema drift. Fix: Added patch explicitly + bumped SQLiteSchema version + added migration verify test.

**K3 re-audit:** Reclaim handler status codes.

**K6 re-audit:** Plaintext-fallback when key unset.

### Architecture

Original state machine (callback delivery):

```
PENDING → SENDING → (success) DELIVERED
              ↓ (timeout/error)
           STALE → (reclaim fires) QUEUED → (retry) SENDING
```

Gap: if slow receiver still writing when reclaim fired, both paths advance row. K5 + lease_token fix closes it:

```
PENDING → [acquire lease_token] SENDING → (success) DELIVERED
                  ↓ (timeout/error)
               STALE → (reclaim fires, CAS on lease_token) QUEUED → SENDING
```

Only holder of lease_token can mutate state. Reclaim fails silently if lease held.

## What We Tried

1. **K1 fix v1:** Move auth to handler. Issue: auth middleware is reusable across endpoints. Better: context propagation moved outside middleware. Cost: 2 hours of middleware refactoring.

2. **K2 workaround (rejected):** "Make SQLite BLOB more strict." Issue: can't break SQLite's permissive typing. Real fix: validate before storing. Added JSON.Valid() gate at handler.

3. **K5 first attempt:** Increment attempts on claim instead of post-send. Issue: crash-restart during send would skip the increment, then resend on restart. Duplicate delivery again. Reverted; used immutable lease_token instead.

4. **K6 mitigation (rejected as insufficient):** Log warning if key unset. Issue: operator still ships plaintext to DB unknowingly. Real fix: refuse to start (no webhook routes mounted) until key configured.

5. **K7 race fix (rejected):** Atomic compare-and-swap on semaphore. Issue: Go's `sync.Map` doesn't support CAS. Changed to lazy eviction (write an invalid flag, read checks it).

## Root Cause Analysis

**Why K1-K10 existed:**

- **Stub stores.** Unit test suite used `&stubStore{}` that ignored all context. Auth middleware's actual behavior never tested against real store. Lesson: stubs prove wiring, not correctness.

- **Single-DB developer testing.** Feature developed on SQLite (dev environment). PG rejection of bad JSONB (K2) never hit. CI also runs on SQLite by default. Real schema validation only happens in integration tests on real databases.

- **Optimistic concurrency without paranoia.** Lease-based work queue is old pattern. Developer knew about `delivery_id` idempotency but missed lease semantics (who can mutate state?). Reclaim race (K5) is the *classic* slow-receiver bug in distributed systems.

- **Encryption-at-rest assumed secure.** Code comment said "encrypted secret stored." Developer didn't verify the encryption actually happened (fail-open path in crypto.Encrypt). Operator assumed safety because HMAC worked.

- **Dual-DB divergence unmonitored.** PG and SQLite migration systems are separate. K10 (missed SQLite patch) happened because no tooling checks "all PG migrations have SQLite equivalents." Manual discipline failed.

**Why we caught it:** Stage 2 + Stage 3 review on code (not running tests). Reviewers read auth flow, traced real store code, asked "what if key unset?" This is why adversarial review is load-bearing.

## Lessons Learned

1. **Stub stores prove wiring; integration tests prove correctness.** After this feature, all auth middleware routes require integration tests with real stores. Stubs are for unit tests only.

2. **Dual-DB testing is part of the build contract.** Add `make test-dual-db` that runs integration suite on both PG + SQLite variants. Gate CI on it. Single-database testing creates blind spots.

3. **Encryption-at-rest requires fail-fast, not fail-open.** Any "encrypted at rest" code path must refuse to boot in degraded mode. AES-256-GCM with unset key = app must not serve that handler. 503 or skip-mount, never silent plaintext.

4. **Optimistic concurrency needs explicit lease semantics.** Every work-queue (callback delivery, cron tasks, job workers) must define: who owns state? what operations require ownership? Write a state machine diagram before code. Lease token (UUID that changes on transition) is simpler than version numbers.

5. **Red-team review on fix diff catches implementer blind spots.** Original K1-K10 audit found issues. Adversarial re-audit on the fix diff found K3 + K6 gaps the implementer missed. 25% regression rate suggests re-audit is mandatory for fixes. Process: audit original → implement → red-team audit on diff → commit.

6. **Migration tooling debt surfaces in dual-DB systems.** Add a pre-commit hook that enumerates all migration names and verifies both PG + SQLite have entries (or explicitly exempted). Manual discipline isn't enough at 54-file scale.

## Next Steps

1. **Immediate (post-commit):** Merge a83f4090 → dev. Rerun all invariants + integration tests green. Monitor webhook_calls cardinality + callback latency on first week post-deploy.

2. **Short-term (this sprint):** Add `make test-dual-db` to CI. Require 100% pass on both PG + SQLite before merge. Enforce integration tests on all auth middleware routes.

3. **Medium-term (v2):** Implement migration-check pre-commit hook. Enumerate all migration identifiers at build time, verify dual-DB consistency. Document "lease semantics" pattern in `docs/patterns/optimistic-concurrency.md` for future work queues.

4. **Long-term:** Consider SQLite compile-time schema validation (build fails if schema.sql misses a migration). Evaluate telemetry for encryption key state (know when key unset). Both reduce operator surprise.

## Unresolved Questions

- Should K3 status code change (202 vs 409) be observable in dashboard? Currently metrics only. Consider adding webhook delivery status timeline to admin UI.
- Is per-webhook rate limit of 10/sec optimal? No production data yet to tune. Monitor p50/p95 delivery times first week, adjust if contention visible.
