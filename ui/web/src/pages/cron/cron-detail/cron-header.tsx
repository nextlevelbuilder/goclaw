import { useTranslation } from "react-i18next";
import { ArrowLeft, Clock, Loader2, Play, Power, Trash2 } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";
import { Tooltip, TooltipContent, TooltipProvider, TooltipTrigger } from "@/components/ui/tooltip";
import { cn } from "@/lib/utils";
import type { CronJob } from "../hooks/use-cron";
import { formatSchedule } from "../cron-utils";
import { useAgents } from "@/pages/agents/hooks/use-agents";

interface CronHeaderProps {
  job: CronJob;
  isRunning: boolean;
  onBack: () => void;
  onRun: () => void;
  onToggle: () => void;
  onDelete: () => void;
}


export function CronHeader({ job, isRunning, onBack, onRun, onToggle, onDelete }: CronHeaderProps) {
  const { t } = useTranslation("cron");
  const { agents } = useAgents();
  const agent = job.agentId ? agents.find((a) => a.id === job.agentId) : null;
  const agentLabel = agent?.display_name || agent?.agent_key || job.agentId;

  return (
    <TooltipProvider>
      <div className="sticky top-0 z-10 flex items-center gap-2 border-b bg-card px-3 py-2 landscape-compact sm:px-4 sm:gap-3">
        <Button variant="ghost" size="icon" onClick={onBack} className="shrink-0 size-9">
          <ArrowLeft className="h-4 w-4" />
        </Button>

        {/* Clock icon */}
        <div className="flex h-10 w-10 shrink-0 items-center justify-center rounded-xl bg-primary/10 text-primary sm:h-12 sm:w-12">
          <Clock className="h-5 w-5 sm:h-6 sm:w-6" />
        </div>

        {/* Job info */}
        <div className="min-w-0 flex-1">
          <div className="flex items-center gap-1.5 flex-wrap">
            <h2 className="truncate text-base font-semibold">{job.name}</h2>
            <Badge variant={job.enabled ? "success" : "secondary"} className="text-2xs">
              {job.enabled ? t("detail.enabled") : t("detail.disabled")}
            </Badge>
            <Tooltip>
              <TooltipTrigger asChild>
                <span
                  className={cn(
                    "inline-block h-2.5 w-2.5 shrink-0 rounded-full",
                    job.enabled ? "bg-emerald-500" : "bg-muted-foreground/50",
                  )}
                />
              </TooltipTrigger>
              <TooltipContent side="bottom" className="text-xs">
                {job.enabled ? t("detail.enabled") : t("detail.disabled")}
              </TooltipContent>
            </Tooltip>
          </div>
          <div className="flex items-center gap-1.5 text-xs text-muted-foreground mt-0.5">
            <Badge variant="outline" className="text-2xs">{job.schedule.kind}</Badge>
            <span className="text-border">·</span>
            <span>{formatSchedule(job)}</span>
            {job.agentId && (
              <>
                <span className="text-border">·</span>
                <span className="text-xs-plus">{agentLabel}</span>
              </>
            )}
          </div>
        </div>

        {/* Run Now */}
        <Button
          variant="ghost"
          size="sm"
          onClick={onRun}
          disabled={isRunning}
          className="shrink-0 gap-1.5 size-9 sm:w-auto sm:px-3"
        >
          {isRunning
            ? <Loader2 className="h-4 w-4 animate-spin" />
            : <Play className="h-4 w-4" />}
          <span className="hidden sm:inline">
            {isRunning ? t("detail.running") : t("detail.runNow")}
          </span>
        </Button>

        {/* Toggle */}
        <Button
          variant="ghost"
          size="sm"
          onClick={onToggle}
          className="shrink-0 gap-1.5 size-9 sm:w-auto sm:px-3"
        >
          <span className="hidden sm:inline">
            {job.enabled ? t("detail.disable") : t("detail.enable")}
          </span>
          <Power className="h-4 w-4 sm:hidden" />
        </Button>

        {/* Delete */}
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
