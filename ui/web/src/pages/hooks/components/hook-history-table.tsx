import { useTranslation } from "react-i18next";
import { useHookHistory } from "@/hooks/use-hooks";
import { TableSkeleton } from "@/components/shared/loading-skeleton";

interface HookHistoryTableProps {
  hookId: string;
}

const DECISION_STYLES: Record<string, string> = {
  allow: "text-emerald-600 dark:text-emerald-400",
  block: "text-red-600 dark:text-red-400",
  error: "text-amber-600 dark:text-amber-400",
  timeout: "text-slate-500",
};

export function HookHistoryTable({ hookId }: HookHistoryTableProps) {
  const { t } = useTranslation("hooks");
  const { data, isPending } = useHookHistory(hookId);

  if (isPending) return <TableSkeleton />;

  if (data?.note) {
    return (
      <div className="rounded-lg border bg-muted/40 px-4 py-6 text-center text-sm text-muted-foreground">
        {t("history.phaseFourNote")}
      </div>
    );
  }

  const executions = data?.executions ?? [];

  if (executions.length === 0) {
    return (
      <div className="rounded-lg border bg-muted/40 px-4 py-6 text-center text-sm text-muted-foreground">
        {t("history.empty")}
      </div>
    );
  }

  return (
    <div className="overflow-x-auto rounded-lg border">
      <table className="min-w-[600px] w-full text-sm">
        <thead className="border-b bg-muted/40">
          <tr>
            <th className="px-4 py-2 text-left text-xs font-medium text-muted-foreground">
              {t("history.decision")}
            </th>
            <th className="px-4 py-2 text-left text-xs font-medium text-muted-foreground">
              {t("history.duration")}
            </th>
            <th className="px-4 py-2 text-left text-xs font-medium text-muted-foreground">
              {t("history.error")}
            </th>
            <th className="px-4 py-2 text-left text-xs font-medium text-muted-foreground">
              {t("history.timestamp")}
            </th>
          </tr>
        </thead>
        <tbody>
          {executions.map((exec) => (
            <tr key={exec.id} className="border-b last:border-b-0 hover:bg-muted/20">
              <td className="px-4 py-2">
                <span className={`text-xs font-medium ${DECISION_STYLES[exec.decision] ?? ""}`}>
                  {t(`decision.${exec.decision}`, { defaultValue: exec.decision })}
                </span>
              </td>
              <td className="px-4 py-2 font-mono text-xs text-muted-foreground">
                {exec.duration_ms}ms
              </td>
              <td className="px-4 py-2 max-w-[200px] truncate text-xs text-muted-foreground">
                {exec.error ?? "—"}
              </td>
              <td className="px-4 py-2 text-xs text-muted-foreground">
                {new Date(exec.created_at).toLocaleString()}
              </td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}
