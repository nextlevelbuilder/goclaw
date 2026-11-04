export interface TakeoverRequest {
  id: string;
  targetId: string;
  host: string;
  reason?: string;
  agentKey?: string;
  sessionKey?: string;
  userId?: string;
  expiresAt: number;
}

/** Validates a browser.takeover.requested payload; null for anything malformed. */
export function parseTakeoverRequest(payload: unknown): TakeoverRequest | null {
  if (!payload || typeof payload !== "object") return null;
  const p = payload as Record<string, unknown>;
  if (typeof p.id !== "string" || !p.id) return null;
  if (typeof p.host !== "string") return null;
  const str = (v: unknown) => (typeof v === "string" ? v : undefined);
  return {
    id: p.id,
    targetId: str(p.targetId) ?? "",
    host: p.host,
    reason: str(p.reason),
    agentKey: str(p.agentKey),
    sessionKey: str(p.sessionKey),
    userId: str(p.userId),
    expiresAt: typeof p.expiresAt === "number" ? p.expiresAt : 0,
  };
}

/** A request is for this user when it carries no user id or the ids match. */
export function isTakeoverForUser(req: TakeoverRequest, userId: string): boolean {
  return !req.userId || !userId || req.userId === userId;
}

export function isTakeoverExpired(req: TakeoverRequest, now: number = Date.now()): boolean {
  return req.expiresAt > 0 && req.expiresAt <= now;
}

export function enqueueTakeover(queue: TakeoverRequest[], req: TakeoverRequest): TakeoverRequest[] {
  return queue.some((r) => r.id === req.id) ? queue : [...queue, req];
}

export function dropTakeover(queue: TakeoverRequest[], id: string): TakeoverRequest[] {
  return queue.filter((r) => r.id !== id);
}

/** Seconds left, never negative; 0 when the request has no deadline. */
export function secondsLeft(req: TakeoverRequest, now: number = Date.now()): number {
  return req.expiresAt > 0 ? Math.max(0, Math.ceil((req.expiresAt - now) / 1000)) : 0;
}

export function formatCountdown(seconds: number): string {
  const m = Math.floor(seconds / 60);
  const s = seconds % 60;
  return `${m}:${s.toString().padStart(2, "0")}`;
}
