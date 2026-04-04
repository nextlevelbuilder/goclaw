import { useState } from "react";
import { useTranslation } from "react-i18next";
import { ChevronRight } from "lucide-react";
import { Input } from "@/components/ui/input";
import { Switch } from "@/components/ui/switch";
import type { ContextPruningConfig } from "@/types/agent";
import { ConfigSection, InfoLabel, numOrUndef } from "./config-section";

interface ContextPruningSectionProps {
  enabled: boolean;
  value: ContextPruningConfig;
  onToggle: (v: boolean) => void;
  onChange: (v: ContextPruningConfig) => void;
}

export function ContextPruningSection({ enabled, value, onToggle, onChange }: ContextPruningSectionProps) {
  const { t } = useTranslation("agents");
  const s = "configSections.contextPruning";
  const [showAdvanced, setShowAdvanced] = useState(false);

  return (
    <ConfigSection
      title={t(`${s}.title`)}
      description={t(`${s}.descriptionSimple`, "Automatically trims old tool results to prevent context overflow. Enabled by default.")}
      enabled={enabled}
      onToggle={onToggle}
    >
      {/* Primary setting */}
      <div className="max-w-xs space-y-2">
        <InfoLabel tip="Number of recent assistant turns whose tool results are always kept intact, never pruned.">{t(`${s}.keepLastAssistants`)}</InfoLabel>
        <Input
          type="number"
          placeholder="3"
          value={value.keepLastAssistants ?? ""}
          onChange={(e) =>
            onChange({ ...value, keepLastAssistants: numOrUndef(e.target.value) })
          }
        />
      </div>

      {/* Advanced toggle */}
      <button
        type="button"
        className="flex items-center gap-1 text-xs text-muted-foreground hover:text-foreground transition-colors"
        onClick={() => setShowAdvanced(!showAdvanced)}
      >
        <ChevronRight className={`h-3 w-3 transition-transform ${showAdvanced ? "rotate-90" : ""}`} />
        {t(`${s}.advanced`, "Advanced")}
      </button>

      {showAdvanced && (
        <div className="space-y-4 pl-4 border-l-2 border-muted">
          {/* Trim ratios */}
          <div className="grid grid-cols-1 gap-4 sm:grid-cols-2">
            <div className="space-y-2">
              <InfoLabel tip="Context usage ratio (0-1) at which soft trimming begins. E.g. 0.3 means trimming starts when context is 30% full.">{t(`${s}.softTrimRatio`)}</InfoLabel>
              <Input
                type="number"
                step="0.05"
                placeholder="0.3"
                value={value.softTrimRatio ?? ""}
                onChange={(e) => onChange({ ...value, softTrimRatio: numOrUndef(e.target.value) })}
              />
            </div>
            <div className="space-y-2">
              <InfoLabel tip="Context usage ratio (0-1) at which hard clearing kicks in. E.g. 0.5 means full clearing at 50% context usage.">{t(`${s}.hardClearRatio`)}</InfoLabel>
              <Input
                type="number"
                step="0.05"
                placeholder="0.5"
                value={value.hardClearRatio ?? ""}
                onChange={(e) => onChange({ ...value, hardClearRatio: numOrUndef(e.target.value) })}
              />
            </div>
          </div>

          {/* Soft Trim chars */}
          <div className="grid grid-cols-1 gap-4 sm:grid-cols-3">
            <div className="space-y-2">
              <InfoLabel tip="Tool results longer than this will be soft-trimmed, keeping only head and tail portions.">{t(`${s}.maxChars`)}</InfoLabel>
              <Input
                type="number"
                placeholder="4000"
                value={value.softTrim?.maxChars ?? ""}
                onChange={(e) =>
                  onChange({ ...value, softTrim: { ...value.softTrim, maxChars: numOrUndef(e.target.value) } })
                }
              />
            </div>
            <div className="space-y-2">
              <InfoLabel tip="Number of characters to keep from the beginning of a trimmed tool result.">{t(`${s}.headChars`)}</InfoLabel>
              <Input
                type="number"
                placeholder="1500"
                value={value.softTrim?.headChars ?? ""}
                onChange={(e) =>
                  onChange({ ...value, softTrim: { ...value.softTrim, headChars: numOrUndef(e.target.value) } })
                }
              />
            </div>
            <div className="space-y-2">
              <InfoLabel tip="Number of characters to keep from the end of a trimmed tool result.">{t(`${s}.tailChars`)}</InfoLabel>
              <Input
                type="number"
                placeholder="1500"
                value={value.softTrim?.tailChars ?? ""}
                onChange={(e) =>
                  onChange({ ...value, softTrim: { ...value.softTrim, tailChars: numOrUndef(e.target.value) } })
                }
              />
            </div>
          </div>

          {/* Hard Clear */}
          <div className="space-y-3">
            <div className="flex items-center gap-2">
              <Switch
                checked={value.hardClear?.enabled ?? true}
                onCheckedChange={(v) =>
                  onChange({ ...value, hardClear: { ...value.hardClear, enabled: v } })
                }
              />
              <InfoLabel tip="When enabled, old tool results beyond the hard clear ratio are replaced entirely with placeholder text.">{t(`${s}.hardClear`)}</InfoLabel>
            </div>
          </div>
        </div>
      )}
    </ConfigSection>
  );
}
