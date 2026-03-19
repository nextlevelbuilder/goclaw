import { useState } from "react";
import { Heart, Settings, List, Activity } from "lucide-react";
import { useTranslation } from "react-i18next";
import { Button } from "@/components/ui/button";
import { Switch } from "@/components/ui/switch";
import type { UseAgentHeartbeatReturn } from "@/pages/agents/hooks/use-agent-heartbeat";
import { HeartbeatConfigDialog } from "../heartbeat-config-dialog";
import { HeartbeatLogsDialog } from "../heartbeat-logs-dialog";
import { formatRelativeTime } from "@/lib/format";

interface HeartbeatCardProps {
  heartbeat: UseAgentHeartbeatReturn;
}

export function HeartbeatCard({ heartbeat }: HeartbeatCardProps) {
  const { t } = useTranslation("agents");
  const { config, loading, saving, toggle, update, test, getChecklist, setChecklist, fetchTargets, refresh, fetchLogs } = heartbeat;
  const [configOpen, setConfigOpen] = useState(false);
  const [logsOpen, setLogsOpen] = useState(false);

  if (loading) {
    return (
      <div className="rounded-lg border p-4">
        <div className="flex items-center gap-2 text-sm text-muted-foreground">
          <div className="h-4 w-4 animate-spin rounded-full border-2 border-muted-foreground border-t-transparent" />
          {t("heartbeat.loading")}
        </div>
      </div>
    );
  }

  if (!config) {
    return (
      <>
        <div className="rounded-lg border p-4">
          <div className="flex items-center justify-between gap-3">
            <div className="flex items-center gap-2">
              <Heart className="h-4 w-4 text-muted-foreground" />
              <span className="text-sm font-medium">{t("heartbeat.title")}</span>
            </div>
            <Button size="sm" variant="outline" onClick={() => setConfigOpen(true)}>
              <Settings className="h-3.5 w-3.5" />
              {t("heartbeat.setupButton")}
            </Button>
          </div>
          <p className="mt-2 text-xs text-muted-foreground">{t("heartbeat.notConfigured")}</p>
        </div>
        {configOpen && (
          <HeartbeatConfigDialog
            open={configOpen} onOpenChange={setConfigOpen}
            config={config} saving={saving} update={update} test={test}
            getChecklist={getChecklist} setChecklist={setChecklist} fetchTargets={fetchTargets} refresh={refresh}
          />
        )}
      </>
    );
  }

  const intervalMin = Math.round(config.intervalSec / 60);
  const activeHours =
    config.activeHoursStart && config.activeHoursEnd
      ? `${config.activeHoursStart}–${config.activeHoursEnd}`
      : null;

  return (
    <>
      <div className="rounded-lg border p-4 space-y-3">
        <div className="flex items-center justify-between gap-3">
          <div className="flex items-center gap-2">
            <Heart className={`h-4 w-4 ${config.enabled ? "fill-rose-500 text-rose-500 animate-pulse" : "text-rose-400"}`} />
            <span className="text-sm font-medium">{t("heartbeat.title")}</span>
          </div>
          <Switch
            checked={config.enabled}
            disabled={saving}
            onCheckedChange={(enabled) => toggle(enabled)}
            aria-label={t("heartbeat.toggleEnabled")}
          />
        </div>

        <div className="space-y-1 text-xs text-muted-foreground">
          <div className="flex flex-wrap gap-x-2 gap-y-0.5">
            <span>
              <Activity className="inline h-3 w-3 mr-0.5" />
              {t("heartbeat.every", { interval: intervalMin })}
            </span>
            {activeHours && <span>{activeHours}</span>}
            {config.channel && (
              <span>{config.channel}{config.chatId ? `/${config.chatId}` : ""}</span>
            )}
            {config.model && (
              <span className="text-violet-600 dark:text-violet-400">{config.model}</span>
            )}
          </div>

          <div className="flex flex-wrap gap-x-3 gap-y-0.5">
            {config.lastRunAt && (
              <span>
                {t("heartbeat.lastRun")}: {formatRelativeTime(config.lastRunAt)}
                {config.lastStatus && ` (${config.lastStatus})`}
              </span>
            )}
            {config.nextRunAt && config.enabled && (
              <span>{t("heartbeat.nextRun")}: {formatRelativeTime(config.nextRunAt)}</span>
            )}
          </div>

          <div className="flex gap-3">
            <span>{t("heartbeat.runs")}: {config.runCount}</span>
            <span>{t("heartbeat.suppressed")}: {config.suppressCount}</span>
          </div>
          {config.lastError && config.lastStatus === "error" && (
            <p className="text-xs text-destructive truncate">{config.lastError}</p>
          )}
        </div>

        <div className="flex gap-2 pt-1">
          <Button size="sm" variant="outline" className="h-7 text-xs" onClick={() => setConfigOpen(true)}>
            <Settings className="h-3 w-3" />
            {t("heartbeat.configure")}
          </Button>
          <Button size="sm" variant="outline" className="h-7 text-xs" onClick={() => setLogsOpen(true)}>
            <List className="h-3 w-3" />
            {t("heartbeat.logs")}
          </Button>
        </div>
      </div>

      {configOpen && (
        <HeartbeatConfigDialog
          open={configOpen} onOpenChange={setConfigOpen}
          config={config} saving={saving} update={update} test={test}
          getChecklist={getChecklist} setChecklist={setChecklist} fetchTargets={fetchTargets} refresh={refresh}
        />
      )}
      {logsOpen && (
        <HeartbeatLogsDialog open={logsOpen} onOpenChange={setLogsOpen} fetchLogs={fetchLogs} />
      )}
    </>
  );
}
