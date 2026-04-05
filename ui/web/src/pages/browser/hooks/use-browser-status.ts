import { useState, useCallback } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { useHttp } from "@/hooks/use-ws";
import { useAuthStore } from "@/stores/use-auth-store";

export interface BrowserStatus {
  running: boolean;
  tabs?: number;
  url?: string;
  engine?: string;
  headless?: boolean;
}

export interface BrowserTab {
  targetId: string;
  url: string;
  title: string;
  agentKey?: string;
  sessionKey?: string;
}

interface UseBrowserStatusOpts {
  agentKey?: string;
  sessionKey?: string;
}

export function useBrowserStatus(optsOrAgentKey?: string | UseBrowserStatusOpts) {
  // Support both old signature (agentKey string) and new (opts object)
  const opts: UseBrowserStatusOpts =
    typeof optsOrAgentKey === "string"
      ? { agentKey: optsOrAgentKey }
      : optsOrAgentKey ?? {};

  const http = useHttp();
  const connected = useAuthStore((s) => s.connected);
  const queryClient = useQueryClient();
  const [actionLoading, setActionLoading] = useState(false);

  const { data: status, isPending: statusLoading } = useQuery({
    queryKey: ["browser", "status"],
    queryFn: () => http.get<BrowserStatus>("/browser/status"),
    refetchInterval: 5000,
    enabled: connected,
  });

  const tabsParams: Record<string, string> = {};
  if (opts.agentKey) tabsParams.agentKey = opts.agentKey;
  if (opts.sessionKey) tabsParams.sessionKey = opts.sessionKey;
  const filterKey = opts.sessionKey ?? opts.agentKey ?? "all";

  const { data: tabsData, isPending: tabsLoading } = useQuery({
    queryKey: ["browser", "tabs", filterKey],
    queryFn: () =>
      http.get<{ tabs: BrowserTab[]; error?: string }>(
        "/browser/tabs",
        Object.keys(tabsParams).length > 0 ? tabsParams : undefined,
      ),
    refetchInterval: 3000,
    enabled: connected && !!status?.running,
  });

  const invalidate = useCallback(() => {
    queryClient.invalidateQueries({ queryKey: ["browser"] });
  }, [queryClient]);

  const startBrowser = useCallback(async () => {
    setActionLoading(true);
    try {
      await http.post("/browser/start");
      invalidate();
    } finally {
      setActionLoading(false);
    }
  }, [http, invalidate]);

  const stopBrowser = useCallback(async () => {
    setActionLoading(true);
    try {
      await http.post("/browser/stop");
      invalidate();
    } finally {
      setActionLoading(false);
    }
  }, [http, invalidate]);

  const createLiveSession = useCallback(
    async (targetId: string, mode: "view" | "takeover" = "view") => {
      return http.post<{ token: string; url: string }>("/browser/live", {
        targetId,
        mode,
      });
    },
    [http],
  );

  // Derive unique agent keys from tabs for filter dropdown
  const agentKeys = [...new Set((tabsData?.tabs ?? []).map((t) => t.agentKey).filter(Boolean))] as string[];

  return {
    status: status ?? null,
    tabs: tabsData?.tabs ?? [],
    agentKeys,
    loading: statusLoading || tabsLoading,
    actionLoading,
    startBrowser,
    stopBrowser,
    createLiveSession,
    refresh: invalidate,
  };
}
