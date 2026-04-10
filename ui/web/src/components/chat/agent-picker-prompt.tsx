import { useState, useEffect } from "react";
import { useTranslation } from "react-i18next";
import { Bot } from "lucide-react";
import { useHttp } from "@/hooks/use-ws";
import { useAuthStore } from "@/stores/use-auth-store";
import type { AgentData } from "@/types/agent";

interface AgentPickerPromptProps {
  onSelect: (agentId: string) => void;
}

function agentEmoji(agent: AgentData): string | undefined {
  return agent.emoji || undefined;
}

export function AgentPickerPrompt({ onSelect }: AgentPickerPromptProps) {
  const { t } = useTranslation("chat");
  const http = useHttp();
  const connected = useAuthStore((s) => s.connected);
  const [agents, setAgents] = useState<AgentData[]>([]);

  useEffect(() => {
    if (!connected) return;
    http
      .get<{ agents: AgentData[] }>("/v1/agents")
      .then((res) => {
        setAgents((res.agents ?? []).filter((a) => a.status === "active"));
      })
      .catch((err) => console.error("[AgentPickerPrompt] fetch agents failed:", err));
  }, [http, connected]);

  return (
    <div className="mx-3 mb-3 safe-bottom">
      <div className="rounded-xl border bg-background/95 backdrop-blur-sm shadow-sm p-4">
        <p className="text-sm font-medium mb-1">{t("selectAgent.title")}</p>
        <p className="text-xs text-muted-foreground mb-3">{t("selectAgent.description")}</p>
        <div className="grid grid-cols-1 sm:grid-cols-2 gap-2 max-h-60 overflow-y-auto">
          {agents.map((agent) => {
            const emoji = agentEmoji(agent);
            return (
              <button
                key={agent.agent_key}
                type="button"
                onClick={() => onSelect(agent.agent_key)}
                className="flex items-center gap-3 rounded-lg border bg-card px-3 py-2.5 text-left text-sm hover:bg-accent transition-colors cursor-pointer"
              >
                {emoji ? (
                  <span className="text-lg shrink-0">{emoji}</span>
                ) : (
                  <Bot className="h-5 w-5 shrink-0 text-muted-foreground" />
                )}
                <div className="min-w-0 flex-1">
                  <span className="font-medium truncate block">
                    {agent.display_name || agent.agent_key}
                  </span>
                  {agent.is_default && (
                    <span className="text-xs text-muted-foreground">Default</span>
                  )}
                </div>
              </button>
            );
          })}
        </div>
      </div>
    </div>
  );
}
