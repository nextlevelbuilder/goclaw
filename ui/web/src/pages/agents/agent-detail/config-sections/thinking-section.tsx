import { useTranslation } from "react-i18next";
import { Badge } from "@/components/ui/badge";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { Switch } from "@/components/ui/switch";
import type { ReasoningCapability } from "@/types/provider";
import { InfoLabel } from "./config-section";

const SIMPLE_LEVELS = ["off", "low", "medium", "high"] as const;
const FALLBACKS = ["downgrade", "provider_default", "off"] as const;

interface ThinkingSectionProps {
  thinkingLevel: string;
  reasoningEffort: string;
  reasoningFallback: string;
  expertMode: boolean;
  model: string;
  capability?: ReasoningCapability | null;
  capabilityLoading?: boolean;
  onThinkingLevelChange: (v: string) => void;
  onReasoningEffortChange: (v: string) => void;
  onReasoningFallbackChange: (v: string) => void;
  onExpertModeChange: (v: boolean) => void;
}

export function ThinkingSection({
  thinkingLevel,
  reasoningEffort,
  reasoningFallback,
  expertMode,
  model,
  capability,
  capabilityLoading = false,
  onThinkingLevelChange,
  onReasoningEffortChange,
  onReasoningFallbackChange,
  onExpertModeChange,
}: ThinkingSectionProps) {
  const { t } = useTranslation("agents");
  const s = "configSections.thinking";
  const supported = new Set(capability?.levels ?? []);
  const expertAvailable = Boolean(capability?.levels?.length);
  const advancedEfforts = ["off", "auto", ...(capability?.levels ?? [])];
  const currentEffort = advancedEfforts.includes(reasoningEffort || "")
    ? reasoningEffort
    : advancedEfforts.includes(thinkingLevel)
      ? thinkingLevel
      : capability?.default_effort ?? "off";

  return (
    <section className="space-y-3">
      <div>
        <h3 className="text-sm font-medium">{t(`${s}.title`)}</h3>
        <p className="text-xs text-muted-foreground">
          {t(`${s}.description`)}
        </p>
      </div>

      <div className="space-y-2">
        <InfoLabel tip={t(`${s}.thinkingLevelTip`)}>
          {t(`${s}.thinkingLevel`)}
        </InfoLabel>
        <Select
          value={thinkingLevel || "off"}
          onValueChange={(value) => {
            onThinkingLevelChange(value);
            onReasoningEffortChange(value);
          }}
        >
          <SelectTrigger className="w-56">
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            {SIMPLE_LEVELS.map((level) => (
              <SelectItem key={level} value={level}>
                <span>{t(`${s}.${level}`)}</span>
                <span className="ml-2 text-xs text-muted-foreground">
                  {t(`${s}.${level}Desc`)}
                </span>
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
      </div>

      <div className="rounded-md border p-3">
        {expertAvailable ? (
          <div className="flex items-center justify-between gap-3">
            <div className="space-y-1">
              <p className="text-sm font-medium">{t(`${s}.expertMode`)}</p>
              <p className="text-xs text-muted-foreground">
                {t(`${s}.expertModeDesc`)}
              </p>
            </div>
            <Switch checked={expertMode} onCheckedChange={onExpertModeChange} />
          </div>
        ) : null}

        <div className="mt-3 space-y-2 text-xs text-muted-foreground">
          <p>
            {capability?.levels?.length
              ? t(`${s}.supportedLevelsForModel`, { model })
              : capabilityLoading
                ? t(`${s}.loadingSupport`, { model })
                : t(`${s}.unknownSupport`, { model })}
          </p>
          {capability?.levels?.length ? (
            <div className="flex flex-wrap gap-1">
              {capability.levels.map((level) => (
                <Badge key={level} variant="outline" className="text-[10px]">
                  {t(`${s}.${level}`)}
                </Badge>
              ))}
            </div>
          ) : null}
          {capability?.default_effort ? (
            <p>
              {t(`${s}.modelDefault`, {
                level: t(`${s}.${capability.default_effort}`),
              })}
            </p>
          ) : null}
          {!expertAvailable && !capabilityLoading ? (
            <p>{t(`${s}.expertModeUnavailable`)}</p>
          ) : null}
        </div>

        {expertAvailable && expertMode ? (
          <div className="mt-4 space-y-3 border-t pt-3">
            <div className="space-y-2">
              <InfoLabel tip={t(`${s}.requestedEffortTip`)}>
                {t(`${s}.requestedEffort`)}
              </InfoLabel>
              <Select
                value={currentEffort}
                onValueChange={onReasoningEffortChange}
              >
                <SelectTrigger className="w-full sm:w-72">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  {advancedEfforts.map((effort) => (
                    <SelectItem key={effort} value={effort}>
                      <span>{t(`${s}.${effort}`)}</span>
                      <span className="ml-2 text-xs text-muted-foreground">
                        {effort !== "off" && effort !== "auto" && supported.has(effort)
                          ? t(`${s}.supportedOption`)
                          : t(`${s}.${effort}Desc`)}
                      </span>
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </div>

            <div className="space-y-2">
              <InfoLabel tip={t(`${s}.fallbackBehaviorTip`)}>
                {t(`${s}.fallbackBehavior`)}
              </InfoLabel>
              <Select
                value={reasoningFallback}
                onValueChange={onReasoningFallbackChange}
              >
                <SelectTrigger className="w-full sm:w-72">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  {FALLBACKS.map((fallback) => (
                    <SelectItem key={fallback} value={fallback}>
                      <span>{t(`${s}.${fallback}`)}</span>
                      <span className="ml-2 text-xs text-muted-foreground">
                        {t(`${s}.${fallback}Desc`)}
                      </span>
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </div>

            <p className="text-xs text-muted-foreground">
              {t(`${s}.legacyShim`)}
            </p>
          </div>
        ) : null}
      </div>
    </section>
  );
}
