const NAME_RE = /^[a-z0-9][a-z0-9_-]{0,63}$/;

export function isValidSecretName(name: string): boolean {
  return NAME_RE.test(name);
}

/** Splits a comma/space separated domain list, trimming and dropping blanks and duplicates. */
export function parseDomains(raw: string): string[] {
  const seen = new Set<string>();
  for (const part of raw.split(/[\s,]+/)) {
    const d = part.trim().toLowerCase();
    if (d) seen.add(d);
  }
  return [...seen];
}

/** other_config key that lets an agent save secrets into the vault (default off). */
export const VAULT_AGENT_WRITE_KEY = "vault_agent_write";

type OtherConfig = Record<string, unknown> | null | undefined;

/** Only an explicit boolean true counts: the flag is opt-in and defaults off. */
export function isVaultAgentWriteOn(otherConfig: OtherConfig): boolean {
  return otherConfig?.[VAULT_AGENT_WRITE_KEY] === true;
}

/**
 * Returns a copy of other_config with the flag set. PUT /v1/agents/{id}
 * replaces the whole other_config column, so every other key must be carried over.
 */
export function withVaultAgentWrite(otherConfig: OtherConfig, on: boolean): Record<string, unknown> {
  return { ...(otherConfig ?? {}), [VAULT_AGENT_WRITE_KEY]: on };
}

export function isPending(e: { status?: string }): boolean {
  return e.status === "pending";
}

/** Logins default to asking before each use, API keys do not. */
export function defaultApproval(kind: "api_key" | "login"): boolean {
  return kind === "login";
}
