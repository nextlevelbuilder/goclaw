import { useState } from "react";
import { useTranslation } from "react-i18next";
import { Download, RotateCcw } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Label } from "@/components/ui/label";
import { Combobox } from "@/components/ui/combobox";
import { OperationProgress } from "@/components/shared/operation-progress";
import { useTenantsAdmin } from "@/pages/tenants-admin/hooks/use-tenants-admin";
import { useTenantBackup } from "./hooks/use-tenant-backup";

export function TenantBackupSection() {
  const { t } = useTranslation("backup");
  const { tenants } = useTenantsAdmin();
  const backup = useTenantBackup();
  const [tenantId, setTenantId] = useState("");

  const tenantOptions = tenants.map((t) => ({ value: t.id, label: t.name || t.slug }));

  if (backup.status === "running") {
    return (
      <div className="space-y-4">
        <h3 className="text-sm font-medium">{t("tenant.backup.running")}</h3>
        <OperationProgress steps={backup.steps} elapsed={backup.elapsed} />
        <div className="flex justify-end">
          <Button variant="outline" onClick={backup.cancel}>{t("cancel", { ns: "common" })}</Button>
        </div>
      </div>
    );
  }

  if (backup.status === "complete") {
    const r = backup.result ?? {};
    const tableCounts = (r.table_counts ?? {}) as Record<string, number>;
    return (
      <div className="space-y-4">
        <h3 className="text-sm font-medium text-green-600">{t("tenant.backup.complete")}</h3>
        <OperationProgress steps={backup.steps} elapsed={backup.elapsed} />

        {Object.keys(tableCounts).length > 0 && (
          <div className="rounded-md border bg-muted/50 p-3 text-sm space-y-1">
            <p className="text-xs font-medium text-muted-foreground mb-1">{t("tenant.backup.tableCounts")}</p>
            {Object.entries(tableCounts).map(([table, count]) => (
              <div key={table} className="flex justify-between text-xs">
                <span className="font-mono">{table}</span>
                <span>{count}</span>
              </div>
            ))}
          </div>
        )}

        <div className="flex items-center gap-2 justify-end">
          {backup.downloadReady && (
            <Button onClick={backup.download}>
              <Download className="mr-1.5 h-4 w-4" />
              {t("tenant.backup.download")}
            </Button>
          )}
          <Button variant="ghost" onClick={() => { backup.reset(); setTenantId(""); }}>
            <RotateCcw className="mr-1.5 h-4 w-4" />
            {t("tenant.backup.newBackup")}
          </Button>
        </div>
      </div>
    );
  }

  if (backup.status === "error") {
    return (
      <div className="space-y-4">
        <h3 className="text-sm font-medium text-destructive">{t("backup.errorTitle", { ns: "backup" })}</h3>
        {backup.error && (
          <div className="rounded-md border border-destructive/30 bg-destructive/5 p-3 text-sm">
            <p className="text-destructive">{backup.error.detail}</p>
          </div>
        )}
        <div className="flex justify-end">
          <Button variant="outline" onClick={() => { backup.reset(); setTenantId(""); }}>
            <RotateCcw className="mr-1.5 h-4 w-4" />
            {t("tenant.backup.newBackup")}
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
      <div className="flex justify-end">
        <Button onClick={() => backup.startBackup(tenantId)} disabled={!tenantId}>
          {t("tenant.backup.start")}
        </Button>
      </div>
    </div>
  );
}
