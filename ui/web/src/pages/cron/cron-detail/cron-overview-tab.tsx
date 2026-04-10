import { useState, useEffect, useCallback } from "react";
import { useTranslation } from "react-i18next";
import { Calendar, Clock, AlertTriangle, Pencil } from "lucide-react";
import { Textarea } from "@/components/ui/textarea";
import { Switch } from "@/components/ui/switch";
import { Button } from "@/components/ui/button";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Label } from "@/components/ui/label";
import { StickySaveBar } from "@/components/shared/sticky-save-bar";
import { MarkdownRenderer } from "@/components/shared/markdown-renderer";
import { isValidIanaTimezone } from "@/lib/constants";
import { formatDate } from "@/lib/format";
import { toast } from "@/stores/use-toast-store";
import { useChannels } from "@/pages/channels/hooks/use-channels";
import { useWs } from "@/hooks/use-ws";
import { Methods } from "@/api/protocol";
import type { CronJob, CronJobPatch } from "../hooks/use-cron";
import { CronStatusBadge } from "../cron-utils";
import { useAgents } from "@/pages/agents/hooks/use-agents";
import { CronScheduleSection } from "./cron-schedule-section";
import { CronDeliverySection } from "./cron-delivery-section";
import type { DeliveryTarget } from "./cron-delivery-section";
import { CronLifecycleSection } from "./cron-lifecycle-section";

interface CronOverviewTabProps {
  job: CronJob;
  onUpdate?: (id: string, params: CronJobPatch) => Promise<void>;
}

type ScheduleKind = "every" | "cron" | "at";

function getEverySeconds(job: CronJob): string {
  if (job.schedule.kind === "every" && job.schedule.everyMs) {
    return String(job.schedule.everyMs / 1000);
  }
  return "60";
}

