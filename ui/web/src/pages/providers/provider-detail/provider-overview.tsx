import { useEffect, useMemo, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import { Copy } from "lucide-react";
import { StickySaveBar } from "@/components/shared/sticky-save-bar";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Switch } from "@/components/ui/switch";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { PROVIDER_TYPES } from "@/constants/providers";
import { toast } from "@/stores/use-toast-store";
import { useProviders } from "../hooks/use-providers";
import { useProviderModels } from "../hooks/use-provider-models";
import { useProviderVerify } from "../hooks/use-provider-verify";
import { ProviderOAuthAccountSection } from "./provider-oauth-account-section";
import { ProviderReasoningSection } from "./provider-reasoning-section";
import { ProviderEmbeddingSection } from "./provider-embedding-section";
import { ProviderPoolActivitySection } from "./provider-pool-activity-section";
import {
  buildProviderSettingsWithChatGPTOAuthRouting,
  buildProviderSettingsWithReasoningDefaults,
  getChatGPTOAuthProviderRouting,
  getEmbeddingSettings,
  getProviderReasoningDefaults,
  deriveLegacyThinkingLevel,
} from "@/types/provider";
import type { ProviderData, ProviderInput } from "@/types/provider";
import type { ChatGPTOAuthRoutingConfig } from "@/types/agent";
import { useChatGPTOAuthProviderStatuses } from "../hooks/use-chatgpt-oauth-provider-statuses";
import { useChatGPTOAuthProviderQuotas } from "../hooks/use-chatgpt-oauth-provider-quotas";
import { ChatGPTOAuthRoutingSection } from "@/pages/agents/agent-detail/config-sections";
import { useProviderCodexPoolActivity } from "../hooks/use-provider-codex-pool-activity";
import { toPoolEntries } from "@/adapters/provider-pool.adapter";
import {
  NO_API_KEY_TYPES,
  NO_EMBEDDING_TYPES,
  SIMPLE_REASONING_LEVELS,
  providerStatus,
  routingSignature,
  comparableAPIKeyValue,
  providerFormSignature,
  reasoningSignature,
} from "./provider-overview-helpers";

interface ProviderOverviewProps {
  provider: ProviderData;
  onUpdate: (id: string, data: ProviderInput) => Promise<void>;
}

