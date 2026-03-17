import { useTranslation } from "react-i18next";
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { Badge } from "@/components/ui/badge";
import { formatDate } from "@/lib/format";
import type { CronRunLogEntry } from "./hooks/use-cron";

interface CronRunLogDialogProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  jobName: string;
  entries: CronRunLogEntry[];
  loading: boolean;
}

export function CronRunLogDialog({
  open,
  onOpenChange,
  jobName,
  entries,
  loading,
}: CronRunLogDialogProps) {
  const { t } = useTranslation("cron");

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-h-[70vh] flex flex-col sm:max-w-2xl">
        <DialogHeader>
          <DialogTitle>{t("runLog.title", { name: jobName })}</DialogTitle>
        </DialogHeader>

        <div className="overflow-y-auto min-h-0 -mx-4 px-4 sm:-mx-6 sm:px-6">
          {loading && entries.length === 0 ? (
            <div className="flex items-center justify-center py-8">
              <div className="h-6 w-6 animate-spin rounded-full border-2 border-muted-foreground border-t-transparent" />
            </div>
          ) : entries.length === 0 ? (
            <p className="py-8 text-center text-sm text-muted-foreground">
              {t("runLog.noHistory")}
            </p>
          ) : (
            <div className="space-y-2">
              {entries.map((entry: CronRunLogEntry, i: number) => (
                <div key={i} className="rounded-md border p-3 text-sm">
                  <div className="flex items-center justify-between">
                    <span className="text-muted-foreground">
                      {formatDate(new Date(entry.ts))}
                    </span>
                    <Badge
                      variant={entry.status === "ok" || entry.status === "success" ? "success" : "destructive"}
                    >
                      {entry.status || "unknown"}
                    </Badge>
                  </div>
                  {entry.summary && (
                    <p className="mt-1 text-muted-foreground">{entry.summary}</p>
                  )}
                  {entry.error && (
                    <p className="mt-1 text-destructive">{entry.error}</p>
                  )}
                </div>
              ))}
            </div>
          )}
        </div>
      </DialogContent>
    </Dialog>
  );
}
