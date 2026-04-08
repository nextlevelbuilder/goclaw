/** V3 pipeline is always enabled. Returns "v3" unconditionally. */
// eslint-disable-next-line @typescript-eslint/no-unused-vars
export function useAgentVersion(_agentId: string): "v3" {
  return "v3";
}
