import { useState } from "react";
import { useTranslation } from "react-i18next";
import { Download, Upload, RotateCcw } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Label } from "@/components/ui/label";
import { OperationProgress } from "@/components/shared/operation-progress";
import { BackupPreflightPanel } from "./backup-preflight-panel";
import { useSystemBackup } from "./hooks/use-system-backup";
import { useS3Config } from "./hooks/use-s3-config";
import { useS3Backups, type S3BackupEntry } from "./hooks/use-s3-backups";
import { formatFileSize } from "@/lib/format";
import { useBackupPreflight } from "./hooks/use-backup-preflight";

function S3HistoryTable({ backups }: { backups: S3BackupEntry[] }) {
  const { t } = useTranslation("backup");

  if (backups.length === 0) {
    return <p className="text-sm text-muted-foreground">{t("backup.s3History.empty")}</p>;
  }

  return (
    <div className="overflow-x-auto">
      <table className="w-full text-sm min-w-[400px]">
        <thead>
          <tr className="border-b text-left text-muted-foreground">
            <th className="pb-2 font-medium">{t("backup.s3History.name")}</th>
            <th className="pb-2 font-medium">{t("backup.s3History.size")}</th>
            <th className="pb-2 font-medium">{t("backup.s3History.date")}</th>
          </tr>
        </thead>
        <tbody>
          {backups.map((b) => (
            <tr key={b.key} className="border-b last:border-0">
              <td className="py-2 font-mono text-xs break-all">{b.key.split("/").pop()}</td>
              <td className="py-2 text-muted-foreground">{formatFileSize(b.size)}</td>
              <td className="py-2 text-muted-foreground">{new Date(b.last_modified).toLocaleDateString()}</td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}

export function SystemBackupPanel() {
  const { t } = useTranslation("backup");
  const [destination, setDestination] = useState<"local" | "s3">("local");

  const backup = useSystemBackup();
  const preflight = useBackupPreflight();
  const s3Config = useS3Config();
  const s3Backups = useS3Backups(s3Config.data?.configured ?? false);

  const s3Configured = s3Config.data?.configured ?? false;
  const hasCritical = preflight.data
    ? !preflight.data.pg_dump_available || !preflight.data.disk_space_ok
    : true;

  // Running state
  if (backup.status === "running") {
    return (
      <div className="space-y-4">
        <h3 className="text-sm font-medium">{t("backup.running")}</h3>
        <OperationProgress steps={backup.steps} elapsed={backup.elapsed} />
        <div className="flex justify-end">
          <Button variant="outline" onClick={backup.cancel}>
            {t("cancel", { ns: "common" })}
          </Button>
        </div>
      </div>
    );
  }

  // Complete state
  if (backup.status === "complete") {
    return (
      <div className="space-y-4">
        <h3 className="text-sm font-medium text-green-600">{t("backup.complete")}</h3>
        <OperationProgress steps={backup.steps} elapsed={backup.elapsed} />

        {backup.result && (
          <div className="rounded-md border bg-muted/50 p-3 text-sm space-y-1">
            {backup.result.total_bytes != null && (
              <div className="flex justify-between">
                <span className="text-muted-foreground">{t("backup.fileSize")}</span>
                <span className="font-mono text-xs">{formatFileSize(backup.result.total_bytes as number)}</span>
              </div>
            )}
            {backup.result.schema_version != null && (
              <div className="flex justify-between">
                <span className="text-muted-foreground">{t("backup.schemaVersion")}</span>
                <span className="font-mono text-xs">{String(backup.result.schema_version)}</span>
              </div>
            )}
          </div>
        )}

        <div className="flex items-center gap-2 justify-end">
          {backup.downloadReady && (
            <Button onClick={backup.download}>
              <Download className="mr-1.5 h-4 w-4" />
              {t("backup.download")}
            </Button>
          )}
          {backup.downloadReady && s3Configured && backup.backupToken && (
            <Button variant="outline" onClick={backup.uploadToS3}>
              <Upload className="mr-1.5 h-4 w-4" />
              {t("backup.uploadS3")}
            </Button>
          )}
          <Button variant="ghost" onClick={backup.reset}>
            <RotateCcw className="mr-1.5 h-4 w-4" />
            {t("backup.newBackup")}
          </Button>
        </div>
      </div>
    );
  }

  // Error state
  if (backup.status === "error") {
    return (
      <div className="space-y-4">
        <h3 className="text-sm font-medium text-destructive">{t("backup.errorTitle")}</h3>
        <OperationProgress steps={backup.steps} elapsed={backup.elapsed} />
        {backup.error && (
          <div className="rounded-md border border-destructive/30 bg-destructive/5 p-3 text-sm">
            <p className="text-destructive">{backup.error.detail}</p>
          </div>
        )}
        <div className="flex justify-end">
          <Button variant="outline" onClick={backup.reset}>
            <RotateCcw className="mr-1.5 h-4 w-4" />
            {t("backup.newBackup")}
          </Button>
        </div>
      </div>
    );
  }

  // Idle state
  return (
    <div className="space-y-5">
      <BackupPreflightPanel />

      {/* Destination toggle (only if S3 configured) */}
      {s3Configured && (
        <div className="space-y-2">
          <Label>{t("backup.options.destination")}</Label>
          <div className="flex gap-3">
            <label className="flex items-center gap-2 text-sm cursor-pointer">
              <input
                type="radio"
                name="dest"
                checked={destination === "local"}
                onChange={() => setDestination("local")}
                className="accent-primary"
              />
              {t("backup.options.local")}
            </label>
            <label className="flex items-center gap-2 text-sm cursor-pointer">
              <input
                type="radio"
                name="dest"
                checked={destination === "s3"}
                onChange={() => setDestination("s3")}
                className="accent-primary"
              />
              {t("backup.options.s3")}
            </label>
          </div>
        </div>
      )}

      <div className="flex justify-end">
        <Button
          onClick={() => backup.startBackup({ toS3: destination === "s3" })}
          disabled={hasCritical}
        >
          {t("backup.start")}
        </Button>
      </div>

      {/* S3 history */}
      {s3Configured && s3Backups.data?.backups && (
        <div className="space-y-3 border-t pt-4">
          <h3 className="text-sm font-medium">{t("backup.s3History.title")}</h3>
          <S3HistoryTable backups={s3Backups.data.backups} />
        </div>
      )}
    </div>
  );
}
