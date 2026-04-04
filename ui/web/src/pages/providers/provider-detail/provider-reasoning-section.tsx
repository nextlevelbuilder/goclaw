import { useTranslation } from "react-i18next";
import { Label } from "@/components/ui/label";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { Switch } from "@/components/ui/switch";
import { Badge } from "@/components/ui/badge";
import { deriveLegacyThinkingLevel, normalizeReasoningFallback } from "@/types/provider";
import type { ReasoningCapability } from "@/types/provider";
import { ADVANCED_REASONING_LEVELS, REASONING_FALLBACKS } from "./provider-overview-helpers";

interface ReasoningModelEntry {
  id: string;
  name?: string;
  reasoning?: ReasoningCapability | null;
}

interface ProviderReasoningSectionProps {
  reasoningThinkingLevel: string;
  setReasoningThinkingLevel: (v: string) => void;
  reasoningEffort: string;
  setReasoningEffort: (v: string) => void;
  reasoningFallback: string;
  setReasoningFallback: (v: string) => void;
  reasoningExpert: boolean;
  setReasoningExpert: (v: boolean) => void;
  setReasoningPreviewModel: (v: string) => void;
  reasoningCapableModels: ReasoningModelEntry[];
  reasoningPreviewEntry: ReasoningModelEntry | null;
  reasoningPreviewCapability: ReasoningCapability | null;
}

export function ProviderReasoningSection({
  reasoningThinkingLevel,
  setReasoningThinkingLevel,
  reasoningEffort,
  setReasoningEffort,
  reasoningFallback,
  setReasoningFallback,
  reasoningExpert,
  setReasoningExpert,
  setReasoningPreviewModel,
  reasoningCapableModels,
  reasoningPreviewEntry,
  reasoningPreviewCapability,
}: ProviderReasoningSectionProps) {
  const { t } = useTranslation("providers");

  return (
    <section className="space-y-4 rounded-lg border p-3 sm:p-4 overflow-hidden">
      <div className="space-y-1">
        <h3 className="text-sm font-medium">{t("detail.reasoningDefaultsTitle")}</h3>
        <p className="text-xs text-muted-foreground">
          {t("detail.reasoningDefaultsDescription")}
        </p>
      </div>

      <div className="space-y-2">
        <Label>{t("detail.reasoningPreset")}</Label>
        <Select
          value={reasoningThinkingLevel}
          onValueChange={(value) => {
            setReasoningThinkingLevel(value);
            setReasoningEffort(value);
          }}
        >
          <SelectTrigger className="w-full sm:w-56">
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            {(["off", "low", "medium", "high"] as const).map((level) => (
              <SelectItem key={level} value={level}>
                <span>{t(`reasoning.${level}`)}</span>
                <span className="ml-2 text-xs text-muted-foreground">
                  {t(`reasoning.${level}Desc`)}
                </span>
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
      </div>

      <div className="rounded-md border p-3">
        <div className="flex items-center justify-between gap-3">
          <div className="space-y-1">
            <p className="text-sm font-medium">{t("detail.reasoningExpertMode")}</p>
            <p className="text-xs text-muted-foreground">
              {t("detail.reasoningExpertModeDesc")}
            </p>
          </div>
          <Switch
            checked={reasoningExpert}
            onCheckedChange={(enabled) => {
              setReasoningExpert(enabled);
              if (!enabled) {
                const legacy = deriveLegacyThinkingLevel(reasoningEffort);
                setReasoningThinkingLevel(legacy);
                setReasoningFallback("downgrade");
              } else if (reasoningEffort === "off" && reasoningThinkingLevel !== "off") {
                setReasoningEffort(reasoningThinkingLevel);
              }
            }}
          />
        </div>

        <div className="mt-3 space-y-2 text-xs text-muted-foreground">
          {reasoningPreviewEntry ? (
            <>
              <p>
                {t("detail.reasoningPreviewDescription", {
                  model: reasoningPreviewEntry.name || reasoningPreviewEntry.id,
                })}
              </p>
              <div className="space-y-2">
                <Label htmlFor="reasoningPreviewModel">{t("detail.reasoningPreviewLabel")}</Label>
                <Select value={reasoningPreviewEntry.id} onValueChange={setReasoningPreviewModel}>
                  <SelectTrigger id="reasoningPreviewModel" className="w-full sm:w-72">
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent>
                    {reasoningCapableModels.map((model) => (
                      <SelectItem key={model.id} value={model.id}>
                        {model.name || model.id}
                      </SelectItem>
                    ))}
                  </SelectContent>
                </Select>
              </div>
              {reasoningPreviewCapability?.levels?.length ? (
                <div className="flex flex-wrap gap-1">
                  {reasoningPreviewCapability.levels.map((level) => (
                    <Badge key={level} variant="outline" className="text-[10px]">
                      {t(`reasoning.${level}`)}
                    </Badge>
                  ))}
                </div>
              ) : null}
              {reasoningPreviewCapability?.default_effort ? (
                <p>
                  {t("detail.reasoningPreviewDefault", {
                    level: t(`reasoning.${reasoningPreviewCapability.default_effort}`),
                  })}
                </p>
              ) : null}
            </>
          ) : (
            <p>{t("detail.reasoningPreviewEmpty")}</p>
          )}
        </div>

        {reasoningExpert ? (
          <div className="mt-4 space-y-3 border-t pt-3">
            <div className="space-y-2">
              <Label>{t("detail.reasoningRequestedEffort")}</Label>
              <Select value={reasoningEffort} onValueChange={setReasoningEffort}>
                <SelectTrigger className="w-full sm:w-72">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  {ADVANCED_REASONING_LEVELS.map((effort) => (
                    <SelectItem key={effort} value={effort}>
                      <span>{t(`reasoning.${effort}`)}</span>
                      <span className="ml-2 text-xs text-muted-foreground">
                        {t(`reasoning.${effort}Desc`)}
                      </span>
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </div>

            <div className="space-y-2">
              <Label>{t("detail.reasoningFallbackBehavior")}</Label>
              <Select
                value={reasoningFallback}
                onValueChange={(value) => setReasoningFallback(normalizeReasoningFallback(value))}
              >
                <SelectTrigger className="w-full sm:w-72">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  {REASONING_FALLBACKS.map((fallback) => (
                    <SelectItem key={fallback} value={fallback}>
                      <span>{t(`reasoning.${fallback}`)}</span>
                      <span className="ml-2 text-xs text-muted-foreground">
                        {t(`reasoning.${fallback}Desc`)}
                      </span>
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </div>
          </div>
        ) : null}
      </div>
    </section>
  );
}
