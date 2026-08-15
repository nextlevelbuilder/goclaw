import { Bot, Star, Trash2, RotateCcw, Sparkles } from "lucide-react";
import { useTranslation } from "react-i18next";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip";
import type { AgentData } from "@/types/agent";
import { cn } from "@/lib/utils";
import { UUID_RE, agentDisplayName, hasActiveChatGPTOAuthRouting, readPromptMode } from "./agent-detail/agent-display-utils";
import { promptModeBadgeClass } from "./agent-detail/prompt-mode-badge-utils";

interface AgentListRowProps {
  agent: AgentData;
  ownerName?: string;
  onClick: () => void;
  onResummon?: () => void;
  onDelete?: () => void;
}

export function AgentListRow({ agent, ownerName, onClick, onResummon, onDelete }: AgentListRowProps) {
  const { t } = useTranslation("agents");
  const displayName = agentDisplayName(agent, t("card.unnamedAgent"));
  const selfEvolve = agent.agent_type === "predefined" && Boolean(agent.self_evolve);
  const emoji = agent.emoji ?? "";
  const hasOAuthRouting = hasActiveChatGPTOAuthRouting(agent.chatgpt_oauth_routing);
  const promptMode = readPromptMode(agent);

  return (
    <button
      type="button"
      onClick={onClick}
      className="flex w-full cursor-pointer items-center gap-3 rounded-lg border bg-card px-4 py-3 text-left transition-all hover:border-primary/30 hover:shadow-sm"
    >
      {/* Icon */}
      <div className="flex h-8 w-8 shrink-0 items-center justify-center rounded-lg bg-primary/10 text-primary">
        {emoji ? <span className="text-base leading-none">{emoji}</span> : <Bot className="h-4 w-4" />}
      </div>

      {/* Name + key */}
      <div className="min-w-0 flex-1">
        <div className="flex items-center gap-1.5">
          <span className="truncate text-sm font-semibold">{displayName}</span>
          {agent.is_default && <Star className="h-3 w-3 shrink-0 fill-amber-400 text-amber-400" />}
        </div>
        {agent.display_name && !UUID_RE.test(agent.agent_key) && (
          <div className="truncate text-xs text-muted-foreground">{agent.agent_key}</div>
        )}
      </div>

      {/* Status */}
      <div className="hidden shrink-0 sm:block">
        {agent.status === "summoning" ? (
          <Badge variant="outline" className="animate-pulse border-orange-400 text-orange-600 dark:text-orange-400">
            {t("card.summoning")}
          </Badge>
        ) : agent.status === "summon_failed" ? (
          <Badge variant="destructive">{t("card.summonFailed")}</Badge>
        ) : (
          <Badge variant={agent.status === "active" ? "success" : "secondary"}>{agent.status}</Badge>
        )}
      </div>

      {/* Model */}
      <div className="hidden shrink-0 text-xs text-muted-foreground md:block md:w-40 md:truncate">
        {[agent.provider, agent.model].filter(Boolean).join(" / ")}
      </div>

      {/* Version + evolve */}
      <div className="hidden shrink-0 items-center gap-1 lg:flex">
        <Tooltip>
          <TooltipTrigger asChild>
            <Badge
              variant="outline"
              className={cn("text-xs-plus", promptModeBadgeClass(promptMode))}
            >
              {t(`detail.prompt.mode.${promptMode}`)}
            </Badge>
          </TooltipTrigger>
          <TooltipContent side="top" className="max-w-[260px] text-xs">
            {t(`detail.prompt.mode.${promptMode}Desc`)}
          </TooltipContent>
        </Tooltip>
        {selfEvolve && (
          <Tooltip>
            <TooltipTrigger asChild>
              <Badge className="bg-orange-100 text-xs-plus text-orange-700 hover:bg-orange-100 dark:bg-orange-900/30 dark:text-orange-300">
                <Sparkles className="mr-0.5 h-3 w-3" />
                {t("card.evolving")}
              </Badge>
            </TooltipTrigger>
            <TooltipContent side="top" className="max-w-[240px] text-xs">
              {t("card.evolvingTooltip")}
            </TooltipContent>
          </Tooltip>
        )}
        {hasOAuthRouting && (
          <Badge variant="outline" className="text-xs-plus">
            {t("chatgptOAuthRouting.badge")}
          </Badge>
        )}
      </div>

      {/* Owner */}
      {ownerName && (
        <div className="hidden shrink-0 text-xs text-muted-foreground xl:block xl:w-28 xl:truncate">
          {ownerName}
        </div>
      )}

      {/* Context window */}
      {agent.context_window > 0 && (
        <span className="hidden shrink-0 text-xs-plus text-muted-foreground lg:block">
          {(agent.context_window / 1000).toFixed(0)}K
        </span>
      )}

      {/* Actions */}
      <div className="flex shrink-0 items-center gap-1">
        {agent.status === "summon_failed" && onResummon && (
          <Button
            variant="outline"
            size="xs"
            onClick={(e) => { e.stopPropagation(); onResummon(); }}
          >
            <RotateCcw className="h-3 w-3" />
          </Button>
        )}
        {onDelete && (
          <Button
            variant="ghost"
            size="xs"
            className="text-muted-foreground hover:text-destructive"
            onClick={(e) => { e.stopPropagation(); onDelete(); }}
          >
            <Trash2 className="h-3.5 w-3.5" />
          </Button>
        )}
      </div>
    </button>
  );
}
