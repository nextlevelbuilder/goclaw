import { Bot, Star, RotateCcw, Trash2, Sparkles } from "lucide-react";
import { useTranslation } from "react-i18next";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip";
import type { AgentData } from "@/types/agent";

interface AgentCardProps {
  agent: AgentData;
  onClick: () => void;
  onResummon?: () => void;
  onDelete?: () => void;
}

const UUID_RE = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i;

export function AgentCard({ agent, onClick, onResummon, onDelete }: AgentCardProps) {
  const { t } = useTranslation("agents");
  const displayName = agent.display_name
    || (UUID_RE.test(agent.agent_key) ? t("card.unnamedAgent") : agent.agent_key);
  const otherCfg = (agent.other_config ?? {}) as Record<string, unknown>;
  const selfEvolve = agent.agent_type === "predefined" && Boolean(otherCfg.self_evolve);
  const emoji = typeof otherCfg.emoji === "string" ? otherCfg.emoji : "";

  // Show agent_key as subtitle only if there's a display_name and agent_key is meaningful
  const showSubtitle = agent.display_name && !UUID_RE.test(agent.agent_key);

  return (
    <button
      type="button"
      onClick={onClick}
      className="flex cursor-pointer flex-col gap-3 rounded-lg border bg-card p-4 text-left transition-all hover:border-primary/30 hover:shadow-md"
    >
      {/* Top row: icon + name + status */}
      <div className="flex items-center gap-3">
        <div className="flex h-9 w-9 shrink-0 items-center justify-center rounded-lg bg-primary/10 text-primary">
          {emoji ? <span className="text-lg leading-none">{emoji}</span> : <Bot className="h-4.5 w-4.5" />}
        </div>
        <div className="min-w-0 flex-1">
          <div className="flex items-center gap-2">
            <span className="truncate text-sm font-semibold">{displayName}</span>
            {agent.is_default && (
              <Star className="h-3.5 w-3.5 shrink-0 fill-amber-400 text-amber-400" />
            )}
          </div>
          {showSubtitle && (
            <div className="truncate text-xs text-muted-foreground">{agent.agent_key}</div>
          )}
        </div>
        {agent.status === "summoning" ? (
          <Badge variant="outline" className="shrink-0 animate-pulse border-orange-400 text-orange-600 dark:text-orange-400">
            {t("card.summoning")}
          </Badge>
        ) : agent.status === "summon_failed" ? (
          <Badge variant="destructive" className="shrink-0">
            {t("card.summonFailed")}
          </Badge>
        ) : (
          <Badge variant={agent.status === "active" ? "success" : "secondary"} className="shrink-0">
            {agent.status}
          </Badge>
        )}
      </div>

      {/* Model info */}
      {(agent.provider || agent.model) && (
        <div className="truncate text-xs text-muted-foreground">
          {[agent.provider, agent.model].filter(Boolean).join(" / ")}
        </div>
      )}

      {/* Expertise summary */}
      {agent.frontmatter && (
        <div className="line-clamp-3 text-xs text-muted-foreground/70">
          {agent.frontmatter}
        </div>
      )}

      {/* Bottom badges */}
      <div className="flex items-center gap-1.5">
        <Tooltip>
          <TooltipTrigger asChild>
            <Badge variant="outline" className="text-[11px]">{agent.agent_type}</Badge>
          </TooltipTrigger>
          <TooltipContent side="top" className="max-w-[260px] text-xs">
            {agent.agent_type === "predefined"
              ? t("card.predefinedTooltip")
              : t("card.openTooltip")}
          </TooltipContent>
        </Tooltip>
        {agent.agent_type === "predefined" && (
          <Tooltip>
            <TooltipTrigger asChild>
              <Badge
                variant={selfEvolve ? "default" : "outline"}
                className={`text-[11px] ${selfEvolve ? "bg-orange-100 text-orange-700 hover:bg-orange-100 dark:bg-orange-900/30 dark:text-orange-300" : "text-muted-foreground"}`}
              >
                <Sparkles className="mr-0.5 h-3 w-3" />
                {selfEvolve ? t("card.evolving") : t("card.static")}
              </Badge>
            </TooltipTrigger>
            <TooltipContent side="top" className="max-w-[240px] text-xs">
              {selfEvolve
                ? t("card.evolvingTooltip")
                : t("card.staticTooltip")}
            </TooltipContent>
          </Tooltip>
        )}
        {agent.context_window > 0 && (
          <span className="text-[11px] text-muted-foreground">
            {(agent.context_window / 1000).toFixed(0)}K ctx
          </span>
        )}
        {agent.status === "summon_failed" && onResummon && (
          <Button
            variant="outline"
            size="xs"
            className="ml-auto"
            onClick={(e) => {
              e.stopPropagation();
              onResummon();
            }}
          >
            <RotateCcw className="h-3 w-3" />
            {t("card.resummon")}
          </Button>
        )}
        {onDelete && (
          <Button
            variant="ghost"
            size="xs"
            className={`text-muted-foreground hover:text-destructive ${agent.status === "summon_failed" && onResummon ? "" : "ml-auto"}`}
            onClick={(e) => {
              e.stopPropagation();
              onDelete();
            }}
          >
            <Trash2 className="h-3.5 w-3.5" />
            {t("card.delete")}
          </Button>
        )}
      </div>
    </button>
  );
}
