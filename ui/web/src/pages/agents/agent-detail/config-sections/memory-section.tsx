import { useTranslation } from "react-i18next";
import { Input } from "@/components/ui/input";
import { Switch } from "@/components/ui/switch";
import type { MemoryConfig } from "@/types/agent";
import { InfoLabel, numOrUndef } from "./config-section";

interface MemorySectionProps {
  value: MemoryConfig;
  onChange: (v: MemoryConfig) => void;
}

export function MemorySection({ value, onChange }: MemorySectionProps) {
  const { t } = useTranslation("agents");
  const s = "configSections.memory";
  return (
    <section className="space-y-3">
      <div>
        <h3 className="text-sm font-medium">{t(`${s}.title`)}</h3>
        <p className="text-xs text-muted-foreground">{t(`${s}.description`)}</p>
      </div>
      <div className="rounded-lg border p-3 space-y-4 sm:p-4">
        <div className="flex items-center gap-2">
          <Switch
            checked={value.enabled ?? true}
            onCheckedChange={(v) => onChange({ ...value, enabled: v })}
          />
          <InfoLabel tip={t(`${s}.enabledTip`)}>{t(`${s}.enabled`)}</InfoLabel>
        </div>
        <div className="grid grid-cols-1 gap-4 sm:grid-cols-2">
          <div className="space-y-2">
            <InfoLabel tip={t(`${s}.maxChunkLenTip`)}>{t(`${s}.maxChunkLen`)}</InfoLabel>
            <Input
              type="number"
              placeholder="1000"
              value={value.max_chunk_len ?? ""}
              onChange={(e) => onChange({ ...value, max_chunk_len: numOrUndef(e.target.value) })}
              className="text-base md:text-sm"
            />
          </div>
          <div className="space-y-2">
            <InfoLabel tip={t(`${s}.chunkOverlapTip`)}>{t(`${s}.chunkOverlap`)}</InfoLabel>
            <Input
              type="number"
              placeholder="200"
              value={value.chunk_overlap ?? ""}
              onChange={(e) => onChange({ ...value, chunk_overlap: numOrUndef(e.target.value) })}
              className="text-base md:text-sm"
            />
          </div>
        </div>
        <div className="grid grid-cols-1 gap-4 sm:grid-cols-2">
          <div className="space-y-2">
            <InfoLabel tip={t(`${s}.maxResultsTip`)}>{t(`${s}.maxResults`)}</InfoLabel>
            <Input
              type="number"
              placeholder="6"
              value={value.max_results ?? ""}
              onChange={(e) => onChange({ ...value, max_results: numOrUndef(e.target.value) })}
              className="text-base md:text-sm"
            />
          </div>
          <div className="space-y-2">
            <InfoLabel tip={t(`${s}.minScoreTip`)}>{t(`${s}.minScore`)}</InfoLabel>
            <Input
              type="number"
              step="0.01"
              placeholder="0.35"
              value={value.min_score ?? ""}
              onChange={(e) => onChange({ ...value, min_score: numOrUndef(e.target.value) })}
              className="text-base md:text-sm"
            />
          </div>
        </div>
        <div className="grid grid-cols-1 gap-4 sm:grid-cols-2">
          <div className="space-y-2">
            <InfoLabel tip={t(`${s}.vectorWeightTip`)}>{t(`${s}.vectorWeight`)}</InfoLabel>
            <Input
              type="number"
              step="0.1"
              placeholder="0.7"
              value={value.vector_weight ?? ""}
              onChange={(e) => onChange({ ...value, vector_weight: numOrUndef(e.target.value) })}
              className="text-base md:text-sm"
            />
          </div>
          <div className="space-y-2">
            <InfoLabel tip={t(`${s}.textWeightTip`)}>{t(`${s}.textWeight`)}</InfoLabel>
            <Input
              type="number"
              step="0.1"
              placeholder="0.3"
              value={value.text_weight ?? ""}
              onChange={(e) => onChange({ ...value, text_weight: numOrUndef(e.target.value) })}
              className="text-base md:text-sm"
            />
          </div>
        </div>
      </div>
    </section>
  );
}
