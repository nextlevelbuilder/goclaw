import { useState, useEffect } from "react";
import { useTranslation } from "react-i18next";
import { Save, Settings, Loader2 } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Switch } from "@/components/ui/switch";
import { InfoLabel } from "@/components/shared/info-label";
import { ProviderModelSelect } from "@/components/shared/provider-model-select";
import {
  Dialog, DialogContent, DialogHeader, DialogTitle,
} from "@/components/ui/dialog";
import { ConfigGroupHeader } from "@/components/shared/config-group-header";
import type {
	  AgentData, ChatGPTOAuthRoutingConfig, CompactionConfig, ContextPruningConfig,
  DeliveryBehaviorConfig, ModelFallbackConfig, ReasoningOverrideMode,
  SandboxConfig, WorkspaceSharingConfig,
} from "@/types/agent";
import {
  ChatGPTOAuthRoutingSection, ThinkingSection, WorkspaceSharingSection, CompactionSection,
  ContextPruningSection, InboundDebounceSection, ModelFallbackSection, SandboxSection,
} from "./config-sections";
import { WorkspaceSection } from "./general-sections";
import { useProviders } from "@/pages/providers/hooks/use-providers";
import { useProviderModels } from "@/pages/providers/hooks/use-provider-models";
import {
  getChatGPTOAuthProviderRouting,
  getProviderReasoningDefaults,
  deriveLegacyThinkingLevel,
} from "@/types/provider";
import { deriveState, buildAdvancedUpdatePayload } from "./agent-advanced-state-utils";

interface AgentAdvancedDialogProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  agent: AgentData;
  onUpdate: (updates: Record<string, unknown>) => Promise<void>;
}

