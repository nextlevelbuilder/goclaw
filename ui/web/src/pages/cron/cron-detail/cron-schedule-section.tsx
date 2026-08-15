import { useTranslation } from "react-i18next";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Button } from "@/components/ui/button";
import { Combobox } from "@/components/ui/combobox";
import { getAllIanaTimezones } from "@/lib/constants";
import type { CronJob } from "../hooks/use-cron";

type ScheduleKind = "every" | "cron" | "at";

interface CronScheduleSectionProps {
  job: CronJob;
  scheduleKind: ScheduleKind;
  setScheduleKind: (kind: ScheduleKind) => void;
  everySeconds: string;
  setEverySeconds: (v: string) => void;
  cronExpr: string;
  setCronExpr: (v: string) => void;
  timezone: string;
  setTimezone: (v: string) => void;
  readonly: boolean;
}

export function CronScheduleSection({
  job,
  scheduleKind,
  setScheduleKind,
  everySeconds,
  setEverySeconds,
  cronExpr,
  setCronExpr,
  timezone,
  setTimezone,
  readonly,
}: CronScheduleSectionProps) {
  const { t } = useTranslation("cron");

  return (
    <section className="space-y-3 rounded-lg border p-3 sm:p-4 overflow-hidden">
      <h3 className="text-sm font-medium">{t("detail.schedule")}</h3>

      <div className="space-y-2">
        <Label>{t("create.name")}</Label>
        <Input value={job.name} disabled className="text-base md:text-sm" />
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
            type="number" min={1} value={everySeconds}
            onChange={(e) => setEverySeconds(e.target.value)}
            disabled={readonly} className="text-base md:text-sm"
          />
        </div>
      )}

      {scheduleKind === "cron" && (
        <div className="space-y-2">
          <Label>{t("create.cronExpression")}</Label>
          <Input
            value={cronExpr} onChange={(e) => setCronExpr(e.target.value)}
            disabled={readonly} placeholder="0 * * * *" className="text-base md:text-sm"
          />
          <p className="text-xs text-muted-foreground">{t("create.cronHint")}</p>
        </div>
      )}

      {scheduleKind === "at" && (
        <p className="text-sm text-muted-foreground">{t("create.onceDesc")}</p>
      )}

      <div className="space-y-2">
        <Label>{t("detail.timezone")}</Label>
        <Combobox
          value={timezone} onChange={setTimezone}
          options={getAllIanaTimezones()}
          placeholder={t("detail.timezone")}
          className="text-base md:text-sm"
        />
        <p className="text-xs text-muted-foreground">{t("detail.timezoneDesc")}</p>
      </div>
    </section>
  );
}
