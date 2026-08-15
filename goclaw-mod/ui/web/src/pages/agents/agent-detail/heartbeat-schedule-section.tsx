import { useTranslation } from "react-i18next";
import { Clock } from "lucide-react";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Combobox } from "@/components/ui/combobox";
import { getAllIanaTimezones } from "@/lib/constants";

interface HeartbeatScheduleSectionProps {
  activeHoursStart: string;
  setActiveHoursStart: (v: string) => void;
  activeHoursEnd: string;
  setActiveHoursEnd: (v: string) => void;
  timezone: string;
  setTimezone: (v: string) => void;
  defaultTz: string;
}

/** Active-hours and timezone controls for the heartbeat schedule. */
export function HeartbeatScheduleSection({
  activeHoursStart, setActiveHoursStart,
  activeHoursEnd, setActiveHoursEnd,
  timezone, setTimezone,
  defaultTz,
}: HeartbeatScheduleSectionProps) {
  const { t } = useTranslation("agents");

  return (
    <div className="space-y-2">
      <div className="flex items-center gap-2">
        <Clock className="h-3.5 w-3.5 text-amber-500" />
        <h4 className="text-xs font-semibold uppercase tracking-wide text-muted-foreground">
          {t("heartbeat.sectionSchedule")}
        </h4>
      </div>
      <div className="flex flex-wrap items-end gap-3">
        <div className="space-y-1 w-24">
          <Label htmlFor="hb-start" className="text-xs">{t("heartbeat.activeHoursStart")}</Label>
          <Input
            id="hb-start"
            placeholder="08:00"
            value={activeHoursStart}
            onChange={(e) => setActiveHoursStart(e.target.value)}
            className="text-base md:text-sm"
          />
        </div>
        <div className="space-y-1 w-24">
          <Label htmlFor="hb-end" className="text-xs">{t("heartbeat.activeHoursEnd")}</Label>
          <Input
            id="hb-end"
            placeholder="22:00"
            value={activeHoursEnd}
            onChange={(e) => setActiveHoursEnd(e.target.value)}
            className="text-base md:text-sm"
          />
        </div>
        <div className="space-y-1 flex-1 min-w-[160px]">
          <Label className="text-xs">{t("heartbeat.timezone")}</Label>
          <Combobox
            value={timezone || "__auto__"}
            onChange={(v) => setTimezone(v === "__auto__" ? "" : v)}
            options={[{ value: "__auto__", label: defaultTz }, ...getAllIanaTimezones()]}
            placeholder={t("heartbeat.timezone")}
            className="text-base md:text-sm"
          />
        </div>
      </div>
      <p className="text-xs text-muted-foreground">{t("heartbeat.scheduleHint")}</p>
    </div>
  );
}
