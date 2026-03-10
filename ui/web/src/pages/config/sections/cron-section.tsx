import { useState, useEffect } from "react";
import { Save } from "lucide-react";
import { useTranslation } from "react-i18next";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { InfoLabel } from "@/components/shared/info-label";

const TIMEZONES = [
  { value: "UTC", label: "UTC" },
  { value: "Asia/Ho_Chi_Minh", label: "Asia/Ho_Chi_Minh (UTC+7)" },
  { value: "Asia/Bangkok", label: "Asia/Bangkok (UTC+7)" },
  { value: "Asia/Shanghai", label: "Asia/Shanghai (UTC+8)" },
  { value: "Asia/Tokyo", label: "Asia/Tokyo (UTC+9)" },
  { value: "Asia/Seoul", label: "Asia/Seoul (UTC+9)" },
  { value: "Asia/Singapore", label: "Asia/Singapore (UTC+8)" },
  { value: "Asia/Kolkata", label: "Asia/Kolkata (UTC+5:30)" },
  { value: "Asia/Dubai", label: "Asia/Dubai (UTC+4)" },
  { value: "Europe/London", label: "Europe/London (UTC+0)" },
  { value: "Europe/Paris", label: "Europe/Paris (UTC+1)" },
  { value: "Europe/Berlin", label: "Europe/Berlin (UTC+1)" },
  { value: "Europe/Moscow", label: "Europe/Moscow (UTC+3)" },
  { value: "America/New_York", label: "America/New_York (UTC-5)" },
  { value: "America/Chicago", label: "America/Chicago (UTC-6)" },
  { value: "America/Denver", label: "America/Denver (UTC-7)" },
  { value: "America/Los_Angeles", label: "America/Los_Angeles (UTC-8)" },
  { value: "America/Sao_Paulo", label: "America/Sao_Paulo (UTC-3)" },
  { value: "Australia/Sydney", label: "Australia/Sydney (UTC+11)" },
  { value: "Pacific/Auckland", label: "Pacific/Auckland (UTC+13)" },
];

interface CronData {
  max_retries?: number;
  retry_base_delay?: string;
  retry_max_delay?: string;
  default_timezone?: string;
}

const DEFAULT: CronData = {};

interface Props {
  data: CronData | undefined;
  onSave: (value: CronData) => Promise<void>;
  saving: boolean;
}

export function CronSection({ data, onSave, saving }: Props) {
  const { t } = useTranslation("config");
  const [draft, setDraft] = useState<CronData>(data ?? DEFAULT);
  const [dirty, setDirty] = useState(false);

  useEffect(() => {
    setDraft(data ?? DEFAULT);
    setDirty(false);
  }, [data]);

  const update = (patch: Partial<CronData>) => {
    setDraft((prev) => ({ ...prev, ...patch }));
    setDirty(true);
  };

  if (!data) return null;

  return (
    <Card>
      <CardHeader className="pb-3">
        <CardTitle className="text-base">{t("cron.title")}</CardTitle>
        <CardDescription>{t("cron.description")}</CardDescription>
      </CardHeader>
      <CardContent className="space-y-4">
        <div className="grid gap-1.5">
          <InfoLabel tip={t("cron.defaultTimezoneTip")}>{t("cron.defaultTimezone")}</InfoLabel>
          <Select
            value={draft.default_timezone || "__system__"}
            onValueChange={(v) => update({ default_timezone: v === "__system__" ? "" : v })}
          >
            <SelectTrigger>
              <SelectValue placeholder={t("cron.defaultTimezonePlaceholder")} />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value="__system__">{t("cron.defaultTimezonePlaceholder")}</SelectItem>
              {TIMEZONES.map((tz) => (
                <SelectItem key={tz.value} value={tz.value}>{tz.label}</SelectItem>
              ))}
            </SelectContent>
          </Select>
        </div>

        <div className="grid grid-cols-3 gap-4">
          <div className="grid gap-1.5">
            <InfoLabel tip={t("cron.maxRetriesTip")}>{t("cron.maxRetries")}</InfoLabel>
            <Input
              type="number"
              value={draft.max_retries ?? ""}
              onChange={(e) => update({ max_retries: Number(e.target.value) })}
              placeholder="3"
              min={0}
            />
          </div>
          <div className="grid gap-1.5">
            <InfoLabel tip={t("cron.baseDelayTip")}>{t("cron.baseDelay")}</InfoLabel>
            <Input
              value={draft.retry_base_delay ?? ""}
              onChange={(e) => update({ retry_base_delay: e.target.value })}
              placeholder="2s"
            />
          </div>
          <div className="grid gap-1.5">
            <InfoLabel tip={t("cron.maxDelayTip")}>{t("cron.maxDelay")}</InfoLabel>
            <Input
              value={draft.retry_max_delay ?? ""}
              onChange={(e) => update({ retry_max_delay: e.target.value })}
              placeholder="30s"
            />
          </div>
        </div>

        {dirty && (
          <div className="flex justify-end pt-2">
            <Button size="sm" onClick={() => onSave(draft)} disabled={saving} className="gap-1.5">
              <Save className="h-3.5 w-3.5" /> {saving ? t("saving") : t("save")}
            </Button>
          </div>
        )}
      </CardContent>
    </Card>
  );
}
