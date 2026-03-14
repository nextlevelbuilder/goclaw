import { useState, useEffect } from "react";
import { useHttp } from "@/hooks/use-ws";
import { useAuthStore } from "@/stores/use-auth-store";
import type { AgentData } from "@/types/agent";

/**
 * Resolves the actual agent_key of the default agent from the API.
 * Returns "default" as fallback while loading or on error.
 */
export function useDefaultAgentKey(): { agentKey: string; loading: boolean } {
  const http = useHttp();
  const connected = useAuthStore((s) => s.connected);
  const [agentKey, setAgentKey] = useState("default");
  const [loading, setLoading] = useState(true);

  useEffect(() => {
    if (!connected) return;
    setLoading(true);
    http
      .get<{ agents: AgentData[] }>("/v1/agents")
      .then((res) => {
        const active = (res.agents ?? []).filter((a) => a.status === "active");
        const defaultAgent = active.find((a) => a.is_default) ?? active[0];
        if (defaultAgent) {
          setAgentKey(defaultAgent.agent_key);
        }
      })
      .catch(() => {})
      .finally(() => setLoading(false));
  }, [http, connected]);

  return { agentKey, loading };
}