export function CronOverviewTab({ job, onUpdate }: CronOverviewTabProps) {
  const { t } = useTranslation("cron");
  const { agents } = useAgents();
  const ws = useWs();
  const { channels: availableChannels } = useChannels();
  const channelNames = Object.keys(availableChannels);
  const readonly = !onUpdate;

  // Schedule fields
  const [scheduleKind, setScheduleKind] = useState<ScheduleKind>(job.schedule.kind as ScheduleKind);
  const [everySeconds, setEverySeconds] = useState(getEverySeconds(job));
  const [cronExpr, setCronExpr] = useState(job.schedule.expr ?? "0 * * * *");
  const [timezone, setTimezone] = useState(job.schedule.tz ?? "UTC");
  const [message, setMessage] = useState(job.payload?.message ?? "");
  const [agentId, setAgentId] = useState(job.agentId ?? "");
  const [enabled, setEnabled] = useState(job.enabled);
  const [editingMessage, setEditingMessage] = useState(false);

  // Delivery fields
  const [deliver, setDeliver] = useState(job.deliver ?? false);
  const [channel, setChannel] = useState(job.deliverChannel ?? "");
  const [to, setTo] = useState(job.deliverTo ?? "");
  const [wakeHeartbeat, setWakeHeartbeat] = useState(job.wakeHeartbeat ?? false);
  const [targets, setTargets] = useState<DeliveryTarget[]>([]);

  // Lifecycle fields
  const [deleteAfterRun, setDeleteAfterRun] = useState(job.deleteAfterRun ?? false);
  const [stateless, setStateless] = useState(job.stateless ?? false);

  const [saving, setSaving] = useState(false);

  // Fetch delivery targets on mount
  const fetchTargets = useCallback(async () => {
    if (!ws.isConnected) return;
    try {
      const res = await ws.call<{ targets: DeliveryTarget[] }>(
        Methods.HEARTBEAT_TARGETS, { agentId: job.agentId || "" },
      );
      setTargets(res.targets ?? []);
    } catch { /* fallback to Input */ }
  }, [ws, job.agentId]);

  useEffect(() => { fetchTargets(); }, [fetchTargets]);

  const handleSave = async () => {
    if (!onUpdate) return;
    if (timezone && timezone !== "UTC" && !isValidIanaTimezone(timezone)) {
      toast.error(t("detail.invalidTimezone", "Invalid timezone"));
      return;
    }
    setSaving(true);
    try {
      let schedule;
      if (scheduleKind === "every") {
        schedule = { kind: "every" as const, everyMs: Number(everySeconds) * 1000, tz: timezone !== "UTC" ? timezone : "" };
      } else if (scheduleKind === "cron") {
        schedule = { kind: "cron" as const, expr: cronExpr, tz: timezone !== "UTC" ? timezone : "" };
      } else {
        schedule = { kind: "at" as const, atMs: job.schedule.atMs ?? Date.now() + 60000, tz: timezone !== "UTC" ? timezone : "" };
      }
      await onUpdate(job.id, {
        schedule,
        message: message.trim(),
        agentId: agentId.trim() || "",
        enabled,
        deliver,
        deliverChannel: deliver ? channel.trim() || undefined : undefined,
        deliverTo: deliver ? to.trim() || undefined : undefined,
        wakeHeartbeat,
        deleteAfterRun,
        stateless,
      });
      setEditingMessage(false);
    } catch {
      // toast shown by hook
    } finally {
      setSaving(false);
    }
  };

  return (
    <div className="space-y-4">
      {/* Schedule section */}
      <CronScheduleSection
        job={job}
        scheduleKind={scheduleKind}
        setScheduleKind={setScheduleKind}
        everySeconds={everySeconds}
        setEverySeconds={setEverySeconds}
        cronExpr={cronExpr}
        setCronExpr={setCronExpr}
        timezone={timezone}
        setTimezone={setTimezone}
        readonly={readonly}
      />

      {/* Message section */}
      <section className="space-y-3 rounded-lg border p-3 sm:p-4 overflow-hidden">
        <div className="flex items-center justify-between">
          <h3 className="text-sm font-medium">{t("detail.messageSection")}</h3>
          {!readonly && (
            <Button variant="ghost" size="sm" className="h-7 gap-1 text-xs text-muted-foreground"
              onClick={() => setEditingMessage(!editingMessage)}>
              <Pencil className="h-3 w-3" />
              {editingMessage ? t("detail.preview") : t("detail.edit")}
            </Button>
          )}
        </div>
        {editingMessage ? (
          <Textarea value={message} onChange={(e) => setMessage(e.target.value)}
            rows={6} placeholder={t("create.messagePlaceholder")} className="text-base md:text-sm resize-none" />
        ) : (
          <div className="rounded-md border bg-muted/30 p-3 sm:p-4">
            {message ? (
              <MarkdownRenderer content={message} className="prose-sm max-w-none" />
            ) : (
              <p className="text-sm italic text-muted-foreground">{t("detail.noMessage")}</p>
            )}
          </div>
        )}
      </section>

      {/* Delivery section */}
      <CronDeliverySection
        deliver={deliver}
        setDeliver={setDeliver}
        channel={channel}
        setChannel={setChannel}
        to={to}
        setTo={setTo}
        wakeHeartbeat={wakeHeartbeat}
        setWakeHeartbeat={setWakeHeartbeat}
        channelNames={channelNames}
        targets={targets}
        readonly={readonly}
      />

      {/* Agent & Status section */}
      <section className="space-y-3 rounded-lg border p-3 sm:p-4 overflow-hidden">
        <h3 className="text-sm font-medium">{t("detail.agentStatus")}</h3>
        <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
          <div className="space-y-2">
            <Label>{t("create.agentId")}</Label>
            <Select name="agentId" value={agentId || "__default__"}
              onValueChange={(v) => setAgentId(v === "__default__" ? "" : v)} disabled={readonly}>
              <SelectTrigger className="text-base md:text-sm">
                <SelectValue placeholder={t("create.agentIdPlaceholder")} />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="__default__">{t("create.agentIdPlaceholder")}</SelectItem>
                {agents.map((a) => (
                  <SelectItem key={a.id} value={a.id}>
                    {a.display_name || a.agent_key || a.id}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </div>
          <div className="space-y-2">
            <Label>{t("columns.enabled")}</Label>
            <div className="flex items-center justify-between gap-4 rounded-md border px-3 py-2.5">
              <span className="text-sm">{enabled ? t("detail.enabled") : t("detail.disabled")}</span>
              <Switch checked={enabled} onCheckedChange={setEnabled} disabled={readonly} />
            </div>
          </div>
        </div>

        {/* Info grid */}
        <div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-3 gap-3">
          {job.state?.nextRunAtMs && (
            <div className="rounded-md bg-muted/50 p-3">
              <div className="flex items-center gap-1.5 text-xs text-muted-foreground">
                <Calendar className="h-3 w-3" />{t("detail.infoRows.nextRun")}
              </div>
              <div className="mt-1 text-sm font-medium">{formatDate(new Date(job.state.nextRunAtMs))}</div>
            </div>
          )}
          {job.state?.lastRunAtMs && (
            <div className="rounded-md bg-muted/50 p-3">
              <div className="flex items-center gap-1.5 text-xs text-muted-foreground">
                <Clock className="h-3 w-3" />{t("detail.infoRows.lastRun")}
              </div>
              <div className="mt-1 text-sm font-medium">{formatDate(new Date(job.state.lastRunAtMs))}</div>
            </div>
          )}
          {job.state?.lastStatus && (
            <div className="rounded-md bg-muted/50 p-3">
              <div className="text-xs text-muted-foreground">{t("detail.infoRows.lastStatus")}</div>
              <div className="mt-1"><CronStatusBadge status={job.state.lastStatus} /></div>
            </div>
          )}
          <div className="rounded-md bg-muted/50 p-3">
            <div className="text-xs text-muted-foreground">{t("detail.infoRows.created")}</div>
            <div className="mt-1 text-sm font-medium">{formatDate(new Date(job.createdAtMs))}</div>
          </div>
        </div>
      </section>

      {/* Lifecycle section */}
      <CronLifecycleSection
        deleteAfterRun={deleteAfterRun}
        setDeleteAfterRun={setDeleteAfterRun}
        stateless={stateless}
        setStateless={setStateless}
        readonly={readonly}
      />

      {/* Last error */}
      {job.state?.lastError && (
        <section className="rounded-lg border border-destructive/30 bg-destructive/5 p-3 sm:p-4 overflow-hidden">
          <div className="mb-2 flex items-center gap-1.5">
            <AlertTriangle className="h-4 w-4 text-destructive" />
            <h3 className="text-sm font-medium text-destructive">{t("detail.lastError")}</h3>
          </div>
          <div className="rounded-md bg-background/50 p-3">
            <MarkdownRenderer content={job.state.lastError} className="prose-sm max-w-none text-destructive/80" />
          </div>
        </section>
      )}

      {!readonly && <StickySaveBar onSave={handleSave} saving={saving} />}
    </div>
  );
}
