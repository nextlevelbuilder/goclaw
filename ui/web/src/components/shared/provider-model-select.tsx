import { useMemo, useEffect } from "react";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Combobox } from "@/components/ui/combobox";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { useProviders } from "@/pages/providers/hooks/use-providers";
import { useProviderModels } from "@/pages/providers/hooks/use-provider-models";
import { useProviderVerify } from "@/pages/providers/hooks/use-provider-verify";
import { InfoLabel } from "./info-label";

interface ProviderModelSelectProps {
  provider: string;
  onProviderChange: (v: string) => void;
  model: string;
  onModelChange: (v: string) => void;
  providerTip?: string;
  modelTip?: string;
  providerLabel?: string;
  modelLabel?: string;
  providerPlaceholder?: string;
  modelPlaceholder?: string;
  /** Show a "Check" verify button. When true, always shows. When omitted, auto-shows if savedProvider/savedModel are provided and values differ. */
  showVerify?: boolean;
  /** Saved provider value — when provided, verify button auto-shows on change. */
  savedProvider?: string;
  /** Saved model value — when provided, verify button auto-shows on change. */
  savedModel?: string;
  /** Called when verification status changes. True = save should be blocked (changed but not verified). */
  onSaveBlockedChange?: (blocked: boolean) => void;
}

export function ProviderModelSelect({
  provider,
  onProviderChange,
  model,
  onModelChange,
  providerTip = "LLM provider name. Must match a configured provider.",
  modelTip = "Model ID to use.",
  providerLabel = "Provider",
  modelLabel = "Model",
  providerPlaceholder = "Select provider",
  modelPlaceholder = "Enter or select model",
  showVerify,
  savedProvider,
  savedModel,
  onSaveBlockedChange,
}: ProviderModelSelectProps) {
  const { providers } = useProviders();
  const enabledProviders = providers.filter((p) => p.enabled);

  // Auto-select first enabled provider when none is set
  useEffect(() => {
    if (!provider && enabledProviders.length > 0) {
      onProviderChange(enabledProviders[0]!.name);
    }
  }, [provider, enabledProviders, onProviderChange]);

  const selectedProviderId = useMemo(
    () => enabledProviders.find((p) => p.name === provider)?.id,
    [enabledProviders, provider],
  );
  const { models, loading: modelsLoading } = useProviderModels(selectedProviderId);
  const { verify, verifying, result: verifyResult, reset: resetVerify } = useProviderVerify();

  const hasSavedValues = savedProvider !== undefined && savedModel !== undefined;
  const llmChanged = hasSavedValues && (provider !== savedProvider || model !== savedModel);
  const shouldShowVerify = showVerify ?? llmChanged;

  useEffect(() => {
    resetVerify();
  }, [provider, model, resetVerify]);

  useEffect(() => {
    onSaveBlockedChange?.(!!llmChanged && !verifyResult?.valid);
  }, [llmChanged, verifyResult, onSaveBlockedChange]);

  const handleProviderChange = (v: string) => {
    onProviderChange(v);
    onModelChange("");
  };

  const handleVerify = async () => {
    if (!selectedProviderId || !model.trim()) return;
    await verify(selectedProviderId, model.trim());
  };

  return (
    <div className="grid grid-cols-2 gap-4">
      <div className="grid gap-1.5">
        <InfoLabel tip={providerTip}>{providerLabel}</InfoLabel>
        {enabledProviders.length > 0 ? (
          <Select value={provider} onValueChange={handleProviderChange}>
            <SelectTrigger>
              <SelectValue placeholder={providerPlaceholder} />
            </SelectTrigger>
            <SelectContent>
              {enabledProviders.map((p) => (
                <SelectItem key={p.name} value={p.name}>
                  {p.display_name || p.name}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        ) : (
          <Input
            value={provider}
            onChange={(e) => handleProviderChange(e.target.value)}
            placeholder="No providers configured"
          />
        )}
      </div>
      <div className="grid gap-1.5">
        <InfoLabel tip={modelTip}>{modelLabel}</InfoLabel>
        <div className="flex gap-2">
          <div className="flex-1">
            <Combobox
              value={model}
              onChange={onModelChange}
              options={models.map((m) => ({ value: m.id, label: m.name }))}
              placeholder={modelsLoading ? "Loading models..." : modelPlaceholder}
            />
          </div>
          {shouldShowVerify && (
            <Button
              type="button"
              variant="outline"
              size="sm"
              className="h-9 shrink-0 px-3"
              disabled={!selectedProviderId || !model.trim() || verifying}
              onClick={handleVerify}
            >
              {verifying ? "..." : "Check"}
            </Button>
          )}
        </div>
        {shouldShowVerify && verifyResult && (
          <p className={`text-xs ${verifyResult.valid ? "text-success" : "text-destructive"}`}>
            {verifyResult.valid ? "Model verified" : verifyResult.error || "Verification failed"}
          </p>
        )}
      </div>
    </div>
  );
}
