import { useState, useEffect } from "react";
import { Loader2, Bot } from "lucide-react";
import { useHttp } from "@/hooks/use-ws";
import { useAuthStore } from "@/stores/use-auth-store";
import type { RunActivity } from "@/types/chat";
import type { AgentData } from "@/types/agent";

interface ChatTopBarProps {
  agentId: string;
  isRunning: boolean;
  activity: RunActivity | null;
}

const phaseLabels: Record<RunActivity["phase"], string> = {
  thinking: "Thinking…",
  tool_exec: "Running tool…",
  streaming: "Responding…",
  compacting: "Compacting…",
  retrying: "Retrying…",
};

export function ChatTopBar({ agentId, isRunning, activity }: ChatTopBarProps) {
  const http = useHttp();
  const connected = useAuthStore((s) => s.connected);
  const [agent, setAgent] = useState<{ name: string; emoji?: string } | null>(null);

  // Fetch agent display info (lightweight, cached per agentId)
  useEffect(() => {
    if (!connected || !agentId) return;
    setAgent(null);
    http
      .get<{ agents: AgentData[] }>("/v1/agents")
      .then((res) => {
        const found = (res.agents ?? []).find((a) => a.agent_key === agentId);
        if (found) {
          const emoji = (found.other_config?.emoji as string) || undefined;
          setAgent({ name: found.display_name || found.agent_key, emoji });
        } else {
          setAgent({ name: agentId });
        }
      })
      .catch(() => setAgent({ name: agentId }));
  }, [http, connected, agentId]);

  const displayName = agent?.name ?? agentId;
  const emoji = agent?.emoji;

  return (
    <div className="flex items-center justify-between border-b px-4 py-1.5">
      <div className="flex items-center gap-2">
        {emoji ? (
          <span className="text-base">{emoji}</span>
        ) : (
          <Bot className="h-4 w-4 text-muted-foreground" />
        )}
        <span className="text-sm font-semibold">{displayName}</span>
      </div>

      {isRunning ? (
        <div className="flex items-center gap-1.5 text-xs text-muted-foreground">
          <span>{activity ? phaseLabels[activity.phase] : "Running…"}</span>
          <Loader2 className="h-3.5 w-3.5 animate-spin" />
        </div>
      ) : (
        <span className="text-xs text-muted-foreground/50">Ready</span>
      )}
    </div>
  );
}
