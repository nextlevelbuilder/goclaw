import { useState, useRef, useCallback } from "react";
import { useTranslation } from "react-i18next";
import { Upload, RotateCcw, FileArchive } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Label } from "@/components/ui/label";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Combobox } from "@/components/ui/combobox";
import { OperationProgress } from "@/components/shared/operation-progress";
import { ConfirmDialog } from "@/components/shared/confirm-dialog";
import { useTenantsAdmin } from "@/pages/tenants-admin/hooks/use-tenants-admin";
import { formatFileSize } from "@/lib/format";
import { useTenantRestore } from "./hooks/use-tenant-restore";

export function TenantRestoreSection() {
  const { t } = useTranslation("backup");
  const { tenants } = useTenantsAdmin();
  const restore = useTenantRestore();
  const fileRef = useRef<HTMLInputElement>(null);

  const [tenantId, setTenantId] = useState("");
  const [mode, setMode] = useState("upsert");
  const [dryRun, setDryRun] = useState(false);
  const [file, setFile] = useState<File | null>(null);
  const [dragging, setDragging] = useState(false);
  const [confirmOpen, setConfirmOpen] = useState(false);

  const tenantOptions = tenants.map((t) => ({ value: t.id, label: t.name || t.slug }));

  const handleFile = useCallback((f: File) => setFile(f), []);
  const handleDrop = useCallback((e: React.DragEvent) => {
    e.preventDefault(); setDragging(false);
    const f = e.dataTransfer.files[0];
    if (f) handleFile(f);
  }, [handleFile]);

  const handleConfirm = () => {
    if (!file || !tenantId) return;
    setConfirmOpen(false);
    restore.startRestore(file, tenantId, { mode, dryRun });
  };

  const handleReset = () => {
    setFile(null);
    setDryRun(false);
    restore.reset();
  };

  if (restore.status === "running") {
    return (
      <div className="space-y-4">
        <h3 className="text-sm font-medium">{t("tenant.restore.running")}</h3>
        <OperationProgress steps={restore.steps} elapsed={restore.elapsed} />
        <div className="flex justify-end">
          <Button variant="outline" onClick={restore.cancel}>{t("cancel", { ns: "common" })}</Button>
        </div>
      </div>
    );
  }

  if (restore.status === "complete" && restore.result) {
    const r = restore.result as Record<string, unknown>;
    const tablesRestored = (r.tables_restored ?? {}) as Record<string, number>;
    return (
      <div className="space-y-4">
        <h3 className="text-sm font-medium text-green-600">{t("tenant.restore.complete")}</h3>
        <OperationProgress steps={restore.steps} elapsed={restore.elapsed} />

        {!!r.dry_run && (
          <div className="rounded-md border border-blue-200 bg-blue-50 dark:border-blue-900/40 dark:bg-blue-950/20 px-3 py-2 text-sm text-blue-700 dark:text-blue-300">
            {t("tenant.restore.dryRunNote")}
          </div>
        )}

        {Object.keys(tablesRestored).length > 0 && (
          <div className="rounded-md border bg-muted/50 p-3 text-sm space-y-1">
            <p className="text-xs font-medium text-muted-foreground mb-1">{t("tenant.restore.tablesRestored")}</p>
            {Object.entries(tablesRestored).map(([table, count]) => (
              <div key={table} className="flex justify-between text-xs">
                <span className="font-mono">{table}</span>
                <span>{count}</span>
              </div>
            ))}
          </div>
        )}

        {typeof r.files_extracted === "number" && r.files_extracted > 0 && (
          <div className="text-sm text-muted-foreground">
            {t("tenant.restore.filesExtracted")}: {String(r.files_extracted)}
          </div>
        )}

        <div className="flex justify-end">
          <Button variant="outline" onClick={handleReset}>
            <RotateCcw className="mr-1.5 h-4 w-4" />
            {t("tenant.restore.newRestore")}
          </Button>
        </div>
      </div>
    );
  }

  if (restore.status === "error") {
    return (
      <div className="space-y-4">
        <h3 className="text-sm font-medium text-destructive">{t("restore.errorTitle")}</h3>
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

  return (
    <div className="space-y-4">
      <div>
        <Label className="mb-1.5">{t("tenant.selectTenant")}</Label>
        <Combobox
          value={tenantId}
          onChange={setTenantId}
          options={tenantOptions}
          placeholder={t("tenant.selectTenantPlaceholder")}
        />
      </div>

      <div>
        <Label>{t("tenant.restore.mode")}</Label>
        <Select value={mode} onValueChange={setMode}>
          <SelectTrigger className="mt-1 text-base md:text-sm"><SelectValue /></SelectTrigger>
          <SelectContent>
            <SelectItem value="upsert">{t("tenant.restore.modeUpsert")}</SelectItem>
            <SelectItem value="replace">{t("tenant.restore.modeReplace")}</SelectItem>
            <SelectItem value="new">{t("tenant.restore.modeNew")}</SelectItem>
          </SelectContent>
        </Select>
      </div>

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
          <p className="text-sm text-muted-foreground">{t("tenant.restore.dropzone")}</p>
          <input ref={fileRef} type="file" className="hidden" accept=".tar.gz,.gz"
            onChange={(e) => { const f = e.target.files?.[0]; if (f) handleFile(f); }} />
        </div>
      )}

      {file && (
        <div className="rounded-md border bg-muted/50 p-3 text-sm">
          <div className="flex items-center gap-2">
            <FileArchive className="h-4 w-4 text-muted-foreground" />
            <span className="font-medium">{file.name}</span>
            <span className="text-xs text-muted-foreground ml-auto">{formatFileSize(file.size)}</span>
          </div>
        </div>
      )}

      <label className="flex items-center gap-2 text-sm cursor-pointer">
        <input type="checkbox" checked={dryRun} onChange={(e) => setDryRun(e.target.checked)} className="accent-primary" />
        {t("tenant.restore.dryRun")}
      </label>

      <div className="flex items-center justify-end gap-2 pt-2">
        {file && (
          <Button variant="outline" onClick={() => setFile(null)}>
            {t("cancel", { ns: "common" })}
          </Button>
        )}
        <Button
          variant="destructive"
          onClick={() => setConfirmOpen(true)}
          disabled={!tenantId || !file}
        >
          {t("tenant.restore.start")}
        </Button>
      </div>

      <ConfirmDialog
        open={confirmOpen}
        onOpenChange={setConfirmOpen}
        title={t("tenant.restore.confirmTitle")}
        description={t("tenant.restore.confirmDesc")}
        variant="destructive"
        onConfirm={handleConfirm}
      />
    </div>
  );
}
