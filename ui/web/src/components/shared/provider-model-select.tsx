import { useMemo, useEffect, useRef } from "react";
import { useTranslation } from "react-i18next";
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
  /** When true, skip auto-selecting the first provider when none is set. Useful when empty means "use default". */
  allowEmpty?: boolean;
}

export function ProviderModelSelect({
  provider,
  onProviderChange,
  model,
  onModelChange,
  providerTip,
  modelTip,
  providerLabel,
  modelLabel,
  providerPlaceholder,
  modelPlaceholder,
  showVerify,
  savedProvider,
  savedModel,
  onSaveBlockedChange,
  allowEmpty,
}: ProviderModelSelectProps) {
  const { t } = useTranslation("common");
  const { providers } = useProviders();
  const enabledProviders = useMemo(
    () => providers.filter((p) => p.enabled),
    [providers],
  );

  // Stable ref for callback — prevents the auto-select effect from re-running
  // on every parent render (inline onProviderChange creates a new ref each time).
  const onProviderChangeRef = useRef(onProviderChange);
  onProviderChangeRef.current = onProviderChange;

  // Auto-select first enabled provider when none is set (unless allowEmpty).
  // Uses ref for callback so this only re-runs when provider or providers actually change.
  useEffect(() => {
    if (!allowEmpty && !provider && enabledProviders.length > 0) {
      onProviderChangeRef.current(enabledProviders[0]!.name);
    }
  }, [allowEmpty, provider, enabledProviders]);

  const selectedProvider = useMemo(
    () => enabledProviders.find((p) => p.name === provider),
    [enabledProviders, provider],
  );
  const selectedProviderId = selectedProvider?.id;
  const { models, loading: modelsLoading } = useProviderModels(selectedProviderId, selectedProvider?.provider_type);
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
    // Only clear model when NOT in allowEmpty mode.
    // In allowEmpty mode (embedding config), both callbacks update the same
    // parent state object — calling onModelChange("") with a stale closure
    // would overwrite the provider change we just made.
    if (!allowEmpty) {
      onModelChange("");
    }
  };

  const handleVerify = async () => {
    if (!selectedProviderId || !model.trim()) return;
    await verify(selectedProviderId, model.trim());
  };

  return (
    <div className="grid grid-cols-1 gap-4 sm:grid-cols-2">
      <div className="grid gap-1.5">
        <InfoLabel tip={providerTip ?? t("providerTip")}>{providerLabel ?? t("provider")}</InfoLabel>
        {enabledProviders.length > 0 ? (
          <Select value={provider || "__empty__"} onValueChange={(v) => handleProviderChange(v === "__empty__" ? "" : v)}>
            <SelectTrigger>
              <SelectValue placeholder={providerPlaceholder ?? t("selectProvider")} />
            </SelectTrigger>
            <SelectContent>
              {allowEmpty && (
                <SelectItem value="__empty__">{providerPlaceholder || "(auto)"}</SelectItem>
              )}
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
            placeholder={t("noProvidersConfigured")}
          />
        )}
      </div>
      <div className="grid gap-1.5">
        <InfoLabel tip={modelTip ?? t("modelTip")}>{modelLabel ?? t("model")}</InfoLabel>
        <div className="flex gap-2">
          <div className="flex-1">
            <Combobox
              value={model}
              onChange={onModelChange}
              options={models.map((m) => ({ value: m.id, label: m.name }))}
              placeholder={modelsLoading ? t("loadingModels") : (modelPlaceholder ?? t("enterOrSelectModel"))}
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
              {verifying ? "..." : t("check")}
            </Button>
          )}
        </div>
        {shouldShowVerify && verifyResult && (
          <p className={`text-xs ${verifyResult.valid ? "text-success" : "text-destructive"}`}>
            {verifyResult.valid ? t("modelVerified") : verifyResult.error || t("verificationFailed")}
          </p>
        )}
      </div>
    </div>
  );
}
