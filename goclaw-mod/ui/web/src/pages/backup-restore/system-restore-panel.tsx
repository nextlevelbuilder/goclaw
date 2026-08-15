import { useState, useRef, useCallback } from "react";
import { useTranslation } from "react-i18next";
import { Upload, AlertTriangle, RotateCcw, FileArchive } from "lucide-react";
import { Button } from "@/components/ui/button";
import { OperationProgress } from "@/components/shared/operation-progress";
import { ConfirmDialog } from "@/components/shared/confirm-dialog";
import { formatFileSize } from "@/lib/format";
import { useSystemRestore } from "./hooks/use-system-restore";

export function SystemRestorePanel() {
  const { t } = useTranslation("backup");
  const restore = useSystemRestore();
  const fileRef = useRef<HTMLInputElement>(null);

  const [file, setFile] = useState<File | null>(null);
  const [dragging, setDragging] = useState(false);
  const [skipDb, setSkipDb] = useState(false);
  const [skipFiles, setSkipFiles] = useState(false);
  const [dryRun, setDryRun] = useState(false);
  const [confirmOpen, setConfirmOpen] = useState(false);

  const handleFile = useCallback((f: File) => setFile(f), []);

  const handleDrop = useCallback((e: React.DragEvent) => {
    e.preventDefault();
    setDragging(false);
    const f = e.dataTransfer.files[0];
    if (f) handleFile(f);
  }, [handleFile]);

  const handleConfirm = () => {
    if (!file) return;
    setConfirmOpen(false);
    restore.startRestore(file, { skipDb, skipFiles, dryRun });
  };

  const handleReset = () => {
    setFile(null);
    setSkipDb(false);
    setSkipFiles(false);
    setDryRun(false);
    restore.reset();
  };

  // Running state
  if (restore.status === "running") {
    return (
      <div className="space-y-4">
        <h3 className="text-sm font-medium">{t("restore.running")}</h3>
        <OperationProgress steps={restore.steps} elapsed={restore.elapsed} />
        <p className="text-xs text-amber-600 dark:text-amber-400 flex items-center gap-1">
          <AlertTriangle className="h-3.5 w-3.5" />
          {t("restore.doNotClose")}
        </p>
        <div className="flex justify-end">
          <Button variant="outline" onClick={restore.cancel}>
            {t("cancel", { ns: "common" })}
          </Button>
        </div>
      </div>
    );
  }

  // Complete state
  if (restore.status === "complete" && restore.result) {
    const r = restore.result as Record<string, unknown>;
    const warnings = (r.warnings ?? []) as string[];
    return (
      <div className="space-y-4">
        <h3 className="text-sm font-medium text-green-600">{t("restore.complete")}</h3>
        <OperationProgress steps={restore.steps} elapsed={restore.elapsed} />

        {!!r.dry_run && (
          <div className="rounded-md border border-blue-200 bg-blue-50 dark:border-blue-900/40 dark:bg-blue-950/20 px-3 py-2 text-sm text-blue-700 dark:text-blue-300">
            {t("restore.dryRunNote")}
          </div>
        )}

        <div className="rounded-md border bg-muted/50 p-3 text-sm space-y-1">
          <div className="flex justify-between">
            <span className="text-muted-foreground">{t("restore.dbRestored")}</span>
            <span>{String(r.database_restored) === "true" ? "Yes" : "No"}</span>
          </div>
          {r.files_extracted != null && (
            <div className="flex justify-between">
              <span className="text-muted-foreground">{t("restore.filesExtracted")}</span>
              <span className="font-mono text-xs">{String(r.files_extracted)}</span>
            </div>
          )}
          {typeof r.bytes_extracted === "number" && r.bytes_extracted > 0 && (
            <div className="flex justify-between">
              <span className="text-muted-foreground">{t("restore.bytesExtracted")}</span>
              <span className="font-mono text-xs">{formatFileSize(r.bytes_extracted)}</span>
            </div>
          )}
          {r.schema_version != null && (
            <div className="flex justify-between">
              <span className="text-muted-foreground">{t("restore.schemaVersion")}</span>
              <span className="font-mono text-xs">{String(r.schema_version)}</span>
            </div>
          )}
        </div>

        {warnings.length > 0 && (
          <div className="space-y-1">
            <p className="text-xs font-medium text-amber-600">{t("restore.warnings")}</p>
            {warnings.map((w, i) => (
              <p key={i} className="text-xs text-amber-600">{w}</p>
            ))}
          </div>
        )}

        <div className="flex justify-end">
          <Button variant="outline" onClick={handleReset}>
            <RotateCcw className="mr-1.5 h-4 w-4" />
            {t("restore.newRestore")}
          </Button>
        </div>
      </div>
    );
  }

  // Error state
  if (restore.status === "error") {
    return (
      <div className="space-y-4">
        <h3 className="text-sm font-medium text-destructive">{t("restore.errorTitle")}</h3>
        <OperationProgress steps={restore.steps} elapsed={restore.elapsed} />
        {restore.error && (
          <div className="rounded-md border border-destructive/30 bg-destructive/5 p-3 text-sm">
            <p className="text-destructive">{restore.error.detail}</p>
          </div>
        )}
        <div className="flex justify-end">
          <Button variant="outline" onClick={handleReset}>
            <RotateCcw className="mr-1.5 h-4 w-4" />
            {t("restore.tryAgain")}
          </Button>
        </div>
      </div>
    );
  }

  // Idle state
  return (
    <div className="space-y-4">
      {/* Warning banner */}
      <div className="flex items-start gap-2.5 rounded-md border border-destructive/30 bg-destructive/5 px-4 py-3 text-sm text-destructive">
        <AlertTriangle className="mt-0.5 h-4 w-4 shrink-0" />
        <span>{t("restore.warning")}</span>
      </div>

      {/* Drop zone */}
      {!file && (
        <div
          className={`flex flex-col items-center justify-center gap-2 rounded-lg border-2 border-dashed p-8 transition-colors cursor-pointer ${
            dragging ? "border-primary bg-primary/5" : "border-muted-foreground/25 hover:border-muted-foreground/50"
          }`}
          onDragOver={(e) => { e.preventDefault(); setDragging(true); }}
          onDragLeave={() => setDragging(false)}
          onDrop={handleDrop}
          onClick={() => fileRef.current?.click()}
        >
          <Upload className="h-8 w-8 text-muted-foreground/50" />
          <p className="text-sm text-muted-foreground">{t("restore.dropzone")}</p>
          <p className="text-xs text-muted-foreground/60">{t("restore.dropzoneHint")}</p>
          <input ref={fileRef} type="file" className="hidden" accept=".tar.gz,.gz"
            onChange={(e) => { const f = e.target.files?.[0]; if (f) handleFile(f); }} />
        </div>
      )}

      {/* File info + options */}
      {file && (
        <>
          <div className="rounded-md border bg-muted/50 p-3 text-sm">
            <div className="flex items-center gap-2">
              <FileArchive className="h-4 w-4 text-muted-foreground" />
              <span className="font-medium">{file.name}</span>
              <span className="text-xs text-muted-foreground ml-auto">{formatFileSize(file.size)}</span>
            </div>
          </div>

          <div className="space-y-2">
            <label className="flex items-center gap-2 text-sm cursor-pointer">
              <input type="checkbox" checked={!skipDb} onChange={(e) => setSkipDb(!e.target.checked)} className="accent-primary" />
              {t("restore.options.restoreDb")}
            </label>
            <label className="flex items-center gap-2 text-sm cursor-pointer">
              <input type="checkbox" checked={!skipFiles} onChange={(e) => setSkipFiles(!e.target.checked)} className="accent-primary" />
              {t("restore.options.restoreFiles")}
            </label>
            <label className="flex items-center gap-2 text-sm cursor-pointer">
              <input type="checkbox" checked={dryRun} onChange={(e) => setDryRun(e.target.checked)} className="accent-primary" />
              {t("restore.options.dryRun")}
            </label>
          </div>

          <div className="flex items-center justify-end gap-2 pt-2">
            <Button variant="outline" onClick={() => setFile(null)}>
              {t("cancel", { ns: "common" })}
            </Button>
            <Button variant="destructive" onClick={() => setConfirmOpen(true)}>
              {t("restore.start")}
            </Button>
          </div>
        </>
      )}

      <ConfirmDialog
        open={confirmOpen}
        onOpenChange={setConfirmOpen}
        title={t("restore.confirmTitle")}
        description={t("restore.confirmDesc")}
        variant="destructive"
        onConfirm={handleConfirm}
      />
    </div>
  );
}
