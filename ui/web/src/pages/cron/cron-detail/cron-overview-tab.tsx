import { useState } from "react";
import { useTranslation } from "react-i18next";
import { Calendar, Clock, AlertTriangle, Globe, Pencil } from "lucide-react";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Textarea } from "@/components/ui/textarea";
import { Switch } from "@/components/ui/switch";
import { Button } from "@/components/ui/button";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { StickySaveBar } from "@/components/shared/sticky-save-bar";
import { MarkdownRenderer } from "@/components/shared/markdown-renderer";
import { formatDate } from "@/lib/format";
import type { CronJob } from "../hooks/use-cron";
import { CronStatusBadge } from "../cron-utils";
import { useAgents } from "@/pages/agents/hooks/use-agents";

interface CronOverviewTabProps {
  job: CronJob;
  onUpdate?: (id: string, params: Record<string, unknown>) => Promise<void>;
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
  const readonly = !onUpdate;

  const [scheduleKind, setScheduleKind] = useState<ScheduleKind>(job.schedule.kind as ScheduleKind);
  const [everySeconds, setEverySeconds] = useState(getEverySeconds(job));
  const [cronExpr, setCronExpr] = useState(job.schedule.expr ?? "0 * * * *");
  const [message, setMessage] = useState(job.payload?.message ?? "");
  const [agentId, setAgentId] = useState(job.agentId ?? "");
  const [enabled, setEnabled] = useState(job.enabled);
  const [editingMessage, setEditingMessage] = useState(false);
  const [saving, setSaving] = useState(false);

  const handleSave = async () => {
    if (!onUpdate) return;
    setSaving(true);
    try {
      let schedule;
      if (scheduleKind === "every") {
        schedule = { kind: "every" as const, everyMs: Number(everySeconds) * 1000 };
      } else if (scheduleKind === "cron") {
        schedule = { kind: "cron" as const, expr: cronExpr };
      } else {
        schedule = { kind: "at" as const, atMs: job.schedule.atMs ?? Date.now() + 60000 };
      }
      await onUpdate(job.id, {
        schedule,
        message: message.trim(),
        agentId: agentId.trim() || undefined,
        enabled,
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
      <section className="space-y-3 rounded-lg border p-3 sm:p-4 overflow-hidden">
        <h3 className="text-sm font-medium">{t("detail.schedule")}</h3>

        <div className="space-y-2">
          <Label>{t("create.name")}</Label>
          <Input
            value={job.name}
            disabled
            className="text-base md:text-sm"
          />
        </div>

        <div className="space-y-2">
          <Label>{t("create.scheduleType")}</Label>
          <div className="flex gap-2">
            {(["every", "cron", "at"] as const).map((kind) => (
              <Button
                key={kind}
                variant={scheduleKind === kind ? "default" : "outline"}
                size="sm"
                onClick={() => !readonly && setScheduleKind(kind)}
                disabled={readonly}
                type="button"
              >
                {kind === "every" ? t("create.every") : kind === "cron" ? t("create.cron") : t("create.once")}
              </Button>
            ))}
          </div>
        </div>

        {scheduleKind === "every" && (
          <div className="space-y-2">
            <Label>{t("create.intervalSeconds")}</Label>
            <Input
              type="number"
              min={1}
              value={everySeconds}
              onChange={(e) => setEverySeconds(e.target.value)}
              disabled={readonly}
              className="text-base md:text-sm"
            />
          </div>
        )}

        {scheduleKind === "cron" && (
          <div className="space-y-2">
            <Label>{t("create.cronExpression")}</Label>
            <Input
              value={cronExpr}
              onChange={(e) => setCronExpr(e.target.value)}
              disabled={readonly}
              placeholder="0 * * * *"
              className="text-base md:text-sm"
            />
            <p className="text-xs text-muted-foreground">{t("create.cronHint")}</p>
          </div>
        )}

        {scheduleKind === "at" && (
          <p className="text-sm text-muted-foreground">{t("create.onceDesc")}</p>
        )}
      </section>

      {/* Message section — markdown preview or textarea */}
      <section className="space-y-3 rounded-lg border p-3 sm:p-4 overflow-hidden">
        <div className="flex items-center justify-between">
          <h3 className="text-sm font-medium">{t("detail.messageSection")}</h3>
          {!readonly && (
            <Button
              variant="ghost"
              size="sm"
              className="h-7 gap-1 text-xs text-muted-foreground"
              onClick={() => setEditingMessage(!editingMessage)}
            >
              <Pencil className="h-3 w-3" />
              {editingMessage ? t("detail.preview") : t("detail.edit")}
            </Button>
          )}
        </div>

        {editingMessage ? (
          <Textarea
            value={message}
            onChange={(e) => setMessage(e.target.value)}
            rows={6}
            placeholder={t("create.messagePlaceholder")}
            className="text-base md:text-sm resize-none"
          />
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

      {/* Agent & Status section */}
      <section className="space-y-3 rounded-lg border p-3 sm:p-4 overflow-hidden">
        <h3 className="text-sm font-medium">{t("detail.agentStatus")}</h3>
        <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
          <div className="space-y-2">
            <Label>{t("create.agentId")}</Label>
            <Select value={agentId || "__default__"} onValueChange={(v) => setAgentId(v === "__default__" ? "" : v)} disabled={readonly}>
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
              <span className="text-sm">{job.enabled ? t("detail.enabled") : t("detail.disabled")}</span>
              <Switch
                checked={enabled}
                onCheckedChange={setEnabled}
                disabled={readonly}
              />
            </div>
          </div>
        </div>

        {/* Info grid */}
        <div className="grid grid-cols-2 gap-3 sm:grid-cols-3">
          {job.state?.nextRunAtMs && (
            <div className="rounded-md bg-muted/50 p-3">
              <div className="flex items-center gap-1.5 text-xs text-muted-foreground">
                <Calendar className="h-3 w-3" />
                {t("detail.infoRows.nextRun")}
              </div>
              <div className="mt-1 text-sm font-medium">{formatDate(new Date(job.state.nextRunAtMs))}</div>
            </div>
          )}
          {job.state?.lastRunAtMs && (
            <div className="rounded-md bg-muted/50 p-3">
              <div className="flex items-center gap-1.5 text-xs text-muted-foreground">
                <Clock className="h-3 w-3" />
                {t("detail.infoRows.lastRun")}
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
          <div className="rounded-md bg-muted/50 p-3">
            <div className="text-xs text-muted-foreground">{t("detail.infoRows.updated")}</div>
            <div className="mt-1 text-sm font-medium">{formatDate(new Date(job.updatedAtMs))}</div>
          </div>
          {job.schedule.tz && (
            <div className="rounded-md bg-muted/50 p-3">
              <div className="flex items-center gap-1.5 text-xs text-muted-foreground">
                <Globe className="h-3 w-3" />
                {t("detail.infoRows.timezone")}
              </div>
              <div className="mt-1 text-sm font-medium">{job.schedule.tz}</div>
            </div>
          )}
        </div>
      </section>

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

      {!readonly && (
        <StickySaveBar
          onSave={handleSave}
          saving={saving}
        />
      )}
    </div>
  );
}
