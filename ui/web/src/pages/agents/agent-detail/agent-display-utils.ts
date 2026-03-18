/** Matches a standard UUID v4 string. */
export const UUID_RE = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i;

/** Returns the display name for an agent, falling back to agent_key or unnamedLabel. */
export function agentDisplayName(
  agent: { display_name?: string; agent_key: string },
  unnamedLabel: string,
): string {
  if (agent.display_name) return agent.display_name;
  if (UUID_RE.test(agent.agent_key)) return unnamedLabel;
  return agent.agent_key;
}

/** Returns a shortened agent key for subtitle display (truncates UUIDs). */
export function agentKeyDisplay(agentKey: string): string {
  return UUID_RE.test(agentKey) ? agentKey.slice(0, 8) + "…" : agentKey;
}
