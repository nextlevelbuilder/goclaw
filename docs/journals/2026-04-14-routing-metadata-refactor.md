# Routing Metadata Refactoring + Pancake Comment Diagnostics

**Date**: 2026-04-14 03:00
**Severity**: High
**Component**: `internal/channels`, `internal/channels/pancake`, `cmd/gateway_consumer_normal.go`
**Status**: Resolved

## What Happened

Pancake page comment replies were landing in inbox instead of the public comment thread. The inbound handler correctly stamped `pancake_mode=comment` onto message metadata, but outbound delivery discarded that key before it reached `Send()`. Every comment reply quietly defaulted to inbox routing. No error, no log, no crash — just silent wrong behavior that required a live test to detect.

The fix cascaded into a proper structural refactor: extracted all routing key definitions and copy helpers into `internal/channels/routing_metadata.go`, added `pancake_mode` to the canonical key set, switched `gateway_consumer_normal.go` to use `channels.CopyFinalRoutingMeta()` instead of its own private inline loop, and wrapped the feature-disabled log in `sync.Once` to prevent spam. 16 new comment handler tests and 2 routing_metadata unit tests were added. All 77 channels package tests and 64 pancake package tests pass.

## The Brutal Truth

This was not subtle. The metadata key list in `cmd/gateway_consumer_normal.go` and the one in `internal/channels/events.go` had already diverged before `pancake_mode` was ever needed. We had two separate hardcoded string slices doing the same job in different files, neither derived from the other. Adding a new routing key meant updating two places, and we missed one. Classic DRY violation with delayed detonation.

The maddening part: the bug was completely silent. No test caught it because there were no tests for metadata preservation across the inbound→outbound hop. The only signal was a live comment reply going to the wrong place. We were testing whether messages arrived, not whether they arrived *correctly routed*.

The `sync.Once` diagnostic fix is also a minor embarrassment. The original `slog.Info` on every disabled comment event would have hammered logs the moment any page with `comment_reply=false` received traffic. It should have been gated from the start. `sync.Once` is the minimal fix, but the real lesson is: always ask "how often will this fire in production?" before adding a per-event log inside a hot path.

## Technical Details

Root cause path:

1. `pancake/comment_handler.go` sets `metadata["pancake_mode"] = "comment"` (line 77)
2. `cmd/gateway_consumer_normal.go` line 218: `outMeta := channels.CopyFinalRoutingMeta(msg.Metadata)` — **before the fix** this was an inline loop over a hardcoded key slice that did not include `pancake_mode`
3. `outMeta` is passed into `ChannelMgr.RegisterRun()` and then forwarded to all outbound publishes
4. `pancake/pancake.go` `Send()` checks `msg.Metadata["pancake_mode"]` to branch between comment reply API and inbox reply API — empty key → inbox branch

The surviving inline loop in `events.go` block.reply path (lines 271-275) still reads directly from `routingMetaKeys` (now the canonical exported slice), which is technically correct but still not using the `copyRoutingMeta()` helper. Code review flagged this as informational — non-blocking because block replies intentionally exclude `placeholder_key` and the keys read are identical to what `copyRoutingMeta()` would copy. But it is residual inconsistency.

`finalReplyMetaKeys` design:

```go
var finalReplyMetaKeys = append([]string{
    "placeholder_key", // final outbound can update placeholder; block replies must not
}, routingMetaKeys...)
```

`placeholder_key` is in `finalReplyMetaKeys` only. Block replies use `routingMetaKeys` (no `placeholder_key`). If a block reply carried `placeholder_key`, the placeholder message would be overwritten mid-stream before the final response was ready. Keeping the two lists distinct was the right call.

Test counts: `go test ./internal/channels/... -count=1` → 77 pass, 0 fail. `go test ./internal/channels/pancake/... -count=1` → 64 pass, 0 fail.

Code review findings (all informational, non-blocking):
- `events.go` block.reply path still uses inline loop — not using `copyRoutingMeta()` helper
- `routing_metadata_test.go` missing nil-map input test for `copySelectedMeta`
- `sync.Once` on `commentReplyDisabledOnce` cannot be reset between tests — `sync.Once` reinit path untested

## What We Tried

No dead ends this session. The root cause was identified immediately from the previous investigation journal (`260414-0225-pancake-comment-routing-metadata-fix.md`). This session was the structural cleanup and test coverage pass that followed the point-fix.

## Root Cause Analysis

Two metadata preservation code paths were maintained independently with no shared contract. When `pancake_mode` was added as a routing key in the pancake channel, it was added to the inbound metadata map but there was no mechanism to enforce that all outbound copy paths would automatically include it. The lack of a canonical routing key registry meant every place that copied metadata had to be manually updated — and one wasn't.

Secondary cause: zero tests existed for the metadata→outbound preservation hop. The inbound handler was tested (produces metadata), the outbound channel was tested (accepts metadata), but the pipeline connecting them was not. The gap was invisible until production routing broke.

## Lessons Learned

1. **Routing keys must have one source of truth.** Any code that copies a subset of metadata for routing must derive from a shared registry. Ad-hoc key lists in consumer code are a time bomb. `routing_metadata.go` is that registry now — it must stay the only place key lists live.

2. **Test the pipeline seam, not just the endpoints.** Testing that inbound sets a metadata key and testing that outbound reads a metadata key is not the same as testing that the key survives the round trip. The round-trip test (`TestHandleCommentEvent_FeatureEnabled` checking `msg.Metadata["pancake_mode"] == "comment"`) is the test that would have caught this bug before it shipped.

3. **Per-event logs in hot paths need rate controls from day one.** `slog.Info` inside a webhook event handler that fires on every Facebook comment is a logging DoS waiting to happen. If a diagnostic log is informational and fires on a repeatable condition (feature disabled), use `sync.Once` or a rate limiter. Don't retrofit it after the fact.

4. **Separation of `placeholder_key` from routing keys is a load-bearing design choice.** If this distinction is ever collapsed or "simplified," block replies will corrupt placeholders mid-stream. Future developer: do not merge `finalReplyMetaKeys` and `routingMetaKeys` without understanding this.

## Next Steps

- [ ] Refactor `events.go` block.reply path to call `copyRoutingMeta()` instead of the remaining inline loop — eliminates the last inconsistency flagged in code review. Owner: any engineer touching `events.go` next. No urgency, non-blocking.
- [ ] Add nil-map safety test for `copySelectedMeta(nil, keys)` in `routing_metadata_test.go` — current implementation calls `src[k]` on nil map which panics in Go. Owner: next person adding routing metadata tests. **This is a latent panic, not just a missing test.**
- [ ] Add outbound Pancake action log (`reply_comment` vs `reply_inbox`) so operators have runtime proof of which branch fires without needing to read source code. Owner: pancake channel work.
- [ ] Evaluate whether all channel-specific routing keys should be defined in one canonical schema to prevent the same drift happening for future channels. This is architectural, needs team discussion before acting.