export function ProviderOverview({ provider, onUpdate }: ProviderOverviewProps) {
  const { t } = useTranslation("providers");
  const { t: tc } = useTranslation("common");
  const { providers } = useProviders();
  const { models: providerModels, reasoningDefaults: providerReasoningDefaults } = useProviderModels(provider.id);
  const { statuses } = useChatGPTOAuthProviderStatuses(providers);

  const typeInfo = PROVIDER_TYPES.find((pt) => pt.value === provider.provider_type);
  const typeLabel = typeInfo?.label ?? provider.provider_type;
  const showApiKey = !NO_API_KEY_TYPES.has(provider.provider_type);
  const showEmbedding = !NO_EMBEDDING_TYPES.has(provider.provider_type);
  const isOAuth = provider.provider_type === "chatgpt_oauth";

  // --- Pool ownership & status maps ---
  const providerByName = useMemo(() => new Map(providers.map((item) => [item.name, item])), [providers]);
  const poolOwnership = useMemo(() => {
    const membersByOwner = new Map<string, string[]>();
    const ownerByMember = new Map<string, string>();
    for (const item of providers) {
      if (item.provider_type !== "chatgpt_oauth") continue;
      const routing = getChatGPTOAuthProviderRouting(item.settings);
      if (!routing || routing.extraProviderNames.length === 0) continue;
      membersByOwner.set(item.name, routing.extraProviderNames);
      for (const memberName of routing.extraProviderNames) {
        if (!ownerByMember.has(memberName)) ownerByMember.set(memberName, item.name);
      }
    }
    return { membersByOwner, ownerByMember };
  }, [providers]);
  const statusByName = useMemo(() => new Map(statuses.map((s) => [s.provider.name, s])), [statuses]);
  const managedByOwnerName = isOAuth ? poolOwnership.ownerByMember.get(provider.name) : undefined;
  const managedByProvider = managedByOwnerName ? providerByName.get(managedByOwnerName) : undefined;
  const managedMemberCount = isOAuth ? poolOwnership.membersByOwner.get(provider.name)?.length ?? 0 : 0;
  const canEditPoolRouting = isOAuth && !managedByOwnerName;
  const currentOAuthAvailability = providerStatus(provider.name, statusByName, provider.enabled);

  // --- Form state ---
  const initialRouting = getChatGPTOAuthProviderRouting(provider.settings);
  const initialReasoningDefaults = getProviderReasoningDefaults(provider.settings) ?? providerReasoningDefaults ?? null;
  const initialReasoningEffort = initialReasoningDefaults?.effort ?? "off";
  const initialReasoningFallback = initialReasoningDefaults?.fallback ?? "downgrade";

  const [displayName, setDisplayName] = useState(provider.display_name || "");
  const [apiKey, setApiKey] = useState(provider.api_key || "");
  const [enabled, setEnabled] = useState(provider.enabled);
  const [poolRouting, setPoolRouting] = useState<ChatGPTOAuthRoutingConfig>({
    strategy: initialRouting?.strategy ?? "primary_first",
    extra_provider_names: initialRouting?.extraProviderNames ?? [],
  });
  const initEmb = getEmbeddingSettings(provider.settings);
  const [embEnabled, setEmbEnabled] = useState(initEmb?.enabled ?? false);
  const [embModel, setEmbModel] = useState(initEmb?.model ?? "");
  const [embApiBase, setEmbApiBase] = useState(initEmb?.api_base ?? "");
  const [reasoningThinkingLevel, setReasoningThinkingLevel] = useState(deriveLegacyThinkingLevel(initialReasoningEffort));
  const [reasoningEffort, setReasoningEffort] = useState(initialReasoningEffort);
  const [reasoningFallback, setReasoningFallback] = useState(initialReasoningFallback);
  const [reasoningExpert, setReasoningExpert] = useState(
    Boolean(initialReasoningDefaults) && (!SIMPLE_REASONING_LEVELS.has(initialReasoningEffort) || initialReasoningFallback !== "downgrade"),
  );
  const [reasoningPreviewModel, setReasoningPreviewModel] = useState("");
  const syncedProviderIDRef = useRef(provider.id);

  // --- Reasoning model preview ---
  const reasoningCapableModels = useMemo(() => providerModels.filter((m) => (m.reasoning?.levels?.length ?? 0) > 0), [providerModels]);
  const reasoningPreviewEntry = useMemo(
    () => reasoningCapableModels.find((m) => m.id === reasoningPreviewModel) ?? reasoningCapableModels[0] ?? null,
    [reasoningCapableModels, reasoningPreviewModel],
  );
  const reasoningPreviewCapability = reasoningPreviewEntry?.reasoning ?? null;
  const showReasoningDefaults = Boolean(initialReasoningDefaults) || reasoningCapableModels.length > 0;

  // --- Dirty checking ---
  const savedReasoningSig = useMemo(() => reasoningSignature(initialReasoningDefaults?.effort ?? "off", initialReasoningDefaults?.fallback ?? "downgrade"), [initialReasoningDefaults?.effort, initialReasoningDefaults?.fallback]);
  const draftReasoningSig = useMemo(() => reasoningSignature(reasoningExpert ? reasoningEffort : reasoningThinkingLevel, reasoningExpert ? reasoningFallback : "downgrade"), [reasoningEffort, reasoningExpert, reasoningFallback, reasoningThinkingLevel]);
  const savedFormSig = useMemo(
    () => providerFormSignature({ displayName: provider.display_name || "", apiKey: provider.api_key || "", savedAPIKey: provider.api_key || "", showApiKey, enabled: provider.enabled, embEnabled: initEmb?.enabled ?? false, embModel: initEmb?.model ?? "", embApiBase: initEmb?.api_base ?? "", routing: { strategy: initialRouting?.strategy ?? "primary_first", extra_provider_names: initialRouting?.extraProviderNames ?? [] }, reasoningEffort: initialReasoningDefaults?.effort ?? "off", reasoningFallback: initialReasoningDefaults?.fallback ?? "downgrade", isOAuth }),
    [initEmb?.api_base, initEmb?.enabled, initEmb?.model, initialReasoningDefaults?.effort, initialReasoningDefaults?.fallback, initialRouting?.extraProviderNames, initialRouting?.strategy, isOAuth, provider.api_key, provider.display_name, provider.enabled, showApiKey],
  );
  const savedFormSigRef = useRef(savedFormSig);
  const draftFormSig = useMemo(
    () => providerFormSignature({ displayName, apiKey, savedAPIKey: provider.api_key || "", showApiKey, enabled, embEnabled, embModel, embApiBase, routing: poolRouting, reasoningEffort: reasoningExpert ? reasoningEffort : reasoningThinkingLevel, reasoningFallback: reasoningExpert ? reasoningFallback : "downgrade", isOAuth }),
    [apiKey, displayName, embApiBase, embEnabled, embModel, enabled, isOAuth, poolRouting, provider.api_key, reasoningEffort, reasoningExpert, reasoningFallback, reasoningThinkingLevel, showApiKey],
  );

  // --- Sync from provider on external update ---
  useEffect(() => {
    const nextID = provider.id;
    const es = getEmbeddingSettings(provider.settings);
    const routing = getChatGPTOAuthProviderRouting(provider.settings);
    const reasoning = getProviderReasoningDefaults(provider.settings) ?? providerReasoningDefaults;
    const syncFromProvider = () => {
      setEmbEnabled(es?.enabled ?? false); setEmbModel(es?.model ?? ""); setEmbApiBase(es?.api_base ?? "");
      setPoolRouting({ strategy: routing?.strategy ?? "primary_first", extra_provider_names: routing?.extraProviderNames ?? [] });
      const nextEffort = reasoning?.effort ?? "off"; const nextFallback = reasoning?.fallback ?? "downgrade";
      setReasoningEffort(nextEffort); setReasoningFallback(nextFallback);
      setReasoningThinkingLevel(deriveLegacyThinkingLevel(nextEffort));
      setReasoningExpert(!SIMPLE_REASONING_LEVELS.has(nextEffort) || nextFallback !== "downgrade");
      setDisplayName(provider.display_name || ""); setApiKey(provider.api_key || ""); setEnabled(provider.enabled);
    };
    if (nextID !== syncedProviderIDRef.current) { syncedProviderIDRef.current = nextID; savedFormSigRef.current = savedFormSig; syncFromProvider(); return; }
    const prev = savedFormSigRef.current;
    if (savedFormSig === prev) return;
    if (draftFormSig === prev) syncFromProvider();
    savedFormSigRef.current = savedFormSig;
  }, [draftFormSig, provider.api_key, provider.display_name, provider.enabled, provider.id, provider.settings, providerReasoningDefaults, savedFormSig]);

  useEffect(() => {
    if (reasoningCapableModels.length === 0) { if (reasoningPreviewModel !== "") setReasoningPreviewModel(""); return; }
    if (reasoningCapableModels.some((m) => m.id === reasoningPreviewModel)) return;
    setReasoningPreviewModel(reasoningCapableModels[0]?.id ?? "");
  }, [reasoningCapableModels, reasoningPreviewModel]);

  // --- Pool entries & quota ---
  const selectedPoolProviderNames = useMemo(
    () => canEditPoolRouting ? Array.from(new Set([provider.name, ...(poolRouting.extra_provider_names ?? [])].filter(Boolean).filter((n) => n === provider.name || providerByName.has(n)))) : [provider.name],
    [canEditPoolRouting, poolRouting.extra_provider_names, provider.name, providerByName],
  );
  const quotaProviderNames = useMemo(() => {
    if (!isOAuth) return [];
    return Array.from(new Set(selectedPoolProviderNames.filter((n) => { if (!n) return false; const item = providerByName.get(n); return providerStatus(n, statusByName, item?.enabled) === "ready"; })));
  }, [isOAuth, providerByName, selectedPoolProviderNames, statusByName]);
  const { quotaByName, isLoading: quotasLoading, isFetching: quotasFetching } = useChatGPTOAuthProviderQuotas(quotaProviderNames, isOAuth);
  const poolEntries = useMemo(() => {
    if (!canEditPoolRouting) return [];
    return toPoolEntries(selectedPoolProviderNames, provider.name, providerByName, statusByName, quotaByName);
  }, [canEditPoolRouting, provider.name, providerByName, quotaByName, selectedPoolProviderNames, statusByName]);
  const isPoolOwner = canEditPoolRouting && selectedPoolProviderNames.length > 1;
  const { data: poolActivity, isFetching: poolActivityFetching, refetch: refreshPoolActivity } = useProviderCodexPoolActivity(provider.id, 8, isPoolOwner);

  // --- Embedding verify ---
  const { verifyEmbedding, embVerifying, embResult, resetEmb } = useProviderVerify();
  useEffect(() => { resetEmb(); }, [embModel, resetEmb]);

  // --- Save handler ---
  const [saving, setSaving] = useState(false);
  const handleSave = async () => {
    setSaving(true);
    try {
      const nextDisplayName = displayName.trim();
      const submittedAPIKey = showApiKey && apiKey && apiKey !== "***" ? apiKey : "";
      const data: ProviderInput = { name: provider.name, display_name: nextDisplayName || undefined, provider_type: provider.provider_type, enabled };
      if (submittedAPIKey) data.api_key = submittedAPIKey;
      let nextSettings = { ...((provider.settings || {}) as Record<string, unknown>) };
      if (showEmbedding) {
        nextSettings = { ...nextSettings, embedding: embEnabled ? { enabled: true, model: embModel.trim() || undefined, api_base: embApiBase.trim() || undefined } : { enabled: false } };
      }
      if (isOAuth) nextSettings = buildProviderSettingsWithChatGPTOAuthRouting(nextSettings, poolRouting);
      nextSettings = buildProviderSettingsWithReasoningDefaults(nextSettings, showReasoningDefaults ? { effort: reasoningExpert ? reasoningEffort : reasoningThinkingLevel, fallback: reasoningExpert ? reasoningFallback : "downgrade" } : null);
      data.settings = nextSettings;
      await onUpdate(provider.id, data);
      setDisplayName(nextDisplayName); setEmbModel(embModel.trim()); setEmbApiBase(embApiBase.trim());
      if (submittedAPIKey) setApiKey("***");
    } catch { /* toast shown by hook */ } finally { setSaving(false); }
  };

  const isDirty = displayName !== (provider.display_name || "")
    || enabled !== provider.enabled
    || (showApiKey && comparableAPIKeyValue(apiKey, provider.api_key || "", showApiKey) !== "")
    || embEnabled !== (initEmb?.enabled ?? false) || embModel !== (initEmb?.model ?? "") || embApiBase !== (initEmb?.api_base ?? "")
    || (isOAuth && routingSignature(poolRouting) !== routingSignature({ strategy: initialRouting?.strategy ?? "primary_first", extra_provider_names: initialRouting?.extraProviderNames ?? [] }))
    || draftReasoningSig !== savedReasoningSig;

  return (
    <div className="space-y-4">
      {/* Identity section */}
      <section className="space-y-4 rounded-lg border p-3 sm:p-4 overflow-hidden">
        <h3 className="text-sm font-medium">{t("detail.identity")}</h3>
        <div className="space-y-2">
          <Label htmlFor="displayName">{t("form.displayName")}</Label>
          <Input id="displayName" value={displayName} onChange={(e) => setDisplayName(e.target.value)} placeholder={isOAuth ? t("form.oauthDisplayNamePlaceholder") : t("form.displayNamePlaceholder")} className="text-base md:text-sm" />
          {isOAuth ? <p className="text-xs text-muted-foreground">{t("form.oauthDisplayNameHint")}</p> : null}
        </div>
        <div className="space-y-2">
          <Label>{t("detail.providerType")}</Label>
          <div className="flex items-center gap-2"><Badge variant="outline">{typeLabel}</Badge></div>
        </div>
        <div className="space-y-2">
          <Label>{isOAuth ? t("form.oauthAlias") : t("form.name")}</Label>
          <div className="flex items-center gap-2">
            <code className="flex-1 rounded-md border bg-muted px-3 py-2 font-mono text-sm text-muted-foreground">{provider.name}</code>
            <Button type="button" variant="outline" size="icon" className="size-9 shrink-0" onClick={() => { navigator.clipboard.writeText(provider.name).catch(() => {}); toast.success(tc("copy")); }}>
              <Copy className="h-4 w-4" />
            </Button>
          </div>
        </div>
      </section>

      {isOAuth ? <ProviderOAuthAccountSection provider={provider} managedByProvider={managedByProvider} managedMemberCount={managedMemberCount} availability={currentOAuthAvailability} quota={quotaByName.get(provider.name)} quotaLoading={quotasLoading || quotasFetching} /> : null}

      {showReasoningDefaults ? (
        <ProviderReasoningSection
          reasoningThinkingLevel={reasoningThinkingLevel} setReasoningThinkingLevel={setReasoningThinkingLevel}
          reasoningEffort={reasoningEffort} setReasoningEffort={setReasoningEffort}
          reasoningFallback={reasoningFallback} setReasoningFallback={(v: string) => setReasoningFallback(v as typeof reasoningFallback)}
          reasoningExpert={reasoningExpert} setReasoningExpert={setReasoningExpert}
          setReasoningPreviewModel={setReasoningPreviewModel}
          reasoningCapableModels={reasoningCapableModels} reasoningPreviewEntry={reasoningPreviewEntry} reasoningPreviewCapability={reasoningPreviewCapability}
        />
      ) : null}

      {canEditPoolRouting ? (
        <ChatGPTOAuthRoutingSection title={t("detail.codexPoolDefaultsTitle")} description={t("detail.codexPoolDefaultsDescription")} currentProvider={provider.name} providers={providers} value={poolRouting} onChange={setPoolRouting} showOverrideMode={false} canManageProviders quotaByName={quotaByName} quotaLoading={quotasLoading || quotasFetching} entries={poolEntries} />
      ) : null}

      {isPoolOwner ? (
        <ProviderPoolActivitySection provider={provider} providerCounts={poolActivity.provider_counts} recentRequests={poolActivity.recent_requests} topAgents={poolActivity.top_agents} statsSampleSize={poolActivity.stats_sample_size} fetching={poolActivityFetching} onRefresh={() => void refreshPoolActivity()} providerByName={providerByName} statusByName={statusByName} quotaByName={quotaByName} />
      ) : null}

      {showApiKey ? (
        <section className="space-y-3 rounded-lg border p-3 sm:p-4 overflow-hidden">
          <h3 className="text-sm font-medium">{t("detail.apiKeySection")}</h3>
          <div className="space-y-2">
            <Label htmlFor="apiKey">{t("form.apiKey")}</Label>
            <Input id="apiKey" type="password" value={apiKey} onChange={(e) => setApiKey(e.target.value)} placeholder={t("form.apiKeyEditPlaceholder")} className="text-base md:text-sm" />
            <p className="text-xs text-muted-foreground">{t("form.apiKeySetHint")}</p>
          </div>
        </section>
      ) : null}

      {showEmbedding ? (
        <ProviderEmbeddingSection embEnabled={embEnabled} setEmbEnabled={setEmbEnabled} embModel={embModel} setEmbModel={setEmbModel} embApiBase={embApiBase} setEmbApiBase={setEmbApiBase} onVerify={() => verifyEmbedding(provider.id, embModel.trim() || undefined, undefined)} verifying={embVerifying} verifyResult={embResult} />
      ) : null}

      <section className="space-y-3 rounded-lg border p-3 sm:p-4 overflow-hidden">
        <h3 className="text-sm font-medium">{t("detail.statusSection")}</h3>
        <div className="flex items-center justify-between gap-4">
          <div className="space-y-0.5">
            <Label htmlFor="enabled" className="text-sm font-medium">{t("form.enabled")}</Label>
            <p className="text-xs text-muted-foreground">{t("detail.enabledDesc")}</p>
          </div>
          <Switch id="enabled" checked={enabled} onCheckedChange={setEnabled} />
        </div>
      </section>

      <StickySaveBar onSave={handleSave} saving={saving} disabled={!isDirty} />
    </div>
  );
}
