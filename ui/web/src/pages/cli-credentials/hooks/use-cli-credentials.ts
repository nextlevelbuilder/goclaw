import { useCallback } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { useHttp } from "@/hooks/use-ws";
import { toast } from "@/stores/use-toast-store";
import i18n from "@/i18n";
import type { SecureCLIBinary, CLICredentialInput, CLIPreset, CLIAgentGrant, CLIAgentGrantInput } from "@/types/cli-credential";

export type { SecureCLIBinary, CLICredentialInput, CLIPreset, CLIAgentGrant, CLIAgentGrantInput };

const QUERY_KEY = ["cliCredentials"] as const;
const PRESETS_KEY = ["cliCredentials", "presets"] as const;

export function useCliCredentials() {
  const http = useHttp();
  const queryClient = useQueryClient();

  const { data, isLoading: loading } = useQuery({
    queryKey: QUERY_KEY,
    queryFn: async () => {
      const res = await http.get<{ items: SecureCLIBinary[] }>("/v1/cli-credentials");
      return res.items ?? [];
    },
    placeholderData: (prev) => prev,
  });

  const items = data ?? [];

  const invalidate = useCallback(
    () => queryClient.invalidateQueries({ queryKey: QUERY_KEY }),
    [queryClient],
  );

  const createCredential = useCallback(
    async (input: CLICredentialInput) => {
      try {
        const res = await http.post<SecureCLIBinary>("/v1/cli-credentials", input);
        await invalidate();
        toast.success(
          i18n.t("cli-credentials:toast.created"),
          i18n.t("cli-credentials:toast.createdDesc", { name: input.binary_name }),
        );
        return res;
      } catch (err) {
        toast.error(i18n.t("cli-credentials:toast.createFailed"), err instanceof Error ? err.message : "");
        throw err;
      }
    },
    [http, invalidate],
  );

  const updateCredential = useCallback(
    async (id: string, input: Partial<CLICredentialInput>) => {
      try {
        await http.put(`/v1/cli-credentials/${id}`, input);
        await invalidate();
        toast.success(i18n.t("cli-credentials:toast.updated"));
      } catch (err) {
        toast.error(i18n.t("cli-credentials:toast.updateFailed"), err instanceof Error ? err.message : "");
        throw err;
      }
    },
    [http, invalidate],
  );

  const deleteCredential = useCallback(
    async (id: string) => {
      try {
        await http.delete(`/v1/cli-credentials/${id}`);
        await invalidate();
        toast.success(i18n.t("cli-credentials:toast.deleted"));
      } catch (err) {
        toast.error(i18n.t("cli-credentials:toast.deleteFailed"), err instanceof Error ? err.message : "");
        throw err;
      }
    },
    [http, invalidate],
  );

  return { items, loading, refresh: invalidate, createCredential, updateCredential, deleteCredential };
}

export function useCliCredentialPresets() {
  const http = useHttp();

  const { data, isLoading } = useQuery({
    queryKey: PRESETS_KEY,
    queryFn: async () => {
      const res = await http.get<{ presets: Record<string, CLIPreset> }>("/v1/cli-credentials/presets");
      return res.presets ?? {};
    },
    staleTime: 5 * 60 * 1000,
  });

  return { presets: data ?? {}, loading: isLoading };
}

/** Hook for managing per-agent grants on a specific CLI binary. */
export function useCliCredentialGrants(binaryId: string) {
  const http = useHttp();
  const queryClient = useQueryClient();
  const queryKey = ["cliCredentials", binaryId, "grants"] as const;

  const { data, isLoading } = useQuery({
    queryKey,
    queryFn: async () => {
      const res = await http.get<{ grants: CLIAgentGrant[] }>(
        `/v1/cli-credentials/${binaryId}/agent-grants`,
      );
      return res.grants ?? [];
    },
    enabled: !!binaryId,
  });

  const invalidate = useCallback(
    () => queryClient.invalidateQueries({ queryKey }),
    [queryClient, queryKey],
  );

  const createGrant = useCallback(
    async (input: CLIAgentGrantInput) => {
      try {
        const res = await http.post<CLIAgentGrant>(
          `/v1/cli-credentials/${binaryId}/agent-grants`,
          input,
        );
        await invalidate();
        toast.success(i18n.t("cli-credentials:grants.toast.granted"));
        return res;
      } catch (err) {
        toast.error(i18n.t("cli-credentials:grants.toast.grantFailed"), err instanceof Error ? err.message : "");
        throw err;
      }
    },
    [http, binaryId, invalidate],
  );

  const updateGrant = useCallback(
    async (grantId: string, input: Partial<CLIAgentGrantInput>) => {
      try {
        await http.put(
          `/v1/cli-credentials/${binaryId}/agent-grants/${grantId}`,
          input,
        );
        await invalidate();
        toast.success(i18n.t("cli-credentials:grants.toast.updated"));
      } catch (err) {
        toast.error(i18n.t("cli-credentials:grants.toast.updateFailed"), err instanceof Error ? err.message : "");
        throw err;
      }
    },
    [http, binaryId, invalidate],
  );

  const deleteGrant = useCallback(
    async (grantId: string) => {
      try {
        await http.delete(`/v1/cli-credentials/${binaryId}/agent-grants/${grantId}`);
        await invalidate();
        toast.success(i18n.t("cli-credentials:grants.toast.revoked"));
      } catch (err) {
        toast.error(i18n.t("cli-credentials:grants.toast.revokeFailed"), err instanceof Error ? err.message : "");
        throw err;
      }
    },
    [http, binaryId, invalidate],
  );

  return { grants: data ?? [], loading: isLoading, createGrant, updateGrant, deleteGrant };
}
