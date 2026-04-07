import { useTranslation } from "react-i18next";
import { Cpu, Info } from "lucide-react";
import { Label } from "@/components/ui/label";
import { Switch } from "@/components/ui/switch";
import { useV3Flags, type V3Flags } from "@/hooks/use-v3-flags";

const FLAG_ITEMS: { key: keyof V3Flags; icon: string; labelKey: string; hintKey: string }[] = [
  { key: "v3_pipeline_enabled", icon: "🔄", labelKey: "detail.v3.pipeline", hintKey: "detail.v3.pipelineHint" },
  { key: "v3_memory_enabled", icon: "🧠", labelKey: "detail.v3.memory", hintKey: "detail.v3.memoryHint" },
  { key: "v3_retrieval_enabled", icon: "🔍", labelKey: "detail.v3.retrieval", hintKey: "detail.v3.retrievalHint" },
];

interface V3SettingsSectionProps {
  agentId: string;
}

export function V3SettingsSection({ agentId }: V3SettingsSectionProps) {
  const { t } = useTranslation("agents");
  const { flags, loading, toggleFlag } = useV3Flags(agentId);

  if (loading || !flags) return null;

  return (
    <section className="space-y-3 rounded-lg border p-3 sm:p-4">
      <div className="flex items-center gap-2">
        <Cpu className="h-4 w-4 text-blue-500 shrink-0" />
        <h3 className="text-sm font-medium">{t("detail.v3.title")}</h3>
      </div>

      {FLAG_ITEMS.map(({ key, icon, labelKey, hintKey }) => (
        <div key={key} className="flex items-center justify-between gap-4">
          <div className="flex items-center gap-2">
            <span className="text-sm shrink-0">{icon}</span>
            <div className="space-y-0.5">
              <Label htmlFor={key} className="text-sm font-normal cursor-pointer">
                {t(labelKey)}
              </Label>
              <p className="text-xs text-muted-foreground">{t(hintKey)}</p>
            </div>
          </div>
          <Switch
            id={key}
            checked={flags[key]}
            onCheckedChange={(v) => toggleFlag(key, v)}
          />
        </div>
      ))}

      {flags.v3_pipeline_enabled && (
        <div className="flex items-start gap-2 rounded-md border border-blue-200 bg-blue-50 px-3 py-2 text-xs text-blue-700 dark:border-blue-800 dark:bg-blue-950/30 dark:text-blue-300">
          <Info className="mt-0.5 h-3.5 w-3.5 shrink-0" />
          <span>{t("detail.v3.pipelineActiveInfo")}</span>
        </div>
      )}
    </section>
  );
}