export function AgentAdvancedDialog({ open, onOpenChange, agent, onUpdate }: AgentAdvancedDialogProps) {
  const { t } = useTranslation("agents");
  const { providers, loading: providersLoading, refresh: refreshProviders } = useProviders();
  const providerByName = new Map(providers.map((provider) => [provider.name, provider]));
  const currentProvider = providerByName.get(agent.provider);
  const { models: providerModels, loading: providerModelsLoading } = useProviderModels(
    currentProvider?.id,
  );
  const providerRoutingDefaults = getChatGPTOAuthProviderRouting(currentProvider?.settings);
  const providerReasoningDefaults = getProviderReasoningDefaults(currentProvider?.settings);
  const currentModelCapability = providerModels.find(
    (entry) => entry.id === agent.model || agent.model.endsWith(`/${entry.id}`),
  )?.reasoning ?? null;
  const expertReasoningAvailable = Boolean(currentModelCapability?.levels?.length);

  const init = deriveState(agent, currentProvider);
  const [wsSharing, setWsSharing] = useState<WorkspaceSharingConfig>(init.wsSharing);
  const [reasoningMode, setReasoningMode] = useState<ReasoningOverrideMode>(init.reasoningMode);
  const [thinkingLevel, setThinkingLevel] = useState(init.thinkingLevel);
  const [reasoningEffort, setReasoningEffort] = useState(init.reasoningEffort);
  const [reasoningFallback, setReasoningFallback] = useState<string>(init.reasoningFallback);
  const [reasoningExpert, setReasoningExpert] = useState(init.reasoningExpert);
  const [chatgptRouting, setChatgptRouting] = useState<ChatGPTOAuthRoutingConfig>(init.chatgptRouting);
  const [modelFallback, setModelFallback] = useState<ModelFallbackConfig>(init.modelFallback);
  const [comp, setComp] = useState<CompactionConfig>(init.comp);
  const [deliveryBehaviorMode, setDeliveryBehaviorMode] = useState(init.deliveryBehaviorMode);
  const [deliveryBehavior, setDeliveryBehavior] = useState<DeliveryBehaviorConfig>(init.deliveryBehavior);
  const [inboundDebounceMode, setInboundDebounceMode] = useState(init.inboundDebounceMode);
  const [inboundDebounceMs, setInboundDebounceMs] = useState(init.inboundDebounceMs);
  const [pruneEnabled, setPruneEnabled] = useState(init.pruneEnabled);
  const [prune, setPrune] = useState<ContextPruningConfig>(init.prune);
  const [sbEnabled, setSbEnabled] = useState(init.sbEnabled);
  const [sb, setSb] = useState<SandboxConfig>(init.sb);

  // Re-sync local state when dialog opens (picks up latest agent data from React Query)
  useEffect(() => {
    if (!open) return;
    refreshProviders();
    const s = deriveState(agent, currentProvider);
    setReasoningMode(s.reasoningMode);
    setThinkingLevel(s.thinkingLevel);
    setReasoningEffort(s.reasoningEffort);
    setReasoningFallback(s.reasoningFallback);
    setReasoningExpert(s.reasoningExpert);
    setChatgptRouting(s.chatgptRouting);
    setModelFallback(s.modelFallback);
    setWsSharing(s.wsSharing);
    setComp(s.comp);
    setDeliveryBehaviorMode(s.deliveryBehaviorMode);
    setDeliveryBehavior(s.deliveryBehavior);
    setInboundDebounceMode(s.inboundDebounceMode);
    setInboundDebounceMs(s.inboundDebounceMs);
    setPruneEnabled(s.pruneEnabled);
    setPrune(s.prune);
    setSbEnabled(s.sbEnabled);
    setSb(s.sb);
   
  }, [open]);

  useEffect(() => {
    if (!open || !currentProvider || providerModelsLoading) return;
    if (!expertReasoningAvailable) {
      if (reasoningExpert) setReasoningExpert(false);
      if (reasoningFallback !== "downgrade") setReasoningFallback("downgrade");
      return;
    }
    const allowedEfforts = new Set(["off", "auto", ...(currentModelCapability?.levels ?? [])]);
    if (!allowedEfforts.has(reasoningEffort)) {
      const fallbackEffort = allowedEfforts.has(thinkingLevel)
        ? thinkingLevel
        : currentModelCapability?.default_effort ?? "off";
      setReasoningEffort(fallbackEffort);
    }
  }, [
    currentModelCapability,
    currentProvider,
    expertReasoningAvailable,
    open,
    providerModelsLoading,
    reasoningEffort,
    reasoningExpert,
    reasoningFallback,
    thinkingLevel,
  ]);

  const [saving, setSaving] = useState(false);

  const handleSave = async () => {
    setSaving(true);
    try {
      const updates = buildAdvancedUpdatePayload({
        agent,
        currentProvider,
        providersLoading,
        providerModelsLoading,
        expertReasoningAvailable,
        reasoningMode,
        reasoningEffort,
        reasoningExpert,
        reasoningFallback,
        thinkingLevel,
        chatgptRouting,
        modelFallback,
        wsSharing,
        comp,
        deliveryBehaviorMode,
        deliveryBehavior,
        inboundDebounceMode,
        inboundDebounceMs,
        pruneEnabled,
        prune,
        sbEnabled,
        sb,
      });
      await onUpdate(updates);
      onOpenChange(false);
    } catch {
      // toast shown by hook — keep dialog open
    } finally {
      setSaving(false);
    }
  };

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-h-[90vh] w-[95vw] flex flex-col sm:max-w-3xl">
        <DialogHeader>
          <DialogTitle className="flex items-center gap-2">
            <Settings className="h-4 w-4" />
            {t("detail.advanced")}
          </DialogTitle>
        </DialogHeader>

        {/* Scrollable body */}
        <div className="overflow-y-auto min-h-0 -mx-4 px-4 sm:-mx-6 sm:px-6 space-y-4">
          {/* Workspace (read-only) */}
          <WorkspaceSection workspace={agent.workspace} />

          {/* Workspace Sharing */}
          <WorkspaceSharingSection value={wsSharing} onChange={setWsSharing} />

          {/* Thinking */}
          <ThinkingSection
            reasoningMode={reasoningMode}
            thinkingLevel={thinkingLevel}
            reasoningEffort={reasoningEffort}
            reasoningFallback={reasoningFallback}
            expertMode={reasoningExpert}
            model={agent.model}
            capability={currentModelCapability}
            providerDefault={providerReasoningDefaults}
            providerLabel={currentProvider?.display_name || agent.provider}
            capabilityLoading={providersLoading || providerModelsLoading}
            onReasoningModeChange={(mode) => {
              setReasoningMode(mode);
              if (mode === "inherit") {
                setReasoningExpert(false);
                setReasoningFallback(providerReasoningDefaults?.fallback ?? "downgrade");
                setReasoningEffort(providerReasoningDefaults?.effort ?? "off");
                setThinkingLevel(
                  deriveLegacyThinkingLevel(providerReasoningDefaults?.effort ?? "off"),
                );
              }
            }}
            onThinkingLevelChange={(value) => {
              setThinkingLevel(value);
              setReasoningEffort(value);
            }}
            onReasoningEffortChange={setReasoningEffort}
            onReasoningFallbackChange={setReasoningFallback}
            onExpertModeChange={(enabled) => {
              setReasoningExpert(enabled);
              if (!enabled) {
                const legacy = deriveLegacyThinkingLevel(reasoningEffort);
                setThinkingLevel(legacy);
                setReasoningFallback("downgrade");
              } else if (reasoningEffort === "off" && thinkingLevel !== "off") {
                setReasoningEffort(thinkingLevel);
              }
            }}
          />

          <ChatGPTOAuthRoutingSection
            currentProvider={agent.provider}
            providers={providers}
            value={chatgptRouting}
            onChange={setChatgptRouting}
            defaultRouting={providerRoutingDefaults}
            membershipEditable={false}
            membershipManagedByLabel={
              currentProvider?.display_name || agent.provider
            }
          />

          <ModelFallbackSection
            primaryProvider={agent.provider}
            primaryModel={agent.model}
            providers={providers}
            value={modelFallback}
            onChange={setModelFallback}
          />

          <DeliveryBehaviorSection
            value={deliveryBehavior}
            mode={deliveryBehaviorMode}
            onModeChange={setDeliveryBehaviorMode}
            onChange={setDeliveryBehavior}
          />

          {/* Performance */}
          <ConfigGroupHeader
            title={t("configGroups.performance")}
            description={t("configGroups.performanceDesc")}
          />
          <div className="space-y-4">
            <CompactionSection value={comp} onChange={setComp} />
            <InboundDebounceSection
              mode={inboundDebounceMode}
              debounceMs={inboundDebounceMs}
              onModeChange={setInboundDebounceMode}
              onDebounceMsChange={setInboundDebounceMs}
            />
            <ContextPruningSection
              enabled={pruneEnabled}
              value={prune}
              onToggle={(v) => { setPruneEnabled(v); if (!v) setPrune({}); }}
              onChange={setPrune}
            />
            <SandboxSection
              enabled={sbEnabled}
              value={sb}
              onToggle={(v) => { setSbEnabled(v); if (!v) setSb({}); }}
              onChange={setSb}
            />
          </div>
        </div>

        {/* Footer */}
        <div className="flex items-center justify-end gap-2 pt-4 border-t shrink-0">
          <Button variant="outline" onClick={() => onOpenChange(false)} disabled={saving}>
            {t("create.cancel")}
          </Button>
          <Button onClick={handleSave} disabled={saving}>
            {saving ? <Loader2 className="h-4 w-4 animate-spin" /> : <Save className="h-4 w-4" />}
            {saving ? t("config.saving") : t("config.saveConfig")}
          </Button>
        </div>
      </DialogContent>
    </Dialog>
  );
}

