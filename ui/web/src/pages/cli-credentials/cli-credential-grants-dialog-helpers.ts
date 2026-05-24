/**
 * Pure helpers for cli-credential-grants-dialog.tsx — extracted for line-count.
 */
import type { CLIAgentGrant } from "./hooks/use-cli-credentials";
import type { GrantEnvState, GrantEnvEntry } from "./cli-credential-grant-env-section";

export const EMPTY_ENV_STATE: GrantEnvState = { overrideEnabled: false, entries: [] };

/**
 * Build env_vars field for PUT/POST body:
 *   undefined → omit field (no change)
 *   null      → clear override (fall back to binary defaults)
 *   {...}     → replace override
 */
export function buildEnvVarsPayload(
  envState: GrantEnvState,
  originalEnvSet: boolean,
): Record<string, string> | null | undefined {
  const { overrideEnabled, entries } = envState;
  if (overrideEnabled) {
    const allMasked = entries.length > 0 && entries.every((e: GrantEnvEntry) => e.masked);
    if (allMasked) return undefined; // not revealed; don't overwrite
    const result: Record<string, string> = {};
    for (const e of entries) {
      if (!e.masked && e.key.trim()) result[e.key.trim()] = e.value;
    }
    return result;
  }
  return originalEnvSet ? null : undefined;
}

/** Derive initial GrantEnvState from an existing grant. */
export function envStateFromGrant(grant: CLIAgentGrant): GrantEnvState {
  if (grant.env_set && grant.env_keys && grant.env_keys.length > 0) {
    return {
      overrideEnabled: true,
      entries: grant.env_keys.map((k) => ({ key: k, value: "", masked: true })),
    };
  }
  return EMPTY_ENV_STATE;
}
