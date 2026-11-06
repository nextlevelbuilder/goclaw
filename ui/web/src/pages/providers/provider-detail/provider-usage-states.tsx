import { useTranslation } from "react-i18next";
import { CircleAlert, CloudOff, RefreshCw } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Skeleton } from "@/components/ui/skeleton";
import { cn } from "@/lib/utils";
import { formatEventTime } from "./provider-usage-sections";

export function UsageErrorCard({ onRetry, retrying }: { onRetry: () => void; retrying: boolean }) {
  const { t } = useTranslation("providers");
  return (
    <div role="alert" className="flex flex-col items-center gap-3 rounded-lg border border-destructive/40 bg-destructive/5 p-6 text-center">
      <CircleAlert className="h-6 w-6 text-destructive" aria-hidden="true" />
      <div className="space-y-1">
        <p className="text-sm font-medium">{t("usage.error.title")}</p>
        <p className="text-sm text-muted-foreground">{t("usage.error.description")}</p>
      </div>
      <Button type="button" variant="outline" size="sm" onClick={onRetry} disabled={retrying} aria-busy={retrying}>
        <RefreshCw className={cn("h-3.5 w-3.5", retrying && "animate-spin")} aria-hidden="true" />
        {t("usage.error.retry")}
      </Button>
    </div>
  );
}

export function StaleDataNote({
  updatedAt,
  tz,
  onRetry,
  retrying,
}: {
  updatedAt: number;
  tz: string;
  onRetry: () => void;
  retrying: boolean;
}) {
  const { t, i18n } = useTranslation("providers");
  return (
    <div role="status" className="flex flex-wrap items-center gap-3 rounded-lg border bg-muted/50 p-3 text-sm">
      <CloudOff className="h-4 w-4 shrink-0 text-muted-foreground" aria-hidden="true" />
      <div className="min-w-0 flex-1">
        <p className="font-medium">{t("usage.stale.title")}</p>
        <p className="text-muted-foreground">
          {t("usage.stale.description", { time: formatEventTime(updatedAt, tz, i18n.language) })}
        </p>
      </div>
      <Button type="button" variant="outline" size="sm" onClick={onRetry} disabled={retrying} aria-busy={retrying}>
        <RefreshCw className={cn("h-3.5 w-3.5", retrying && "animate-spin")} aria-hidden="true" />
        {t("usage.stale.retry")}
      </Button>
    </div>
  );
}

export function UsageSkeleton() {
  const { t } = useTranslation("providers");
  const pulse = "motion-reduce:animate-none";
  return (
    <div role="status" aria-busy="true" aria-label={t("usage.loading")} className="space-y-4">
      <div className="grid grid-cols-1 gap-3 sm:grid-cols-2 lg:grid-cols-4">
        {Array.from({ length: 4 }, (_, i) => (
          <Skeleton key={i} className={cn("h-24", pulse)} />
        ))}
      </div>
      <Skeleton className={cn("h-[340px]", pulse)} />
      <Skeleton className={cn("h-40", pulse)} />
    </div>
  );
}
