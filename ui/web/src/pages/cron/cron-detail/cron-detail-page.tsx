import { useState, useEffect, useCallback } from "react";
import { useTranslation } from "react-i18next";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { ConfirmDialog } from "@/components/shared/confirm-dialog";
import type { CronJob, CronJobPatch, CronRunLogEntry } from "../hooks/use-cron";
import { CronHeader } from "./cron-header";
import { CronOverviewTab } from "./cron-overview-tab";
import { CronRunHistoryTab } from "./cron-run-history-tab";

interface CronDetailPageProps {
  job: CronJob;
  onBack: () => void;
  onRun: (id: string) => Promise<void>;
  onToggle: (id: string, enabled: boolean) => Promise<void>;
  onDelete: (id: string) => Promise<void>;
  onUpdate?: (id: string, params: CronJobPatch) => Promise<void>;
  getRunLog: (id: string, limit?: number, offset?: number) => Promise<{ entries: CronRunLogEntry[]; total: number }>;
  onRefresh: () => void;
}

export function CronDetailPage({
  job,
  onBack,
  onRun,
  onToggle,
  onDelete,
  onUpdate,
  getRunLog,
  onRefresh,
}: CronDetailPageProps) {
  const { t } = useTranslation("cron");
  const [activeTab, setActiveTab] = useState("overview");
  const [confirmDelete, setConfirmDelete] = useState(false);
  const [confirmToggle, setConfirmToggle] = useState(false);
  const [running, setRunning] = useState(false);

  const isRunning = running || job.state?.lastStatus === "running";

  // Poll while running to detect completion
  useEffect(() => {
    if (!isRunning) return;
    const interval = setInterval(onRefresh, 3000);
    return () => clearInterval(interval);
  }, [isRunning, onRefresh]);

  // Clear local running state when backend reports completion
  useEffect(() => {
    if (running && job.state?.lastStatus && job.state.lastStatus !== "running") {
      setRunning(false);
    }
  }, [running, job.state?.lastStatus]);

  const handleRun = useCallback(async () => {
    setRunning(true);
    await onRun(job.id);
  }, [job.id, onRun]);

  return (
    <div>
      <CronHeader
        job={job}
        isRunning={isRunning}
        onBack={onBack}
        onRun={handleRun}
        onToggle={() => setConfirmToggle(true)}
        onDelete={() => setConfirmDelete(true)}
      />

      <div className="p-3 pb-10 sm:p-4 sm:pb-10">
        <div className="mx-auto max-w-4xl">
          <Tabs value={activeTab} onValueChange={setActiveTab}>
            <TabsList className="w-full justify-start overflow-x-auto overflow-y-hidden">
              <TabsTrigger value="overview">{t("detail.tabs.overview")}</TabsTrigger>
              <TabsTrigger value="history">{t("detail.tabs.history")}</TabsTrigger>
            </TabsList>

            <TabsContent value="overview" className="mt-4">
              <CronOverviewTab key={job.id + "-" + job.updatedAtMs} job={job} onUpdate={onUpdate} />
            </TabsContent>

            <TabsContent value="history" className="mt-4">
              <CronRunHistoryTab job={job} getRunLog={getRunLog} onRefresh={onRefresh} />
            </TabsContent>
          </Tabs>
        </div>
      </div>

      <ConfirmDialog
        open={confirmDelete}
        onOpenChange={setConfirmDelete}
        title={t("delete.title")}
        description={t("delete.description", { name: job.name })}
        confirmLabel={t("delete.confirmLabel")}
        variant="destructive"
        onConfirm={async () => {
          await onDelete(job.id);
          setConfirmDelete(false);
        }}
      />

      <ConfirmDialog
        open={confirmToggle}
        onOpenChange={setConfirmToggle}
        title={job.enabled ? t("disable.title") : t("enable.title")}
        description={
          job.enabled
            ? t("disable.description", { name: job.name })
            : t("enable.description", { name: job.name })
        }
        confirmLabel={job.enabled ? t("disable.confirmLabel") : t("enable.confirmLabel")}
        variant={job.enabled ? "destructive" : "default"}
        onConfirm={async () => {
          await onToggle(job.id, !job.enabled);
          setConfirmToggle(false);
        }}
      />
    </div>
  );
}
