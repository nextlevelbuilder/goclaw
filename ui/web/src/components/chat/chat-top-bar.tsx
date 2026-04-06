import { useState, useEffect } from "react";
import { Loader2, Bot, Users, PanelRightOpen, PanelRightClose } from "lucide-react";
import { useHttp } from "@/hooks/use-ws";
import { useAuthStore } from "@/stores/use-auth-store";
import type { RunActivity, ActiveTeamTask } from "@/types/chat";
import type { AgentData } from "@/types/agent";

interface ChatTopBarProps {
  agentId: string;
  isRunning: boolean;
  isBusy: boolean;
  activity: RunActivity | null;
  teamTasks: ActiveTeamTask[];
  onToggleTaskPanel?: () => void;
  taskPanelOpen?: boolean;
}

const phaseLabels: Record<RunActivity["phase"], string> = {
  thinking: "Thinking…",
  tool_exec: "Running tool…",
  streaming: "Responding…",
  compacting: "Compacting…",
  retrying: "Retrying…",
  leader_processing: "Processing team results…",
};

export function ChatTopBar({ agentId, isRunning, isBusy, activity, teamTasks, onToggleTaskPanel, taskPanelOpen }: ChatTopBarProps) {
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
          const emoji = found.emoji || undefined;
          setAgent({ name: found.display_name || found.agent_key, emoji });
        } else {
          setAgent({ name: agentId });
        }
      })
      .catch(() => setAgent({ name: agentId }));
  }, [http, connected, agentId]);

  const displayName = agent?.name ?? agentId;
  const emoji = agent?.emoji;
  const PanelIcon = taskPanelOpen ? PanelRightClose : PanelRightOpen;

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

      <div className="flex items-center gap-2">
        {isRunning ? (
          <div className="flex items-center gap-1.5 text-xs text-muted-foreground">
            <span>{activity ? phaseLabels[activity.phase] : "Running…"}</span>
            <Loader2 className="h-3.5 w-3.5 animate-spin" />
          </div>
        ) : isBusy ? (
          <button
            type="button"
            onClick={onToggleTaskPanel}
            className="flex items-center gap-1.5 rounded-md px-1.5 py-0.5 text-xs text-muted-foreground hover:bg-accent hover:text-accent-foreground"
          >
            <Users className="h-3.5 w-3.5" />
            <span>Team: {teamTasks.length} task{teamTasks.length > 1 ? "s" : ""} active</span>
            <Loader2 className="h-3.5 w-3.5 animate-spin" />
          </button>
        ) : (
          <span className="text-xs text-muted-foreground/50">Ready</span>
        )}

        {/* Task panel toggle — visible when there are (or recently were) team tasks */}
        {teamTasks.length > 0 && (
          <button
            type="button"
            onClick={onToggleTaskPanel}
            className="relative rounded-md p-1.5 text-muted-foreground hover:bg-accent hover:text-accent-foreground"
            title={taskPanelOpen ? "Close task panel" : "Open task panel"}
          >
            <PanelIcon className="h-4 w-4" />
          </button>
        )}
      </div>
    </div>
  );
}