function DeliveryBehaviorSection({
  value,
  mode,
  onModeChange,
  onChange,
}: {
  value: DeliveryBehaviorConfig;
  mode: "inherit" | "custom";
  onModeChange: (mode: "inherit" | "custom") => void;
  onChange: (value: DeliveryBehaviorConfig) => void;
}) {
  const { t } = useTranslation("agents");
  const disabled = mode === "inherit";
  const patch = (updates: DeliveryBehaviorConfig) => onChange({ ...value, ...updates });
  const patchQuick = (updates: NonNullable<DeliveryBehaviorConfig["quick_ack"]>) =>
    patch({ quick_ack: { ...(value.quick_ack ?? {}), ...updates } });
  const patchIntermediate = (updates: NonNullable<DeliveryBehaviorConfig["intermediate_replies"]>) =>
    patch({ intermediate_replies: { ...(value.intermediate_replies ?? {}), ...updates } });

  return (
    <div className="space-y-3 rounded-md border p-4">
      <div className="flex items-start justify-between gap-4">
        <div>
          <InfoLabel tip={t("delivery.modeTip")} labelClassName="text-sm font-medium">
            {t("delivery.title")}
          </InfoLabel>
          <p className="text-xs text-muted-foreground">{t("delivery.description")}</p>
        </div>
        <Select value={mode} onValueChange={(next) => onModeChange(next as "inherit" | "custom")}>
          <SelectTrigger className="w-32 text-base md:text-sm">
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value="inherit">{t("delivery.inherit")}</SelectItem>
            <SelectItem value="custom">{t("delivery.custom")}</SelectItem>
          </SelectContent>
        </Select>
      </div>
      <div className="grid gap-4 md:grid-cols-2">
        <div className="space-y-3">
          <div className="flex items-center justify-between gap-3">
            <InfoLabel tip={t("delivery.quickAckTip")}>{t("delivery.quickAck")}</InfoLabel>
            <Switch checked={value.quick_ack?.enabled ?? true} disabled={disabled} onCheckedChange={(enabled) => patchQuick({ enabled })} />
          </div>
          <DeliveryProviderFields
            disabled={disabled}
            provider={value.quick_ack?.provider ?? ""}
            model={value.quick_ack?.model ?? ""}
            providerTip={t("delivery.quickAckProviderTip")}
            modelTip={t("delivery.quickAckModelTip")}
            onProvider={(provider) => patchQuick({ provider, model: "" })}
            onModel={(model) => patchQuick({ model })}
          />
          <NumberField label={t("delivery.quickAckDelay")} tip={t("delivery.quickAckDelayTip")} value={value.quick_ack?.min_delay_ms ?? 1000} disabled={disabled} onChange={(min_delay_ms) => patchQuick({ min_delay_ms })} />
          <NumberField label={t("delivery.timeoutMs")} tip={t("delivery.quickAckTimeoutTip")} value={value.quick_ack?.timeout_ms ?? 2500} disabled={disabled} onChange={(timeout_ms) => patchQuick({ timeout_ms })} />
        </div>
        <div className="space-y-3">
          <div className="flex items-center justify-between gap-3">
            <InfoLabel tip={t("delivery.intermediateTip")}>{t("delivery.intermediate")}</InfoLabel>
            <Switch checked={value.intermediate_replies?.enabled ?? false} disabled={disabled} onCheckedChange={(enabled) => patchIntermediate({ enabled })} />
          </div>
          <DeliveryProviderFields
            disabled={disabled}
            provider={value.intermediate_replies?.provider ?? ""}
            model={value.intermediate_replies?.model ?? ""}
            providerTip={t("delivery.intermediateProviderTip")}
            modelTip={t("delivery.intermediateModelTip")}
            onProvider={(provider) => patchIntermediate({ provider, model: "" })}
            onModel={(model) => patchIntermediate({ model })}
          />
          <NumberField label={t("delivery.timeoutMs")} tip={t("delivery.intermediateTimeoutTip")} value={value.intermediate_replies?.timeout_ms ?? 2500} disabled={disabled} onChange={(timeout_ms) => patchIntermediate({ timeout_ms })} />
        </div>
      </div>
    </div>
  );
}

