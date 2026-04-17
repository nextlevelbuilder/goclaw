import { RefreshCw, Loader2 } from "lucide-react";
import { useTranslation } from "react-i18next";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { formatRelativeTime } from "@/lib/format";
import type { UpdateInfo } from "../hooks/use-updates";

interface Props {
  updates: UpdateInfo[];
  checkedAt?: string;
  stale: boolean;
  loading: boolean;
  isMaster: boolean;
  onRefresh: () => void;
  onUpdateAll: () => void;
}

/**
 * Summary bar shown at the top of the GitHub Binaries section.
 * Visible when updates are available OR the cache is stale.
 */
export function UpdatesSummaryBar({
  updates,
  checkedAt,
  stale,
  loading,
  isMaster,
  onRefresh,
  onUpdateAll,
}: Props) {
  const { t } = useTranslation("packages");

  const hasUpdates = updates.length > 0;

  // Only render when there is something actionable to show
  if (!hasUpdates && !stale) return null;

  const lastChecked = checkedAt
    ? t("updates.lastCheckedAgo", { ago: formatRelativeTime(checkedAt) })
    : t("updates.neverChecked");

  return (
    <div className="flex flex-wrap items-center gap-3 rounded-lg border border-sky-200/70 bg-sky-50/70 dark:border-sky-900/50 dark:bg-sky-950/20 px-4 py-2.5 mb-3">
      {/* Badge + last-checked */}
      <div className="flex items-center gap-2 flex-1 min-w-0">
        {hasUpdates ? (
          <Badge variant="info">
            {t("updates.available", { count: updates.length })}
          </Badge>
        ) : (
          <Badge variant="warning">{t("updates.cacheStale")}</Badge>
        )}
        <span className="text-xs text-muted-foreground truncate">{lastChecked}</span>
      </div>

      {/* Actions */}
      <div className="flex items-center gap-2 shrink-0">
        <Button
          variant="outline"
          size="sm"
          onClick={onRefresh}
          disabled={loading}
          className="h-7 gap-1.5"
        >
          {loading ? (
            <Loader2 className="h-3.5 w-3.5 animate-spin" />
          ) : (
            <RefreshCw className="h-3.5 w-3.5" />
          )}
          {loading ? t("updates.refreshing") : t("updates.refresh")}
        </Button>

        {/* Update All — hidden for non-master users entirely (UX: only show the action if you can take it) */}
        {isMaster && (
          <Button
            size="sm"
            onClick={onUpdateAll}
            disabled={!hasUpdates || loading}
            className="h-7"
          >
            {t("updates.updateAll")}
          </Button>
        )}
      </div>
    </div>
  );
}
