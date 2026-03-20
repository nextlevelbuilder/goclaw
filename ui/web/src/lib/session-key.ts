/**
 * Parse a session key like "agent:myAgent:abc123" into parts.
 */
export function parseSessionKey(key: string): {
  agentId: string;
  scope: string;
} {
  const parts = key.split(":");
  if (parts.length >= 3 && parts[0] === "agent") {
    return { agentId: parts[1]!, scope: parts.slice(2).join(":") };
  }
  return { agentId: "", scope: key };
}

/**
 * Build a session key from agent ID and scope.
 */
export function buildSessionKey(agentId: string, scope: string): string {
  return `agent:${agentId}:${scope}`;
}

/**
 * Check if a session belongs to the current web user.
 * New format: "ws:direct:{convId}" — API already filters by userId, so all WS sessions are own.
 * Legacy format: "ws-{userId}-{timestamp}".
 * Sessions from other channels (telegram, discord, etc.) are foreign.
 */
export function isOwnSession(sessionKey: string, userId: string): boolean {
  if (!userId) return false;
  const { scope } = parseSessionKey(sessionKey);
  if (scope.startsWith("ws:direct:")) return true;
  return scope.startsWith(`ws-${userId}-`);
}
