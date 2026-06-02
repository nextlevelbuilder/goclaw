import { useEffect } from "react";
import { useTranslation } from "react-i18next";
import { RefreshCw, CheckCircle, XCircle, ShieldOff } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";
import { Skeleton } from "@/components/ui/skeleton";
import { formatDate } from "@/lib/format";
import {
  useWorkstationActivity,
  type WorkstationActivity,
} from "./hooks/use-workstation-activity";

interface WorkstationActivityTabProps {
  workstationId: string;
}

// ActionBadge renders a coloured badge for exec/deny actions.
function ActionBadge({ action }: { action: WorkstationActivity["action"] }) {
  const { t } = useTranslation("workstations");
  if (action === "deny") {
    return (
      <Badge variant="destructive" className="gap-1 text-xs">
        <ShieldOff className="h-3 w-3" />
        {t("activity.actions.deny")}
      </Badge>
    );
  }
  return (
    <Badge variant="secondary" className="gap-1 text-xs">
      {t("activity.actions.exec")}
    </Badge>
  );
}

// ExitCodeCell shows exit code with a green/red icon.
function ExitCodeCell({ exitCode }: { exitCode: number | null }) {
  if (exitCode === null) return <span className="text-muted-foreground">—</span>;
  const ok = exitCode === 0;
  return (
    <span className="flex items-center gap-1">
      {ok ? (
        <CheckCircle className="h-3.5 w-3.5 text-green-500" />
      ) : (
        <XCircle className="h-3.5 w-3.5 text-red-500" />
      )}
      <span className={ok ? "text-green-700 dark:text-green-400" : "text-red-700 dark:text-red-400"}>
        {exitCode}
      </span>
    </span>
  );
}

function formatDuration(ms: number | null): string {
  if (ms === null) return "—";
  if (ms < 1000) return `${ms}ms`;
  return `${(ms / 1000).toFixed(1)}s`;
}

export function WorkstationActivityTab({ workstationId }: WorkstationActivityTabProps) {
  const { t } = useTranslation("workstations");
  const { rows, loading, error, hasMore, load, loadMore } = useWorkstationActivity();

  useEffect(() => {
    load(workstationId);
  }, [workstationId, load]);

  if (loading && rows.length === 0) {
    return (
      <div className="space-y-2 p-4">
        {Array.from({ length: 5 }).map((_, i) => (
          <Skeleton key={i} className="h-10 w-full" />
        ))}
      </div>
    );
  }

  if (error) {
    return (
      <div className="flex flex-col items-center gap-2 p-8 text-center">
        <p className="text-sm text-destructive">{error}</p>
        <Button variant="outline" size="sm" onClick={() => load(workstationId)}>
          {t("common:retry", "Retry")}
        </Button>
      </div>
    );
  }

  if (rows.length === 0) {
    return (
      <div className="flex flex-col items-center gap-2 p-12 text-center">
        <p className="font-medium text-muted-foreground">{t("activity.emptyTitle")}</p>
        <p className="text-sm text-muted-foreground">{t("activity.emptyDescription")}</p>
      </div>
    );
  }

  return (
    <div className="space-y-3 p-4">
      <div className="flex items-center justify-between">
        <p className="text-sm font-medium">{t("activity.title")}</p>
        <Button
          variant="ghost"
          size="sm"
          className="h-7 gap-1 text-xs"
          onClick={() => load(workstationId)}
          disabled={loading}
        >
          <RefreshCw className={"h-3 w-3" + (loading ? " animate-spin" : "")} />
          {t("common:refresh", "Refresh")}
        </Button>
      </div>

      {/* Table */}
      <div className="overflow-x-auto rounded-md border">
        <table className="min-w-[600px] w-full text-sm">
          <thead className="border-b bg-muted/50">
            <tr>
              <th className="px-3 py-2 text-left font-medium text-muted-foreground">
                {t("activity.columns.action")}
              </th>
              <th className="px-3 py-2 text-left font-medium text-muted-foreground">
                {t("activity.columns.cmdPreview")}
              </th>
              <th className="px-3 py-2 text-left font-medium text-muted-foreground">
                {t("activity.columns.exitCode")}
              </th>
              <th className="px-3 py-2 text-left font-medium text-muted-foreground">
                {t("activity.columns.duration")}
              </th>
              <th className="px-3 py-2 text-left font-medium text-muted-foreground">
                {t("activity.columns.timestamp")}
              </th>
            </tr>
          </thead>
          <tbody className="divide-y">
            {rows.map((row) => (
              <tr key={row.id} className="hover:bg-muted/30 transition-colors">
                <td className="px-3 py-2">
                  <ActionBadge action={row.action} />
                </td>
                <td className="px-3 py-2 font-mono text-xs text-muted-foreground max-w-[240px] truncate">
                  {row.cmdPreview || <span className="italic">—</span>}
                </td>
                <td className="px-3 py-2">
                  <ExitCodeCell exitCode={row.exitCode} />
                </td>
                <td className="px-3 py-2 text-muted-foreground">
                  {formatDuration(row.durationMs)}
                </td>
                <td className="px-3 py-2 text-muted-foreground whitespace-nowrap">
                  {formatDate(row.createdAt)}
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>

      {hasMore && (
        <div className="flex justify-center pt-2">
          <Button variant="outline" size="sm" onClick={loadMore} disabled={loading}>
            {loading ? (
              <RefreshCw className="h-3.5 w-3.5 animate-spin" />
            ) : (
              t("activity.loadMore")
            )}
          </Button>
        </div>
      )}
    </div>
  );
}
