# Issue 118 Plan Redteam

Plan: `plans/260531-1555-issue-118-llm-generated-channel-progress/plan.md`
Date: 2026-05-31

## Findings

### Fixed

1. Persistence wording was too absolute.
   - Evidence: `internal/agent/run_timeline_recorder.go:137` already maps existing `block.reply` events to assistant-message timeline items.
   - Risk: plan could promise "no progress messages are stored" while existing event recorder already handles `block.reply`.
   - Fix: plan now says this issue adds no new persistence and does not broaden recorder behavior.

2. `off` mode semantics could accidentally disable explicit `gateway.block_reply`.
   - Evidence: `internal/channels/events.go:276` currently gates block replies by `RunContext.BlockReplyEnabled`, independent from chat behavior.
   - Risk: quick ack mode `off` could be over-implemented as a global block reply kill switch.
   - Fix: phase 02 now says `off` only disables chat-behavior quick acknowledgement; explicit `gateway.block_reply=true` must keep working.

3. Legacy nil mode behavior needed a firm decision.
   - Evidence: current `QuickAckConfig` has no mode in `internal/config/config_channels.go:11`.
   - Risk: implementation could infer old fixed-template behavior from non-empty `templates`, silently preserving the old default.
   - Fix: phase 02 now says nil/empty mode resolves to `llm_generated`, even when legacy configs have templates. This is the requested default change.

## Remaining Risks

- Generated progress cannot be guaranteed before tool execution without a separate LLM call. Plan states this explicitly.
- Fallback template and later generated progress may both be visible in long runs. Implementation must keep this bounded by existing one-ack behavior plus existing `block.reply` semantics.
- UI work may push `behavior-chat-card.tsx` past 200 lines. Phase 04 calls for a focused split if needed.

## Verdict

Plan is ready to implement after the fixed findings above.

## Open Questions

None.
