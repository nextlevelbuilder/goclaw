import { Clock, Play, Trash2, Loader2 } from "lucide-react";
import { useTranslation } from "react-i18next";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import type { CronJob } from "./hooks/use-cron";
import { formatSchedule, CronStatusBadge } from "./cron-utils";

interface CronListRowProps {
  job: CronJob;
  onClick: () => void;
  onRun?: () => void;
  onDelete?: () => void;
}

export function CronListRow({ job, onClick, onRun, onDelete }: CronListRowProps) {
  const { t } = useTranslation("cron");
  const isRunning = job.state?.lastStatus === "running";

  return (
    <button
      type="button"
      onClick={onClick}
      className="flex w-full cursor-pointer items-center gap-3 rounded-lg border bg-card px-4 py-3 text-left transition-all hover:border-primary/30 hover:shadow-sm"
    >
      {/* Icon */}
      <div className="flex h-8 w-8 shrink-0 items-center justify-center rounded-lg bg-primary/10 text-primary">
        <Clock className="h-4 w-4" />
      </div>

      {/* Name + schedule */}
      <div className="min-w-0 flex-1">
        <div className="flex items-center gap-2">
          <span className="truncate text-sm font-semibold">{job.name}</span>
          <span className={`inline-block h-2 w-2 shrink-0 rounded-full ${job.enabled ? "bg-emerald-500" : "bg-muted-foreground/40"}`} />
        </div>
        <div className="flex items-center gap-1.5 text-xs text-muted-foreground">
          <Badge variant="outline" className="text-[10px] px-1 py-0">{job.schedule.kind}</Badge>
          <span className="truncate">{formatSchedule(job)}</span>
        </div>
      </div>

      {/* Status */}
      <div className="hidden shrink-0 sm:block">
        <CronStatusBadge status={job.state?.lastStatus} />
      </div>

      {/* Agent */}
      <div className="hidden shrink-0 text-xs text-muted-foreground md:block md:w-28 md:truncate">
        {job.agentId || t("card.defaultAgent")}
      </div>

      {/* Message preview */}
      <div className="hidden shrink-0 text-xs text-muted-foreground/60 lg:block lg:w-40 lg:truncate">
        {job.payload?.message}
      </div>

      {/* Actions */}
      <div className="flex shrink-0 items-center gap-1">
        {onRun && (
          <Button
            variant="ghost"
            size="xs"
            className="text-muted-foreground hover:text-primary"
            disabled={isRunning}
            onClick={(e) => { e.stopPropagation(); onRun(); }}
          >
            {isRunning
              ? <Loader2 className="h-3.5 w-3.5 animate-spin" />
              : <Play className="h-3.5 w-3.5" />}
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
