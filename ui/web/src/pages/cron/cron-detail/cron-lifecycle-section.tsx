import { useTranslation } from "react-i18next";
import { Settings } from "lucide-react";
import { Switch } from "@/components/ui/switch";

interface CronLifecycleSectionProps {
  deleteAfterRun: boolean;
  setDeleteAfterRun: (v: boolean) => void;
  stateless: boolean;
  setStateless: (v: boolean) => void;
  readonly: boolean;
}

export function CronLifecycleSection({
  deleteAfterRun,
  setDeleteAfterRun,
  stateless,
  setStateless,
  readonly,
}: CronLifecycleSectionProps) {
  const { t } = useTranslation("cron");

  return (
    <section className="space-y-3 rounded-lg border p-3 sm:p-4 overflow-hidden">
      <div className="flex items-center gap-2">
        <Settings className="h-4 w-4 text-muted-foreground" />
        <h3 className="text-sm font-medium">{t("detail.lifecycle")}</h3>
      </div>

      <div className="flex items-center justify-between gap-4 rounded-md border px-3 py-2.5">
        <div>
          <p className="text-sm font-medium">{t("detail.deleteAfterRun")}</p>
          <p className="text-xs text-muted-foreground">{t("detail.deleteAfterRunDesc")}</p>
        </div>
        <Switch checked={deleteAfterRun} onCheckedChange={setDeleteAfterRun} disabled={readonly} />
      </div>

      <div className="flex items-center justify-between gap-4 rounded-md border px-3 py-2.5">
        <div>
          <p className="text-sm font-medium">{t("stateless")}</p>
          <p className="text-xs text-muted-foreground">{t("statelessHelp")}</p>
        </div>
        <Switch checked={stateless} onCheckedChange={setStateless} disabled={readonly} />
      </div>
    </section>
  );
}
