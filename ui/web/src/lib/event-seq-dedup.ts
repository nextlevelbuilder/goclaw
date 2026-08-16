/**
 * Per-run WebSocket event sequence dedup.
 *
 * The gateway assigns an increasing `seq` (> 0) to every agent event frame of a
 * run (chunk / thinking / tool / status). On reconnect the same frame may be
 * delivered twice; stores track the highest seq seen per run key and drop
 * frames whose seq is at or below it. Frames without a seq (older servers)
 * are always processed — compat path.
 *
 * Key: `${sessionKey}:${runId}` — scoped per session so run ids that happen to
 * collide across sessions never mix.
 *
 * Entries are kept until the run's tracking map is reset (session switch):
 * deleting on a terminal event would re-open the duplicate window — a
 * replayed terminal or trailing chunk after cleanup would be processed again.
 * Run ids are unique per run, so a kept entry never blocks a later run.
 */

export type RunSeqMap = Map<string, number>;

/**
 * Build the dedup key for a run-scoped agent event.
 * `sessionKey` fallback: some events (announce runs) can omit it.
 */
export function runSeqKey(runId: string, sessionKey?: string): string {
  return sessionKey ? `${sessionKey}:${runId}` : runId;
}

/**
 * Decide whether a frame carrying a run-scoped event should be processed.
 *
 * - seq absent or 0: always process (server-cold compat).
 * - seq <= last seen for the run: duplicate — drop.
 * - seq > last seen: new event — process, and track it.
 *
 * Tracking is gated: frames that arrive before run.started was observed
 * (reconnect replay, announce runs) never register their seq, so a later
 * replayed run.started (seq 1) can't be beaten by a higher seq seen earlier.
 * run.started itself always registers — it is the first frame of a run, so no
 * earlier frame can exist for the same key.
 *
 * Returns `true` when the caller should process the event.
 */
export function shouldProcessRunEvent(
  seen: RunSeqMap,
  runId: string,
  sessionKey: string | undefined,
  type: string | undefined,
  seq: number | undefined,
  started: boolean,
): boolean {
  if (!seq || seq <= 0) return true; // no seq — process as before
  const key = runSeqKey(runId, sessionKey);
  const last = seen.get(key) ?? 0;
  if (seq <= last) return false; // duplicate — drop
  if (started || type === "run.started") seen.set(key, seq);
  return true;
}