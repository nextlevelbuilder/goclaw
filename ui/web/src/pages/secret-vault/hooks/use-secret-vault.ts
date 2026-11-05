import { useCallback } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import i18n from "@/i18n";
import { useHttp } from "@/hooks/use-ws";
import { toast } from "@/stores/use-toast-store";
import { queryKeys } from "@/lib/query-keys";
import type { AgentData } from "@/types/agent";
import type {
  SecretVaultEntry,
  SecretVaultCreateInput,
  SecretVaultMetaInput,
  SecretVaultValueInput,
  SecretVaultApproveInput,
  SecretVaultAuditRow,
} from "@/types/secret-vault";
import { withVaultAgentWrite } from "../secret-vault-utils";

const QUERY_KEY = ["secretVault"] as const;

// Stable reference so consumers can safely memoize on `items`.
const NO_ENTRIES: SecretVaultEntry[] = [];

function fail(err: unknown): never {
  toast.error(i18n.t("secret-vault:toast.failed"), err instanceof Error ? err.message : "");
  throw err;
}

export function useSecretVault() {
  const http = useHttp();
  const queryClient = useQueryClient();

  const { data, isLoading: loading } = useQuery({
    queryKey: QUERY_KEY,
    queryFn: async () => (await http.get<{ items: SecretVaultEntry[] }>("/v1/secret-vault")).items ?? [],
    placeholderData: (prev) => prev,
  });

  const invalidate = useCallback(() => queryClient.invalidateQueries({ queryKey: QUERY_KEY }), [queryClient]);

  const createSecret = useCallback(
    async (input: SecretVaultCreateInput) => {
      try {
        const res = await http.post<SecretVaultEntry>("/v1/secret-vault", input);
        await invalidate();
        toast.success(i18n.t("secret-vault:toast.created"));
        return res;
      } catch (err) {
        return fail(err);
      }
    },
    [http, invalidate],
  );

  const updateMeta = useCallback(
    async (id: string, input: SecretVaultMetaInput) => {
      try {
        await http.put(`/v1/secret-vault/${id}`, input);
        await invalidate();
        toast.success(i18n.t("secret-vault:toast.updated"));
      } catch (err) {
        fail(err);
      }
    },
    [http, invalidate],
  );

  const replaceValue = useCallback(
    async (id: string, input: SecretVaultValueInput) => {
      try {
        await http.put(`/v1/secret-vault/${id}/value`, input);
        await invalidate();
        toast.success(i18n.t("secret-vault:toast.updated"));
      } catch (err) {
        fail(err);
      }
    },
    [http, invalidate],
  );

  const deleteSecret = useCallback(
    async (id: string) => {
      try {
        await http.delete(`/v1/secret-vault/${id}`);
        await invalidate();
        toast.success(i18n.t("secret-vault:toast.deleted"));
      } catch (err) {
        fail(err);
      }
    },
    [http, invalidate],
  );

  const approveSecret = useCallback(
    async (id: string, input: SecretVaultApproveInput) => {
      try {
        await http.post<SecretVaultEntry>(`/v1/secret-vault/${id}/approve`, input);
        await invalidate();
        toast.success(i18n.t("secret-vault:toast.approved"));
      } catch (err) {
        fail(err);
      }
    },
    [http, invalidate],
  );

  return {
    items: data ?? NO_ENTRIES,
    loading,
    refresh: invalidate,
    createSecret,
    updateMeta,
    replaceValue,
    deleteSecret,
    approveSecret,
  };
}

/**
 * Toggles other_config.vault_agent_write for one agent. PUT /v1/agents/{id}
 * replaces the whole other_config, so the agent is re-read right before the
 * write and only the flag is changed; the cached list may be stale.
 */
export function useVaultAgentWrite() {
  const http = useHttp();
  const queryClient = useQueryClient();

  const setAgentWrite = useCallback(
    async (agentId: string, on: boolean) => {
      try {
        const agent = await http.get<AgentData>(`/v1/agents/${agentId}`);
        await http.put(`/v1/agents/${agentId}`, { other_config: withVaultAgentWrite(agent.other_config, on) });
        await queryClient.invalidateQueries({ queryKey: queryKeys.agents.all });
        toast.success(i18n.t("secret-vault:toast.agentWriteSaved"));
      } catch (err) {
        fail(err);
      }
    },
    [http, queryClient],
  );

  return { setAgentWrite };
}

// Stable reference: a fresh [] per render would retrigger the dialog's effect forever.
const NO_AGENT_IDS: string[] = [];

export function useSecretVaultGrants(id: string, enabled: boolean) {
  const http = useHttp();
  const queryClient = useQueryClient();
  const key = ["secretVault", id, "grants"] as const;

  const { data, isLoading: loading } = useQuery({
    queryKey: key,
    enabled: enabled && !!id,
    queryFn: async () => (await http.get<{ agent_ids: string[] }>(`/v1/secret-vault/${id}/grants`)).agent_ids ?? [],
  });

  const saveGrants = useCallback(
    async (agentIds: string[]) => {
      try {
        await http.put(`/v1/secret-vault/${id}/grants`, { agent_ids: agentIds });
        await queryClient.invalidateQueries({ queryKey: key });
        toast.success(i18n.t("secret-vault:toast.grantsSaved"));
      } catch (err) {
        fail(err);
      }
    },
    // eslint-disable-next-line react-hooks/exhaustive-deps
    [http, id, queryClient],
  );

  return { agentIds: data ?? NO_AGENT_IDS, loading, saveGrants };
}

export function useSecretVaultAudit(id: string, enabled: boolean) {
  const http = useHttp();
  const { data, isLoading: loading } = useQuery({
    queryKey: ["secretVault", id, "audit"],
    enabled: enabled && !!id,
    queryFn: async () => (await http.get<{ items: SecretVaultAuditRow[] }>(`/v1/secret-vault/${id}/audit`)).items ?? [],
  });
  return { rows: data ?? [], loading };
}
