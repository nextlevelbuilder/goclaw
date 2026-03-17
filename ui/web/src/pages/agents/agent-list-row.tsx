import { Bot, Star, Trash2, RotateCcw, Sparkles } from "lucide-react";
import { useTranslation } from "react-i18next";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip";
import type { AgentData } from "@/types/agent";

interface AgentListRowProps {
  agent: AgentData;
  ownerName?: string;
  onClick: () => void;
  onResummon?: () => void;
  onDelete?: () => void;
}

const UUID_RE = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i;

export function AgentListRow({ agent, ownerName, onClick, onResummon, onDelete }: AgentListRowProps) {
  const { t } = useTranslation("agents");
  const displayName = agent.display_name
    || (UUID_RE.test(agent.agent_key) ? t("card.unnamedAgent") : agent.agent_key);
  const otherCfg = (agent.other_config ?? {}) as Record<string, unknown>;
  const selfEvolve = agent.agent_type === "predefined" && Boolean(otherCfg.self_evolve);
  const emoji = typeof otherCfg.emoji === "string" ? otherCfg.emoji : "";

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
          <Badge variant="outline" className="animate-pulse border-violet-400 text-violet-600 dark:text-violet-400">
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

      {/* Type + evolve */}
      <div className="hidden shrink-0 items-center gap-1 lg:flex">
        <Badge variant="outline" className="text-[11px]">{agent.agent_type}</Badge>
        {selfEvolve && (
          <Tooltip>
            <TooltipTrigger asChild>
              <Badge className="bg-violet-100 text-[11px] text-violet-700 hover:bg-violet-100 dark:bg-violet-900/30 dark:text-violet-300">
                <Sparkles className="mr-0.5 h-3 w-3" />
                {t("card.evolving")}
              </Badge>
            </TooltipTrigger>
            <TooltipContent side="top" className="max-w-[240px] text-xs">
              {t("card.evolvingTooltip")}
            </TooltipContent>
          </Tooltip>
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
        <span className="hidden shrink-0 text-[11px] text-muted-foreground lg:block">
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
