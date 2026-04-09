import { useQuery } from "@tanstack/react-query";
import { useHttp } from "@/hooks/use-ws";
import { queryKeys } from "@/lib/query-keys";

export interface WhatsAppGroup {
  jid: string;
  name: string;
  member_count?: number;
  agent_id?: string;
  display_name?: string;
  enabled?: boolean | null;
  configured: boolean;
}

export function useWhatsAppGroups(instanceId: string | undefined) {
  const http = useHttp();

  const { data, isLoading: loading, refetch: refresh } = useQuery({
    queryKey: queryKeys.channels.whatsappGroups(instanceId ?? ""),
    queryFn: async () => {
      const res = await http.get<{ groups: WhatsAppGroup[] }>(
        `/v1/channels/instances/${instanceId}/whatsapp/groups`,
      );
      return res.groups ?? [];
    },
    enabled: !!instanceId,
  });

  const refreshGroups = async () => {
    // Trigger on-demand cache refresh on the backend, then refetch.
    await http.get<{ groups: WhatsAppGroup[] }>(
      `/v1/channels/instances/${instanceId}/whatsapp/groups?refresh=true`,
    );
    return refresh();
  };

  return { groups: data ?? [], loading, refresh, refreshGroups };
}