function DeliveryProviderFields({
  disabled,
  provider,
  model,
  providerTip,
  modelTip,
  onProvider,
  onModel,
}: {
  disabled: boolean;
  provider: string;
  model: string;
  providerTip: string;
  modelTip: string;
  onProvider: (value: string) => void;
  onModel: (value: string) => void;
}) {
  const { t } = useTranslation("agents");
  return (
    <ProviderModelSelect
      provider={provider}
      onProviderChange={onProvider}
      model={model}
      onModelChange={onModel}
      disabled={disabled}
      allowEmpty
      providerLabel={t("delivery.provider")}
      modelLabel={t("delivery.model")}
      providerTip={providerTip}
      modelTip={modelTip}
      providerPlaceholder={t("delivery.providerPlaceholder")}
      modelPlaceholder={t("delivery.modelPlaceholder")}
    />
  );
}

function NumberField({ label, tip, value, disabled, onChange }: { label: string; tip: string; value: number; disabled: boolean; onChange: (value: number) => void }) {
  return (
    <div className="grid gap-1.5">
      <InfoLabel tip={tip}>{label}</InfoLabel>
      <Input className="text-base md:text-sm" type="number" min={0} value={value} disabled={disabled} onChange={(e) => onChange(Number(e.target.value))} />
    </div>
  );
}
