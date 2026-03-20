import { useState } from "react";
import { useTranslation } from "react-i18next";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Textarea } from "@/components/ui/textarea";
import { Switch } from "@/components/ui/switch";
import { Button } from "@/components/ui/button";
import { StickySaveBar } from "@/components/shared/sticky-save-bar";
import { formatDate } from "@/lib/format";
import type { CronJob } from "../hooks/use-cron";

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

function InfoRow({ label, value }: { label: string; value: string }) {
  return (
    <div>
      <span className="text-muted-foreground text-xs">{label}</span>
      <div className="mt-0.5 text-sm font-medium">{value}</div>
    </div>
  );
}

export function CronOverviewTab({ job, onUpdate }: CronOverviewTabProps) {
  const { t } = useTranslation("cron");
  const readonly = !onUpdate;

  const [scheduleKind, setScheduleKind] = useState<ScheduleKind>(job.schedule.kind as ScheduleKind);
  const [everySeconds, setEverySeconds] = useState(getEverySeconds(job));
  const [cronExpr, setCronExpr] = useState(job.schedule.expr ?? "0 * * * *");
  const [message, setMessage] = useState(job.payload?.message ?? "");
  const [agentId, setAgentId] = useState(job.agentId ?? "");
  const [enabled, setEnabled] = useState(job.enabled);

  const [saving, setSaving] = useState(false);
  const [saved, setSaved] = useState(false);
  const [saveError, setSaveError] = useState<string | null>(null);

  const handleSave = async () => {
    if (!onUpdate) return;
    setSaving(true);
    setSaveError(null);
    setSaved(false);
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
      setSaved(true);
      setTimeout(() => setSaved(false), 3000);
    } catch (err) {
      setSaveError(err instanceof Error ? err.message : t("detail.saveFailed"));
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

      {/* Message section */}
      <section className="space-y-3 rounded-lg border p-3 sm:p-4 overflow-hidden">
        <h3 className="text-sm font-medium">{t("detail.messageSection")}</h3>
        <div className="space-y-2">
          <Label>{t("create.message")}</Label>
          <Textarea
            value={message}
            onChange={(e) => setMessage(e.target.value)}
            rows={4}
            disabled={readonly}
            placeholder={t("create.messagePlaceholder")}
            className="text-base md:text-sm resize-none"
          />
        </div>
      </section>

      {/* Agent & Status section */}
      <section className="space-y-3 rounded-lg border p-3 sm:p-4 overflow-hidden">
        <h3 className="text-sm font-medium">{t("detail.agentStatus")}</h3>
        <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
          <div className="space-y-2">
            <Label>{t("create.agentId")}</Label>
            <Input
              value={agentId}
              onChange={(e) => setAgentId(e.target.value)}
              disabled={readonly}
              placeholder={t("create.agentIdPlaceholder")}
              className="text-base md:text-sm"
            />
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

        {/* Info rows */}
        <div className="grid grid-cols-1 gap-4 rounded-md bg-muted/50 p-4 sm:grid-cols-2">
          {job.state?.nextRunAtMs && (
            <InfoRow label={t("detail.infoRows.nextRun")} value={formatDate(new Date(job.state.nextRunAtMs))} />
          )}
          {job.state?.lastRunAtMs && (
            <InfoRow label={t("detail.infoRows.lastRun")} value={formatDate(new Date(job.state.lastRunAtMs))} />
          )}
          {job.state?.lastStatus && (
            <InfoRow label={t("detail.infoRows.lastStatus")} value={job.state.lastStatus} />
          )}
          <InfoRow label={t("detail.infoRows.created")} value={formatDate(new Date(job.createdAtMs))} />
          <InfoRow label={t("detail.infoRows.updated")} value={formatDate(new Date(job.updatedAtMs))} />
          {job.schedule.tz && (
            <InfoRow label={t("detail.infoRows.timezone")} value={job.schedule.tz} />
          )}
        </div>

        {/* Last error */}
        {job.state?.lastError && (
          <div className="rounded-md border border-destructive/30 bg-destructive/5 p-4 text-sm">
            <h4 className="mb-1 font-medium text-destructive">{t("detail.lastError")}</h4>
            <p className="text-xs text-destructive/80">{job.state.lastError}</p>
          </div>
        )}
      </section>

      {!readonly && (
        <StickySaveBar
          onSave={handleSave}
          saving={saving}
          saved={saved}
          error={saveError}
        />
      )}
    </div>
  );
}
