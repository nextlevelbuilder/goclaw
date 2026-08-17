# Agent D — Phase 4 Module 3 (C7): Client-side dedup (runId, seq) web + desktop

## Files changed

### New (web)
- `ui/web/src/lib/event-seq-dedup.ts` — pure helper: `RunSeqMap`, `runSeqKey(runId, sessionKey?)`, `shouldProcessRunEvent(seen, runId, sessionKey, type, seq, started)`.
- `ui/web/src/lib/event-seq-dedup.test.ts` — vitest unit tests (dedup, gap/out-of-order, old-server compat seq=0/absent, terminal replay, per-session keying, pre-start replay).

### New (desktop)
- `ui/desktop/frontend/src/lib/event-seq-dedup.ts` — same helper, desktop package copy (separate app bundle; no shared package boundary).
- `ui/desktop/frontend/src/lib/event-seq-dedup.test.ts` — vitest unit tests mirroring web.

### Modified (web)
- `ui/web/src/api/ws-client.ts` — `EventListener` gains optional `seq` param; `handleEvent` passes `frame.seq` to listeners. Wildcard `"*"` listeners unchanged (still receive `{event, payload}`).
- `ui/web/src/hooks/use-ws-event.ts` — `useWsEvent` handler type gains optional `seq?: number`; forwards from `ws.on`.
- `ui/web/src/pages/chat/hooks/use-chat-messages.ts` — `seenSeqRef` per-run map (cleared on session switch in existing reset effect); single dedup gate at top of `handleAgentEvent` before any append (chunk/thinking/tool/status/terminal).

### Modified (desktop)
- `ui/desktop/frontend/src/lib/ws-types.ts` — `EventHandler` gains optional `seq?: number`.
- `ui/desktop/frontend/src/lib/ws.ts` — `handleEvent` passes `frame.seq` to handlers.
- `ui/desktop/frontend/src/hooks/use-chat.ts` — `seenSeqRef` per-run map (cleared on session switch); single dedup gate before switch.

## Mechanism

- Key: `${sessionKey}:${runId}` (falls back to bare `runId` when sessionKey absent — announce runs). Scoped per session so colliding run ids never mix.
- seq absent or 0 → always process (old-server compat, current behavior untouched).
- seq <= lastSeq for the run key → drop entirely (no append, no RAF, no store write).
- seq > lastSeq → process; tracking gated on `started` (currentRunIdRef set) OR `type === "run.started"` — so replayed frames arriving before run.started (reconnect replay) never register a stale high seq that would block the real run.started (seq 1).
- Cleanup: entries kept until session switch (map cleared). Simpler and safer than delete-on-terminal: deleting on terminal would re-open the duplicate window (a replayed terminal or trailing chunk after cleanup would be re-processed). Run ids are unique per run, so a kept entry never blocks a later run. No LRU needed.

## Acceptance criteria status

1. Dedup behavior: covered by unit tests on both apps (duplicate seq dropped; gap seq 1,2,4 → 4 processed, stale 3 dropped; seq=0/absent → process as before). Verified in code path: gate sits before append in both hooks.
2. pnpm build: IN PROGRESS — deps installing in background; will run `pnpm build` for web + desktop after.
3. Existing tests: only additive new test files; no existing test touched. Type changes are additive (optional param) — no existing caller broken.

## Why ws-client.ts / ws-types.ts / use-ws-event.ts needed changes

C7 spec listed them as "if needed" — needed, because `seq` lives on the event **frame** (`EventFrame.seq`), not in the payload, and the hooks only receive `payload`. Passing frame.seq through the listener chain was the minimal disclosure (one optional param). Alternative (payload-embedded seq) would break the server contract — payload is agent event shape.

## No-commit compliance

No commit made. Working tree contains only the 6 files above + plan docs (untracked).

Status: IN PROGRESS (build verification pending)
