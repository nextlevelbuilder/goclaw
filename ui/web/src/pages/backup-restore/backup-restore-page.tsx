import { useSearchParams } from "react-router";
import { useTranslation } from "react-i18next";
import { Tabs, TabsList, TabsTrigger, TabsContent } from "@/components/ui/tabs";
import { PageHeader } from "@/components/shared/page-header";
import { useTenants } from "@/hooks/use-tenants";
import { SystemBackupPanel } from "./system-backup-panel";
import { SystemRestorePanel } from "./system-restore-panel";
import { S3ConfigPanel } from "./s3-config-panel";
import { TenantBackupPanel } from "./tenant-backup-panel";

export function BackupRestorePage() {
  const { t } = useTranslation("backup");
  const [params, setParams] = useSearchParams();
  const { isOwner } = useTenants();

  const tab = params.get("tab") ?? "system-backup";

  const setTab = (v: string) => {
    const next = new URLSearchParams(params);
    next.set("tab", v);
    setParams(next, { replace: true });
  };

  return (
    <div className="p-4 sm:p-6 pb-10 space-y-6">
      <PageHeader title={t("title")} description={t("description")} />

      <div className="mx-auto max-w-3xl">
        <Tabs value={tab} onValueChange={setTab}>
          <TabsList>
            <TabsTrigger value="system-backup">{t("tabs.systemBackup")}</TabsTrigger>
            <TabsTrigger value="system-restore">{t("tabs.systemRestore")}</TabsTrigger>
            <TabsTrigger value="s3-config">{t("tabs.s3Config")}</TabsTrigger>
            {isOwner && (
              <TabsTrigger value="tenant-backup">{t("tabs.tenantBackup")}</TabsTrigger>
            )}
          </TabsList>

          <TabsContent value="system-backup" className="mt-4">
            <SystemBackupPanel />
          </TabsContent>
          <TabsContent value="system-restore" className="mt-4">
            <SystemRestorePanel />
          </TabsContent>
          <TabsContent value="s3-config" className="mt-4">
            <S3ConfigPanel />
          </TabsContent>
          {isOwner && (
            <TabsContent value="tenant-backup" className="mt-4">
              <TenantBackupPanel />
            </TabsContent>
          )}
        </Tabs>
      </div>
    </div>
  );
}
