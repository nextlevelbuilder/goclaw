import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";
import { Tooltip, TooltipContent, TooltipProvider, TooltipTrigger } from "@/components/ui/tooltip";
import { ArrowLeft, Bot, Heart, Settings, Sparkles, Star, Trash2 } from "lucide-react";
import { useTranslation } from "react-i18next";
import type { AgentData } from "@/types/agent";
import type { HeartbeatConfig } from "@/pages/agents/hooks/use-agent-heartbeat";
import { useCountdown } from "@/hooks/use-countdown";
import { agentDisplayName, agentKeyDisplay } from "./agent-display-utils";

interface AgentHeaderProps {
  agent: AgentData;
  heartbeat: HeartbeatConfig | null;
  onBack: () => void;
  onDelete: () => void;
  onAdvanced: () => void;
  onHeartbeat: () => void;
}

export function AgentHeader({ agent, heartbeat, onBack, onDelete, onAdvanced, onHeartbeat }: AgentHeaderProps) {
  const { t } = useTranslation("agents");

  const otherCfg = (agent.other_config ?? {}) as Record<string, unknown>;
  const emoji = typeof otherCfg.emoji === "string" ? otherCfg.emoji : "";
  const selfEvolve = Boolean(otherCfg.self_evolve);
  const title = agentDisplayName(agent, t("card.unnamedAgent"));
  const keyDisplay = agentKeyDisplay(agent.agent_key);

  const hbConfigured = heartbeat != null;
  const hbEnabled = heartbeat?.enabled ?? false;
  const countdown = useCountdown(hbEnabled ? heartbeat?.nextRunAt : null);

  return (
    <TooltipProvider>
      <div className="sticky top-0 z-10 flex items-center gap-2 border-b bg-card px-3 py-2 landscape-compact sm:px-4 sm:gap-3">
        <Button variant="ghost" size="icon" onClick={onBack} className="shrink-0 size-9">
          <ArrowLeft className="h-4 w-4" />
        </Button>

        {/* Emoji avatar */}
        <div className="flex h-10 w-10 shrink-0 items-center justify-center rounded-xl bg-primary/10 text-primary sm:h-12 sm:w-12">
          {emoji
            ? <span className="text-xl leading-none sm:text-2xl">{emoji}</span>
            : <Bot className="h-5 w-5 sm:h-6 sm:w-6" />}
        </div>

        {/* Agent info */}
        <div className="min-w-0 flex-1">
          <div className="flex items-center gap-1.5 flex-wrap">
            <h2 className="truncate text-base font-semibold">{title}</h2>
            {agent.is_default && (
              <Star className="h-3.5 w-3.5 shrink-0 fill-amber-400 text-amber-400" />
            )}
            <Badge
              variant={
                agent.status === "active"
                  ? "success"
                  : agent.status === "summon_failed"
                    ? "destructive"
                    : "secondary"
              }
              className="text-[10px]"
            >
              {agent.status === "summon_failed" ? t("detail.summonFailed") : agent.status}
            </Badge>
            <Tooltip>
              <TooltipTrigger asChild>
                <Badge variant="outline" className="text-[10px]">{agent.agent_type}</Badge>
              </TooltipTrigger>
              <TooltipContent side="bottom" className="max-w-[260px] text-xs">
                {agent.agent_type === "predefined" ? t("card.predefinedTooltip") : t("card.openTooltip")}
              </TooltipContent>
            </Tooltip>
            {agent.agent_type === "predefined" && (
              <Tooltip>
                <TooltipTrigger asChild>
                  <Badge
                    variant={selfEvolve ? "default" : "outline"}
                    className={`text-[10px] ${selfEvolve ? "bg-violet-100 text-violet-700 hover:bg-violet-100 dark:bg-violet-900/30 dark:text-violet-300" : "text-muted-foreground"}`}
                  >
                    <Sparkles className="mr-0.5 h-2.5 w-2.5" />
                    {selfEvolve ? t("detail.evolving") : t("detail.static")}
                  </Badge>
                </TooltipTrigger>
                <TooltipContent side="bottom" className="max-w-[240px] text-xs">
                  {selfEvolve ? t("detail.evolvingTooltipDetail") : t("detail.staticTooltipDetail")}
                </TooltipContent>
              </Tooltip>
            )}
          </div>
          <div className="flex items-center gap-1.5 text-xs text-muted-foreground mt-0.5">
            <span className="font-mono text-[11px]">{keyDisplay}</span>
            {agent.provider && (
              <>
                <span className="text-border">·</span>
                <span>{agent.provider} / {agent.model}</span>
              </>
            )}
          </div>
        </div>

        {/* Heartbeat action */}
        <Button variant="ghost" size="sm" onClick={onHeartbeat} className="shrink-0 gap-1.5 size-9 sm:w-auto sm:px-3">
          <Heart className={`h-4 w-4 ${hbEnabled ? "fill-rose-500 text-rose-500 animate-pulse" : hbConfigured ? "text-rose-400" : "text-muted-foreground"}`} />
          <span className={`hidden sm:inline ${hbEnabled ? "text-rose-600 dark:text-rose-400" : ""}`}>
            {!hbConfigured
              ? t("heartbeat.notSet")
              : !hbEnabled
                ? t("heartbeat.off")
                : countdown
                  ? t("heartbeat.nextIn", { time: countdown })
                  : t("heartbeat.on")}
          </span>
        </Button>
        <Button variant="ghost" size="sm" onClick={onAdvanced} className="shrink-0 gap-1.5 size-9 sm:w-auto sm:px-3">
          <Settings className="h-4 w-4" />
          <span className="hidden sm:inline">{t("detail.advanced")}</span>
        </Button>
        <Button
          variant="ghost"
          size="sm"
          onClick={onDelete}
          className="shrink-0 gap-1.5 size-9 sm:w-auto sm:px-3 text-muted-foreground hover:text-destructive"
        >
          <Trash2 className="h-4 w-4" />
          <span className="hidden sm:inline">{t("delete.title")}</span>
        </Button>
      </div>
    </TooltipProvider>
  );
}
