import { Badge } from "@/components/ui/badge";
import type { CronJob } from "./hooks/use-cron";

export function formatSchedule(job: CronJob): string {
  const s = job.schedule;
  if (s.kind === "every" && s.everyMs) {
    const sec = s.everyMs / 1000;
    if (sec < 60) return `every ${sec}s`;
    if (sec < 3600) return `every ${Math.round(sec / 60)}m`;
    return `every ${Math.round(sec / 3600)}h`;
  }
  if (s.kind === "cron" && s.expr) return s.expr;
  if (s.kind === "at" && s.atMs) return `once at ${new Date(s.atMs).toLocaleString()}`;
  return s.kind;
}

/** Status badge for cron jobs. */
export function CronStatusBadge({ status }: { status?: string }) {
  if (!status) return null;
  if (status === "running") {
    return (
      <Badge variant="outline" className="shrink-0 animate-pulse border-blue-400 text-blue-600 dark:text-blue-400">
        {status}
      </Badge>
    );
  }
  if (status === "ok" || status === "success") {
    return <Badge variant="success" className="shrink-0">{status}</Badge>;
  }
  if (status === "error" || status === "failed") {
    return <Badge variant="destructive" className="shrink-0">{status}</Badge>;
  }
  return <Badge variant="outline" className="shrink-0">{status}</Badge>;
}
