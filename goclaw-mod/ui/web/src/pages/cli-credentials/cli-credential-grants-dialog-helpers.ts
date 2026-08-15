/**
 * Pure helpers for cli-credential-grants-dialog.tsx — extracted for line-count.
 */
import type { CLIAgentGrant } from "./hooks/use-cli-credentials";
import type { GrantEnvState, GrantEnvEntry } from "./cli-credential-grant-env-section";
import type { CLIEnvPayload } from "@/types/cli-credential";

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
): CLIEnvPayload | null | undefined {
  const { overrideEnabled, entries } = envState;
  if (overrideEnabled) {
    const allMasked = entries.length > 0 && entries.every((e: GrantEnvEntry) => e.masked);
    if (allMasked) return undefined; // not revealed; don't overwrite
    const result: CLIEnvPayload = {};
    for (const e of entries) {
      if (!e.masked && e.key.trim()) result[e.key.trim()] = { kind: e.kind, value: e.value };
    }
    return result;
  }
  return originalEnvSet ? null : undefined;
}

/** Derive initial GrantEnvState from an existing grant. */
export function envStateFromGrant(grant: CLIAgentGrant): GrantEnvState {
  if (grant.env && Object.keys(grant.env).length > 0) {
    return {
      overrideEnabled: true,
      entries: Object.entries(grant.env)
        .sort(([a], [b]) => a.localeCompare(b))
        .map(([key, entry]) => ({
          key,
          value: entry.kind === "value" && entry.value !== null ? entry.value : "",
          kind: entry.kind,
          masked: entry.masked,
        })),
    };
  }
  if (grant.env_set && grant.env_keys && grant.env_keys.length > 0) {
    return {
      overrideEnabled: true,
      entries: grant.env_keys.map((k) => ({ key: k, value: "", kind: "sensitive", masked: true })),
    };
  }
  return EMPTY_ENV_STATE;
}
