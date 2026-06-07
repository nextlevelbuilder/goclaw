import { useTranslation } from "react-i18next";
import { Input } from "@/components/ui/input";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import type { InboundDebounceOverrideMode } from "@/types/agent";
import { InfoLabel } from "./config-section";

interface InboundDebounceSectionProps {
  mode: InboundDebounceOverrideMode;
  debounceMs: number;
  onModeChange: (mode: InboundDebounceOverrideMode) => void;
  onDebounceMsChange: (value: number) => void;
}

export function InboundDebounceSection({
  mode,
  debounceMs,
  onModeChange,
  onDebounceMsChange,
}: InboundDebounceSectionProps) {
  const { t } = useTranslation("agents");
  const s = "configSections.inboundDebounce";
  return (
    <section className="space-y-3">
      <div>
        <h3 className="text-sm font-medium">{t(`${s}.title`)}</h3>
        <p className="text-xs text-muted-foreground">{t(`${s}.description`)}</p>
      </div>
      <div className="rounded-lg border p-3 space-y-4 sm:p-4">
        <div className="grid grid-cols-1 gap-4 sm:grid-cols-2">
          <div className="space-y-2">
            <InfoLabel tip={t(`${s}.modeTip`)}>{t(`${s}.mode`)}</InfoLabel>
            <Select value={mode} onValueChange={(value) => onModeChange(value as InboundDebounceOverrideMode)}>
              <SelectTrigger className="w-full">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="inherit">{t(`${s}.inherit`)}</SelectItem>
                <SelectItem value="custom">{t(`${s}.custom`)}</SelectItem>
              </SelectContent>
            </Select>
          </div>
          <div className="space-y-2">
            <InfoLabel tip={t(`${s}.debounceMsTip`)}>{t(`${s}.debounceMs`)}</InfoLabel>
            <Input
              type="number"
              min={0}
              value={debounceMs}
              disabled={mode === "inherit"}
              onChange={(event) => onDebounceMsChange(Math.max(0, Number(event.target.value)))}
              placeholder="0"
            />
          </div>
        </div>
      </div>
    </section>
  );
}
