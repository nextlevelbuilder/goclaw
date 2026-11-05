import type { SecretRequest } from "@/types/secret-vault";

const KINDS = new Set(["api_key", "login", "approval"]);

/**
 * Validates a secretVault.form.requested payload. Returns null for anything
 * malformed so a bad event can never open a dialog.
 */
export function parseSecretRequest(payload: unknown): SecretRequest | null {
  if (!payload || typeof payload !== "object") return null;
  const p = payload as Record<string, unknown>;
  if (typeof p.id !== "string" || !p.id) return null;
  if (typeof p.kind !== "string" || !KINDS.has(p.kind)) return null;
  if (typeof p.name !== "string") return null;
  const str = (v: unknown) => (typeof v === "string" ? v : undefined);
  return {
    id: p.id,
    kind: p.kind as SecretRequest["kind"],
    name: p.name,
    domains: Array.isArray(p.domains) ? p.domains.filter((d): d is string => typeof d === "string") : undefined,
    reason: str(p.reason),
    hints: str(p.hints),
    host: str(p.host),
    action: str(p.action),
    agentKey: str(p.agentKey),
    sessionKey: str(p.sessionKey),
    userId: str(p.userId),
    expiresAt: typeof p.expiresAt === "number" ? p.expiresAt : 0,
  };
}

/** A request is for this user when it carries no user id or the ids match. */
export function isForUser(req: SecretRequest, userId: string): boolean {
  return !req.userId || !userId || req.userId === userId;
}

export function isExpired(req: SecretRequest, now: number = Date.now()): boolean {
  return req.expiresAt > 0 && req.expiresAt <= now;
}

/** Adds a request to the queue, ignoring duplicates by id. */
export function enqueueRequest(queue: SecretRequest[], req: SecretRequest): SecretRequest[] {
  return queue.some((r) => r.id === req.id) ? queue : [...queue, req];
}

export function dropRequest(queue: SecretRequest[], id: string): SecretRequest[] {
  return queue.filter((r) => r.id !== id);
}
