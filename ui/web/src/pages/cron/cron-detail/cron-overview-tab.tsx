import { useState, useEffect, useCallback } from "react";
import { useTranslation } from "react-i18next";
import { Calendar, Clock, AlertTriangle, Pencil, Terminal } from "lucide-react";
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
import { CronStatusBadge, formatCommand, isCommandCron } from "../cron-utils";
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
  const [editingCommand, setEditingCommand] = useState(false);

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
  const command = job.payload?.command;
  const [commandJson, setCommandJson] = useState(() => JSON.stringify(command ?? { argv: [] }, null, 2));
  const commandEnvCount = command?.env ? Object.keys(command.env).length : 0;

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

    const commandChanged = isCommandCron(job)
      && commandJson.trim() !== JSON.stringify(command ?? { argv: [] }, null, 2).trim();
    let parsedCommand: import("../hooks/use-cron").CronCommandSpec | undefined;
    if (commandChanged) {
      try {
        parsedCommand = JSON.parse(commandJson);
        if (!Array.isArray(parsedCommand?.argv) || parsedCommand.argv.length === 0 || !parsedCommand.argv[0]) {
          toast.error(t("detail.invalidCommandJson"));
          return;
        }
      } catch {
        toast.error(t("detail.invalidCommandJson"));
        return;
      }
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
      const patch: import("../hooks/use-cron").CronJobPatch = {
        schedule,
        message: isCommandCron(job) ? undefined : message.trim(),
        command: commandChanged ? parsedCommand : undefined,
        agentId: agentId.trim() || "",
        deliver,
        deliverChannel: deliver ? channel.trim() || undefined : undefined,
        deliverTo: deliver ? to.trim() || undefined : undefined,
        wakeHeartbeat,
        deleteAfterRun,
        stateless,
      };
      // Only include enabled in patch if it actually changed to avoid unintended toggles
      if (enabled !== job.enabled) patch.enabled = enabled;
      await onUpdate(job.id, patch);
      setEditingMessage(false);
      setEditingCommand(false);
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

      {isCommandCron(job) ? (
        <section className="space-y-3 rounded-lg border border-amber-200 bg-amber-50/40 p-3 sm:p-4 overflow-hidden dark:border-amber-900/50 dark:bg-amber-950/20">
          <div className="flex items-center justify-between gap-2">
            <div className="flex items-center gap-2">
              <Terminal className="h-4 w-4 text-amber-600 dark:text-amber-400" />
              <h3 className="text-sm font-medium">{t("detail.commandSection")}</h3>
            </div>
            <div className="flex items-center gap-2">
              <span className="rounded-full border border-amber-300 px-2 py-0.5 text-[10px] font-medium uppercase tracking-wide text-amber-700 dark:border-amber-800 dark:text-amber-300">
                {t("payload.command")}
              </span>
              {!readonly && (
                <Button variant="ghost" size="sm" className="h-7 gap-1 text-xs text-muted-foreground"
                  onClick={() => setEditingCommand(!editingCommand)}>
                  <Pencil className="h-3 w-3" />
                  {editingCommand ? t("detail.preview") : t("detail.edit")}
                </Button>
              )}
            </div>
          </div>
          <div className="space-y-3 rounded-md border bg-background/80 p-3 sm:p-4">
            {editingCommand ? (
              <div>
                <div className="mb-1 text-xs font-medium text-muted-foreground">{t("detail.commandJson")}</div>
                <Textarea value={commandJson} onChange={(e) => setCommandJson(e.target.value)}
                  rows={10} className="font-mono text-base md:text-sm" />
                <p className="mt-1 text-xs text-muted-foreground">{t("detail.commandJsonHelp")}</p>
              </div>
            ) : (
              <div>
                <div className="mb-1 text-xs font-medium text-muted-foreground">{t("detail.commandArgv")}</div>
                {formatCommand(job) ? (
                  <pre className="overflow-x-auto rounded-md bg-muted px-3 py-2 text-xs"><code>{formatCommand(job)}</code></pre>
                ) : (
                  <p className="text-sm italic text-muted-foreground">{t("detail.noCommand")}</p>
                )}
              </div>
            )}
            <div className="grid grid-cols-1 gap-3 sm:grid-cols-2 lg:grid-cols-4">
              <div className="min-w-0">
                <div className="text-xs font-medium text-muted-foreground">{t("detail.commandCwd")}</div>
                <div className="truncate text-sm">{command?.cwd || t("detail.defaultWorkingDirectory")}</div>
              </div>
              <div>
                <div className="text-xs font-medium text-muted-foreground">{t("detail.commandTimeout")}</div>
                <div className="text-sm">{command?.timeoutSeconds ? t("detail.seconds", { count: command.timeoutSeconds }) : t("detail.defaultValue")}</div>
              </div>
              <div>
                <div className="text-xs font-medium text-muted-foreground">{t("detail.commandOutputLimit")}</div>
                <div className="text-sm">{command?.outputMaxBytes ? t("detail.bytes", { count: command.outputMaxBytes }) : t("detail.defaultValue")}</div>
              </div>
              <div>
                <div className="text-xs font-medium text-muted-foreground">{t("detail.commandEnv")}</div>
                <div className="text-sm">{commandEnvCount ? t("detail.envVars", { count: commandEnvCount }) : t("detail.none")}</div>
              </div>
            </div>
            {(command?.input || command?.noOutputTimeoutSeconds) && (
              <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
                {command.input && (
                  <div>
                    <div className="text-xs font-medium text-muted-foreground">{t("detail.commandInput")}</div>
                    <pre className="max-h-32 overflow-auto rounded-md bg-muted px-3 py-2 text-xs"><code>{command.input}</code></pre>
                  </div>
                )}
                {command.noOutputTimeoutSeconds ? (
                  <div>
                    <div className="text-xs font-medium text-muted-foreground">{t("detail.commandNoOutputTimeout")}</div>
                    <div className="text-sm">{t("detail.seconds", { count: command.noOutputTimeoutSeconds })}</div>
                  </div>
                ) : null}
              </div>
            )}
          </div>
        </section>
      ) : (
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
      )}

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
