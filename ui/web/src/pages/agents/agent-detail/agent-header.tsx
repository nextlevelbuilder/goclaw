import { useState } from "react";
import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";
import { Tooltip, TooltipContent, TooltipProvider, TooltipTrigger } from "@/components/ui/tooltip";
import { ArrowLeft, Bot, Eye, Heart, Settings, Sparkles, Star, Trash2 } from "lucide-react";
import { useTranslation } from "react-i18next";
import type { AgentData } from "@/types/agent";
import type { HeartbeatConfig } from "@/pages/agents/hooks/use-agent-heartbeat";
import { useCountdown } from "@/hooks/use-countdown";
import { agentDisplayName, agentKeyDisplay, hasActiveChatGPTOAuthRouting, readPromptMode } from "./agent-display-utils";
import { cn } from "@/lib/utils";
import { promptModeBadgeClass } from "./prompt-mode-badge-utils";
import { V3CapabilitiesModal } from "@/components/agents/v3-capabilities-modal/v3-capabilities-modal";

interface AgentHeaderProps {
  agent: AgentData;
  heartbeat: HeartbeatConfig | null;
  onBack: () => void;
  onDelete: () => void;
  onAdvanced: () => void;
  onHeartbeat: () => void;
  onSystemPrompt?: () => void;
}

export function AgentHeader({ agent, heartbeat, onBack, onDelete, onAdvanced, onHeartbeat, onSystemPrompt }: AgentHeaderProps) {
  const { t } = useTranslation("agents");
  const [v3Open, setV3Open] = useState(false);

  const emoji = agent.emoji ?? "";
  const selfEvolve = Boolean(agent.self_evolve);
  const title = agentDisplayName(agent, t("card.unnamedAgent"));
  const keyDisplay = agentKeyDisplay(agent.agent_key);
  const hasOAuthRouting = hasActiveChatGPTOAuthRouting(agent.chatgpt_oauth_routing);
  const promptMode = readPromptMode(agent);

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
            <Tooltip>
              <TooltipTrigger asChild>
                <span
                  className={cn(
                    "inline-block h-2.5 w-2.5 shrink-0 rounded-full",
                    agent.status === "active"
                      ? "bg-emerald-500"
                      : agent.status === "summon_failed"
                        ? "bg-destructive"
                        : "bg-muted-foreground/50",
                  )}
                />
              </TooltipTrigger>
              <TooltipContent side="bottom" className="text-xs">
                {agent.status === "summon_failed" ? t("detail.summonFailed") : agent.status}
              </TooltipContent>
            </Tooltip>
            <Tooltip>
              <TooltipTrigger asChild>
                <Badge
                  variant="outline"
                  className={cn("text-2xs", promptModeBadgeClass(promptMode))}
                >
                  {t(`detail.prompt.mode.${promptMode}`)}
                </Badge>
              </TooltipTrigger>
              <TooltipContent side="bottom" className="max-w-[260px] text-xs">
                {t(`detail.prompt.mode.${promptMode}Desc`)}
              </TooltipContent>
            </Tooltip>
            <Badge
              variant="outline"
              className="text-2xs bg-blue-50 text-blue-700 hover:bg-blue-100 dark:bg-blue-900/30 dark:text-blue-300 cursor-pointer"
              onClick={() => setV3Open(true)}
            >
              V3
            </Badge>
            {agent.agent_type === "predefined" && (
              <Tooltip>
                <TooltipTrigger asChild>
                  <Badge
                    variant={selfEvolve ? "default" : "outline"}
                    className={`text-2xs ${selfEvolve ? "bg-orange-100 text-orange-700 hover:bg-orange-100 dark:bg-orange-900/30 dark:text-orange-300" : "text-muted-foreground"}`}
                  >
                    <Sparkles className="h-2.5 w-2.5 sm:mr-0.5" />
                    <span className="hidden sm:inline">{selfEvolve ? t("detail.evolving") : t("detail.static")}</span>
                  </Badge>
                </TooltipTrigger>
                <TooltipContent side="bottom" className="max-w-[240px] text-xs">
                  {selfEvolve ? t("detail.evolvingTooltipDetail") : t("detail.staticTooltipDetail")}
                </TooltipContent>
              </Tooltip>
            )}
            {hasOAuthRouting && (
              <Tooltip>
                <TooltipTrigger asChild>
                  <Badge variant="outline" className="text-2xs">
                    {t("chatgptOAuthRouting.badge")}
                  </Badge>
                </TooltipTrigger>
                <TooltipContent side="bottom" className="max-w-[240px] text-xs">
                  {t("chatgptOAuthRouting.badgeTooltip")}
                </TooltipContent>
              </Tooltip>
            )}
          </div>
          <div className="flex items-center gap-1.5 text-xs text-muted-foreground mt-0.5">
            <span className="font-mono text-xs-plus">{keyDisplay}</span>
            {agent.provider && (
              <>
                <span className="text-border">·</span>
                <span>{agent.provider} / {agent.model}</span>
              </>
            )}
          </div>
        </div>

        {/* System prompt preview */}
        <Button variant="ghost" size="sm" onClick={onSystemPrompt} className="shrink-0 gap-1.5">
          <Eye className="h-4 w-4" />
          <span className="hidden sm:inline">{t("files.systemPromptPreview")}</span>
        </Button>
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
        <Tooltip>
          <TooltipTrigger asChild>
            <Button variant="ghost" size="icon" onClick={onAdvanced} className="shrink-0 size-9">
              <Settings className="h-4 w-4" />
            </Button>
          </TooltipTrigger>
          <TooltipContent side="bottom" className="text-xs">
            {t("detail.advanced")}
          </TooltipContent>
        </Tooltip>
        <Tooltip>
          <TooltipTrigger asChild>
            <Button
              variant="ghost"
              size="icon"
              onClick={onDelete}
              className="shrink-0 size-9 text-destructive hover:text-destructive hover:bg-destructive/10"
            >
              <Trash2 className="h-4 w-4" />
            </Button>
          </TooltipTrigger>
          <TooltipContent side="bottom" className="text-xs">
            {t("delete.title")}
          </TooltipContent>
        </Tooltip>
      </div>
      <V3CapabilitiesModal open={v3Open} onOpenChange={setV3Open} />
    </TooltipProvider>
  );
}
