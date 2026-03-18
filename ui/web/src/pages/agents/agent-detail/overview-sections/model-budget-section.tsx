import { useCallback } from "react";
import { useTranslation } from "react-i18next";
import { DollarSign } from "lucide-react";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { ProviderModelSelect } from "@/components/shared/provider-model-select";

interface ModelBudgetSectionProps {
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
  budgetDollars: string;
  onBudgetDollarsChange: (v: string) => void;
  onSaveBlockedChange?: (blocked: boolean) => void;
}

export function ModelBudgetSection({
  provider, onProviderChange, model, onModelChange,
  contextWindow, onContextWindowChange,
  maxToolIterations, onMaxToolIterationsChange,
  savedProvider, savedModel,
  budgetDollars, onBudgetDollarsChange,
  onSaveBlockedChange,
}: ModelBudgetSectionProps) {
  const { t } = useTranslation("agents");

  const handleSaveBlockedChange = useCallback((blocked: boolean) => {
    onSaveBlockedChange?.(blocked);
  }, [onSaveBlockedChange]);

  return (
    <section className="space-y-3 rounded-lg border p-3 sm:p-4 overflow-hidden">
      <h3 className="text-sm font-medium">{t("detail.modelBudget")}</h3>

      <ProviderModelSelect
        provider={provider}
        onProviderChange={onProviderChange}
        model={model}
        onModelChange={onModelChange}
        savedProvider={savedProvider}
        savedModel={savedModel}
        onSaveBlockedChange={handleSaveBlockedChange}
        providerTip="LLM provider name. Must match a configured provider."
        modelTip="Model ID to use."
      />

      <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
        <div className="space-y-1.5">
          <Label htmlFor="contextWindow" className="text-xs">{t("llmConfig.contextWindow")}</Label>
          <Input
            id="contextWindow"
            type="number"
            value={contextWindow || ""}
            onChange={(e) => onContextWindowChange(Number(e.target.value) || 0)}
            placeholder="200000"
            className="text-base md:text-sm"
          />
          <p className="text-xs text-muted-foreground">{t("llmConfig.contextWindowHint")}</p>
        </div>
        <div className="space-y-1.5">
          <Label htmlFor="maxToolIterations" className="text-xs">{t("llmConfig.maxToolIterations")}</Label>
          <Input
            id="maxToolIterations"
            type="number"
            value={maxToolIterations || ""}
            onChange={(e) => onMaxToolIterationsChange(Number(e.target.value) || 0)}
            placeholder="25"
            className="text-base md:text-sm"
          />
          <p className="text-xs text-muted-foreground">{t("llmConfig.maxToolIterationsHint")}</p>
        </div>
      </div>

      <div className="space-y-1.5">
        <Label htmlFor="budget" className="text-xs">
          <span className="flex items-center gap-1">
            <DollarSign className="h-3 w-3 text-emerald-500" />
            {t("general.budgetLabel")}
          </span>
        </Label>
        <div className="flex items-center gap-2">
          <span className="text-sm text-muted-foreground">$</span>
          <Input
            id="budget"
            type="number"
            min="0"
            step="0.01"
            placeholder="0.00"
            value={budgetDollars}
            onChange={(e) => onBudgetDollarsChange(e.target.value)}
            className="max-w-[200px] text-base md:text-sm"
          />
        </div>
        <p className="text-xs text-muted-foreground">{t("general.budgetHint")}</p>
      </div>
    </section>
  );
}
