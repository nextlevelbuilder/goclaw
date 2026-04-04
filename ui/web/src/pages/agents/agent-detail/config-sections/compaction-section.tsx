import { useTranslation } from "react-i18next";
import { Input } from "@/components/ui/input";
import { Switch } from "@/components/ui/switch";
import type { CompactionConfig } from "@/types/agent";
import { InfoLabel, numOrUndef } from "./config-section";

interface CompactionSectionProps {
  value: CompactionConfig;
  onChange: (v: CompactionConfig) => void;
}

export function CompactionSection({ value, onChange }: CompactionSectionProps) {
  const { t } = useTranslation("agents");
  const s = "configSections.compaction";
  return (
    <section className="space-y-3">
      <div>
        <h3 className="text-sm font-medium">{t(`${s}.title`)}</h3>
        <p className="text-xs text-muted-foreground">{t(`${s}.description`)}</p>
      </div>
      <div className="rounded-lg border p-3 space-y-4 sm:p-4">
      <div className="grid grid-cols-1 gap-4 sm:grid-cols-2">
        <div className="space-y-2">
          <InfoLabel tip={t(`${s}.maxHistoryShareTip`)}>{t(`${s}.maxHistoryShare`)}</InfoLabel>
          <Input
            type="number"
            step="0.05"
            placeholder="0.85"
            value={value.maxHistoryShare ?? ""}
            onChange={(e) => onChange({ ...value, maxHistoryShare: numOrUndef(e.target.value) })}
          />
        </div>
        <div className="space-y-2">
          <InfoLabel tip={t(`${s}.keepLastMessagesTip`)}>{t(`${s}.keepLastMessages`)}</InfoLabel>
          <Input
            type="number"
            placeholder="4"
            value={value.keepLastMessages ?? ""}
            onChange={(e) => onChange({ ...value, keepLastMessages: numOrUndef(e.target.value) })}
          />
        </div>
      </div>
      <div className="flex items-center gap-2">
        <Switch
          checked={value.memoryFlush?.enabled ?? true}
          onCheckedChange={(v) =>
            onChange({ ...value, memoryFlush: { ...value.memoryFlush, enabled: v } })
          }
        />
        <InfoLabel tip={t(`${s}.memoryFlushTip`)}>{t(`${s}.memoryFlush`)}</InfoLabel>
      </div>
      </div>
    </section>
  );
}
