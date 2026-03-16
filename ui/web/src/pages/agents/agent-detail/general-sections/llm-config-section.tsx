import { useTranslation } from "react-i18next";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { ProviderModelSelect } from "@/components/shared/provider-model-select";

interface LlmConfigSectionProps {
  provider: string;
  onProviderChange: (v: string) => void;
  model: string;
  onModelChange: (v: string) => void;
  contextWindow: number;
  onContextWindowChange: (v: number) => void;
  maxToolIterations: number;
  onMaxToolIterationsChange: (v: number) => void;
  savedProvider: string;
  savedModel: string;
  /** Called when verification status changes. True = save should be blocked. */
  onSaveBlockedChange?: (blocked: boolean) => void;
  stripAssistantPrefill: boolean;
  onStripAssistantPrefillChange: (v: boolean) => void;
}

export function LlmConfigSection({
  provider,
  onProviderChange,
  model,
  onModelChange,
  contextWindow,
  onContextWindowChange,
  maxToolIterations,
  onMaxToolIterationsChange,
  savedProvider,
  savedModel,
  onSaveBlockedChange,
  stripAssistantPrefill,
  onStripAssistantPrefillChange,
}: LlmConfigSectionProps) {
  const { t } = useTranslation("agents");
  return (
    <section className="space-y-4">
      <h3 className="text-sm font-medium text-muted-foreground">{t("llmConfig.title")}</h3>
      <div className="space-y-4 rounded-lg border p-4">
        <ProviderModelSelect
          provider={provider}
          onProviderChange={onProviderChange}
          model={model}
          onModelChange={onModelChange}
          savedProvider={savedProvider}
          savedModel={savedModel}
          onSaveBlockedChange={onSaveBlockedChange}
          providerTip="LLM provider name. Must match a configured provider."
          modelTip="Model ID to use."
        />
        <div className="grid grid-cols-1 gap-4 sm:grid-cols-2">
          <div className="space-y-2">
            <Label htmlFor="contextWindow">{t("llmConfig.contextWindow")}</Label>
            <Input
              id="contextWindow"
              type="number"
              value={contextWindow || ""}
              onChange={(e) => onContextWindowChange(Number(e.target.value) || 0)}
              placeholder="200000"
            />
            <p className="text-xs text-muted-foreground">{t("llmConfig.contextWindowHint")}</p>
          </div>
          <div className="space-y-2">
            <Label htmlFor="maxToolIterations">{t("llmConfig.maxToolIterations")}</Label>
            <Input
              id="maxToolIterations"
              type="number"
              value={maxToolIterations || ""}
              onChange={(e) => onMaxToolIterationsChange(Number(e.target.value) || 0)}
              placeholder="25"
            />
            <p className="text-xs text-muted-foreground">{t("llmConfig.maxToolIterationsHint")}</p>
          </div>
        </div>
        <div className="flex items-center gap-2">
          <input
            type="checkbox"
            id="strip-assistant-prefill"
            checked={stripAssistantPrefill}
            onChange={(e) => onStripAssistantPrefillChange(e.target.checked)}
            className="h-4 w-4 rounded border-border"
          />
          <label htmlFor="strip-assistant-prefill" className="text-sm">{t("llmConfig.stripAssistantPrefill")}</label>
          <span className="text-xs text-muted-foreground">{t("llmConfig.stripAssistantPrefillHint")}</span>
        </div>
      </div>
    </section>
  );
}
