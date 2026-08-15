import { useTranslation } from "react-i18next";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { Switch } from "@/components/ui/switch";
import type { ReasoningOverrideMode } from "@/types/agent";
import type { ReasoningCapability } from "@/types/provider";
import { InfoLabel } from "./config-section";

const SIMPLE_LEVELS = ["off", "low", "medium", "high"] as const;
const FALLBACKS = ["downgrade", "provider_default", "off"] as const;

interface ThinkingSectionProps {
  reasoningMode: ReasoningOverrideMode;
  thinkingLevel: string;
  reasoningEffort: string;
  reasoningFallback: string;
  expertMode: boolean;
  model: string;
  capability?: ReasoningCapability | null;
  providerDefault?: {
    effort?: string;
    fallback?: "downgrade" | "provider_default" | "off";
  } | null;
  providerLabel?: string;
  capabilityLoading?: boolean;
  onReasoningModeChange: (v: ReasoningOverrideMode) => void;
  onThinkingLevelChange: (v: string) => void;
  onReasoningEffortChange: (v: string) => void;
  onReasoningFallbackChange: (v: string) => void;
  onExpertModeChange: (v: boolean) => void;
}

export function ThinkingSection({
  reasoningMode,
  thinkingLevel,
  reasoningEffort,
  reasoningFallback,
  expertMode,
  model,
  capability,
  providerDefault,
  providerLabel,
  capabilityLoading = false,
  onReasoningModeChange,
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
  const inheritedEffort = normalizeInheritedEffort(providerDefault?.effort);
  const inheritedFallback = providerDefault?.fallback ?? "downgrade";
  const showCustomControls = reasoningMode === "custom";

  return (
    <section className="space-y-3">
      <div>
        <h3 className="text-sm font-medium">{t(`${s}.title`)}</h3>
        <p className="text-xs text-muted-foreground">
          {t(`${s}.description`)}
        </p>
      </div>

      <section className="space-y-2.5">
        <p className="text-xs font-medium uppercase tracking-wide text-muted-foreground">
          {t(`${s}.modeLabel`)}
        </p>
        <div className="grid gap-1.5 xl:grid-cols-2">
          <Button
            type="button"
            variant={reasoningMode === "inherit" ? "default" : "outline"}
            onClick={() => onReasoningModeChange("inherit")}
            className="h-9"
          >
            {t(`${s}.inherit`)}
          </Button>
          <Button
            type="button"
            variant={reasoningMode === "custom" ? "default" : "outline"}
            onClick={() => onReasoningModeChange("custom")}
            className="h-9"
          >
            {t(`${s}.custom`)}
          </Button>
        </div>
        {reasoningMode === "inherit" ? (
          providerDefault ? (
            <div className="rounded-lg border px-3 py-3 text-sm">
              <p className="font-medium">
                {t(`${s}.providerDefaultSummary`, {
                  provider: providerLabel || t(`${s}.providerLabelFallback`),
                })}
              </p>
              <div className="mt-2 flex flex-wrap gap-2 text-xs text-muted-foreground">
                <Badge variant="secondary">{t(`${s}.${inheritedEffort}`)}</Badge>
                <Badge variant="outline">{t(`${s}.${inheritedFallback}`)}</Badge>
              </div>
            </div>
          ) : (
            <div className="rounded-lg border border-dashed px-3 py-3 text-sm text-muted-foreground">
              {t(`${s}.noProviderDefault`)}
            </div>
          )
        ) : (
          <p className="text-xs text-muted-foreground">
            {t(`${s}.customDesc`)}
          </p>
        )}
      </section>

      {showCustomControls ? (
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
      ) : null}

      <div className="rounded-md border p-3">
        {expertAvailable && showCustomControls ? (
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
                <Badge key={level} variant="outline" className="text-2xs">
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

        {expertAvailable && expertMode && showCustomControls ? (
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

function normalizeInheritedEffort(value: string | undefined): string {
  if (!value) return "off";
  return [
    "off", "auto", "none", "minimal", "low", "medium", "high", "xhigh",
  ].includes(value) ? value : "off";
}
