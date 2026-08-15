import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query";
import { useHttp } from "@/hooks/use-ws";

export interface Voice {
  voice_id: string;
  name: string;
  preview_url?: string;
  labels?: Record<string, string>;
  category?: string;
}

interface VoicesResponse {
  voices: Voice[];
}

export const voiceKeys = {
  all: ["voices"] as const,
};

export function useVoices() {
  const http = useHttp();
  return useQuery({
    queryKey: voiceKeys.all,
    queryFn: async () => {
      const res = await http.get<VoicesResponse>("/v1/voices");
      return res.voices ?? [];
    },
    staleTime: 5 * 60_000,
    retry: 1,
  });
}

export function useRefreshVoices() {
  const http = useHttp();
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: () => http.post<{ status: string }>("/v1/voices/refresh"),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: voiceKeys.all });
    },
  });
}
