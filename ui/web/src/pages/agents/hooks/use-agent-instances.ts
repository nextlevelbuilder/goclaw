import { useCallback, useState } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { useHttp } from "@/hooks/use-ws";
import { queryKeys } from "@/lib/query-keys";
import { toast } from "@/stores/use-toast-store";
import i18n from "@/i18n";
import { userFriendlyError } from "@/lib/error-utils";

export interface UserInstance {
  user_id: string;
  first_seen_at?: string;
  last_seen_at?: string;
  file_count: number;
  metadata?: Record<string, string>;
}

export interface UserContextFile {
  agent_id: string;
  user_id: string;
  file_name: string;
  content: string;
}

export function useAgentInstances(agentId: string) {
  const http = useHttp();
  const queryClient = useQueryClient();
  const [saving, setSaving] = useState(false);

  const { data, isLoading } = useQuery({
    queryKey: queryKeys.agents.instances(agentId),
    queryFn: async () => {
      const res = await http.get<{ instances: UserInstance[] }>(`/v1/agents/${agentId}/instances`);
      return res.instances ?? [];
    },
    staleTime: 60_000,
    enabled: !!agentId,
  });

  const getFiles = useCallback(
    async (userID: string): Promise<UserContextFile[]> => {
      const res = await http.get<{ files: UserContextFile[] }>(
        `/v1/agents/${agentId}/instances/${encodeURIComponent(userID)}/files`,
      );
      return res.files ?? [];
    },
    [agentId, http],
  );

  const setFile = useCallback(
    async (userID: string, fileName: string, content: string) => {
      setSaving(true);
      try {
        await http.put(
          `/v1/agents/${agentId}/instances/${encodeURIComponent(userID)}/files/${encodeURIComponent(fileName)}`,
          { content },
        );
        queryClient.invalidateQueries({ queryKey: queryKeys.agents.instances(agentId) });
        toast.success(i18n.t("agents:toast.updated"));
      } catch (err) {
        toast.error(i18n.t("agents:toast.updateFailed"), userFriendlyError(err));
        throw err;
      } finally {
        setSaving(false);
      }
    },
    [agentId, http, queryClient],
  );

  return {
    instances: data ?? [],
    loading: isLoading,
    saving,
    getFiles,
    setFile,
  };
}
