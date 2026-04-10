import { useTranslation } from "react-i18next";
import { Tabs, TabsList, TabsTrigger, TabsContent } from "@/components/ui/tabs";
import { TenantBackupSection } from "./tenant-backup-section";
import { TenantRestoreSection } from "./tenant-restore-section";

export function TenantBackupPanel() {
  const { t } = useTranslation("backup");

  return (
    <div className="space-y-4">
      <div>
        <h3 className="text-sm font-medium">{t("tenant.title")}</h3>
        <p className="mt-1 text-sm text-muted-foreground">{t("tenant.description")}</p>
      </div>

      <Tabs defaultValue="backup">
        <TabsList>
          <TabsTrigger value="backup">{t("tabs.systemBackup")}</TabsTrigger>
          <TabsTrigger value="restore">{t("tabs.systemRestore")}</TabsTrigger>
        </TabsList>
        <TabsContent value="backup" className="mt-4">
          <TenantBackupSection />
        </TabsContent>
        <TabsContent value="restore" className="mt-4">
          <TenantRestoreSection />
        </TabsContent>
      </Tabs>
    </div>
  );
}
