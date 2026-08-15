import type { CLIEnvEntryResponse, CLIEnvPayload } from "@/types/cli-credential";
import type { ManualEnvEntry } from "./cli-credential-env-vars-section";
import type { GitCredentialType } from "./cli-credential-git-fields";

export function entriesFromEnv(env: Record<string, CLIEnvEntryResponse> | null | undefined): ManualEnvEntry[] {
  if (!env || Object.keys(env).length === 0) return [];
  return Object.entries(env).map(([key, entry]) => ({
    key,
    value: entry.value ?? "",
    kind: entry.kind ?? "sensitive",
  }));
}

export function envPayloadFromEntries(entries: ManualEnvEntry[]): CLIEnvPayload {
  const env: CLIEnvPayload = {};
  for (const entry of entries) {
    const key = entry.key.trim();
    if (key) env[key] = { kind: entry.kind, value: entry.value };
  }
  return env;
}

export type AgentCredentialPayloadResult =
  | { kind: "typed"; payload: { credential_type: "pat" | "ssh_key"; host_scope: string; blob: Record<string, string> } }
  | { kind: "env"; payload: { env: CLIEnvPayload } }
  | { kind: "no_change" }
  | { kind: "error"; errorKey: string };

export function buildAgentCredentialPayload(input: {
  isGit: boolean;
  type: GitCredentialType;
  hostScope: string;
  token: string;
  privateKey: string;
  hasExistingSecret: boolean;
  envEntries: ManualEnvEntry[];
  isNewEntry: boolean;
}): AgentCredentialPayloadResult {
  const { isGit, type, hostScope, token, privateKey, hasExistingSecret, envEntries, isNewEntry } = input;
  if (isGit && type !== "env") {
    const scope = hostScope.trim();
    if (!scope) return { kind: "error", errorKey: "git.cred_host_scope_required" };
    if (type === "pat") {
      if (!token) return hasExistingSecret ? { kind: "no_change" } : { kind: "error", errorKey: "git.cred_blob_missing_token" };
      return { kind: "typed", payload: { credential_type: "pat", host_scope: scope, blob: { token } } };
    }
    if (!privateKey.trim()) return hasExistingSecret ? { kind: "no_change" } : { kind: "error", errorKey: "git.cred_blob_missing_key" };
    return { kind: "typed", payload: { credential_type: "ssh_key", host_scope: scope, blob: { key: privateKey } } };
  }
  const env = envPayloadFromEntries(envEntries);
  if (isNewEntry && Object.keys(env).length === 0) return { kind: "error", errorKey: "env_required" };
  return { kind: "env", payload: { env } };
}
