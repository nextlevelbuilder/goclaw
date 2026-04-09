import { useTranslation } from "react-i18next";
import { CheckCircle2, XCircle, RefreshCw, AlertTriangle, Loader2 } from "lucide-react";
import { Button } from "@/components/ui/button";
import { useBackupPreflight } from "./hooks/use-backup-preflight";

export function BackupPreflightPanel() {
  const { t } = useTranslation("backup");
  const { data, isLoading, refetch, isFetching } = useBackupPreflight();

  if (isLoading) {
    return (
      <div className="rounded-lg border bg-card p-4 space-y-2">
        <div className="flex items-center gap-2 text-sm text-muted-foreground">
          <Loader2 className="h-4 w-4 animate-spin" />
          {t("backup.preflight.title")}...
        </div>
      </div>
    );
  }

  if (!data) return null;

  const checks = [
    { ok: data.pg_dump_available, label: t("backup.preflight.pgDump") },
    { ok: data.disk_space_ok, label: t("backup.preflight.diskSpace") },
  ];

  const info = [
    { label: t("backup.preflight.dbSize"), value: data.db_size_human },
    { label: t("backup.preflight.dataDir"), value: data.data_dir_size_human },
    { label: t("backup.preflight.workspace"), value: data.workspace_size_human },
    { label: t("backup.preflight.freeDisk"), value: data.free_disk_human },
  ];

  const hasCritical = !data.pg_dump_available || !data.disk_space_ok;

  return (
    <div className="rounded-lg border bg-card p-4 space-y-3">
      <div className="flex items-center justify-between">
        <h3 className="text-sm font-medium">{t("backup.preflight.title")}</h3>
        <Button variant="ghost" size="sm" onClick={() => refetch()} disabled={isFetching}>
          <RefreshCw className={`h-3.5 w-3.5 ${isFetching ? "animate-spin" : ""}`} />
          <span className="ml-1.5">{t("backup.preflight.refresh")}</span>
        </Button>
      </div>

      {checks.map((c) => (
        <div key={c.label} className="flex items-center gap-2 text-sm">
          {c.ok ? (
            <CheckCircle2 className="h-4 w-4 text-green-500" />
          ) : (
            <XCircle className="h-4 w-4 text-destructive" />
          )}
          <span>{c.label}</span>
        </div>
      ))}

      <div className="grid grid-cols-2 gap-x-4 gap-y-1 text-sm text-muted-foreground pt-1">
        {info.map((i) => (
          <div key={i.label} className="flex justify-between">
            <span>{i.label}</span>
            <span className="font-mono text-xs">{i.value}</span>
          </div>
        ))}
      </div>

      {data.warnings?.length > 0 && (
        <div className="space-y-1 pt-1">
          <p className="text-xs font-medium text-amber-600 dark:text-amber-400">{t("backup.preflight.warnings")}</p>
          {data.warnings.map((w, i) => (
            <div key={i} className="flex items-start gap-1.5 text-xs text-amber-600 dark:text-amber-400">
              <AlertTriangle className="h-3 w-3 mt-0.5 shrink-0" />
              <span>{w}</span>
            </div>
          ))}
        </div>
      )}

      {hasCritical && (
        <p className="text-xs text-destructive">
          {t("backup.preflight.critical", { defaultValue: "Critical checks failed. Backup may not work correctly." })}
        </p>
      )}
    </div>
  );
}
