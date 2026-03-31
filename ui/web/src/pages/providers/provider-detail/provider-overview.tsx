import { useEffect, useMemo, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import {
  Copy,
  Loader2,
  CheckCircle2,
  XCircle,
  AlertTriangle,
} from "lucide-react";
import { StickySaveBar } from "@/components/shared/sticky-save-bar";
import { Input } from "@/components/ui/input";
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
import { Button } from "@/components/ui/button";
import { PROVIDER_TYPES } from "@/constants/providers";
import { toast } from "@/stores/use-toast-store";
import { useProviders } from "../hooks/use-providers";
import { useProviderModels } from "../hooks/use-provider-models";
import { useProviderVerify } from "../hooks/use-provider-verify";
import { ProviderOAuthAccountSection } from "./provider-oauth-account-section";
import {
  buildProviderSettingsWithChatGPTOAuthRouting,
  buildProviderSettingsWithReasoningDefaults,
  getChatGPTOAuthProviderRouting,
  getEmbeddingSettings,
  getProviderReasoningDefaults,
  normalizeReasoningEffort,
  normalizeReasoningFallback,
  deriveLegacyThinkingLevel,
} from "@/types/provider";
import type { ProviderData, ProviderInput } from "@/types/provider";
import type { ChatGPTOAuthRoutingConfig } from "@/types/agent";
import {
  useChatGPTOAuthProviderStatuses,
  type ChatGPTOAuthAvailability,
} from "../hooks/use-chatgpt-oauth-provider-statuses";
import { useChatGPTOAuthProviderQuotas } from "../hooks/use-chatgpt-oauth-provider-quotas";
import {
  ChatGPTOAuthRoutingSection,
} from "@/pages/agents/agent-detail/config-sections";
import type { CodexPoolEntry } from "@/pages/agents/agent-detail/codex-pool-activity-panel";
import { useProviderCodexPoolActivity } from "../hooks/use-provider-codex-pool-activity";
import { ProviderPoolActivitySection } from "./provider-pool-activity-section";

interface ProviderOverviewProps {
  provider: ProviderData;
  onUpdate: (id: string, data: ProviderInput) => Promise<void>;
}

const NO_API_KEY_TYPES = new Set(["claude_cli", "acp", "chatgpt_oauth"]);
const NO_EMBEDDING_TYPES = new Set([
  "claude_cli",
  "acp",
  "chatgpt_oauth",
  "suno",
  "anthropic_native",
]);
const SIMPLE_REASONING_LEVELS = new Set(["off", "low", "medium", "high"]);
const ADVANCED_REASONING_LEVELS = ["off", "auto", "none", "minimal", "low", "medium", "high", "xhigh"] as const;
const REASONING_FALLBACKS = ["downgrade", "provider_default", "off"] as const;

function providerStatus(
  providerName: string,
  statusByName: Map<string, { availability: ChatGPTOAuthAvailability }>,
  enabled?: boolean,
): ChatGPTOAuthAvailability {
  return (
    statusByName.get(providerName)?.availability ??
    (enabled === false ? "disabled" : "needs_sign_in")
  );
}

function routingSignature(routing: ChatGPTOAuthRoutingConfig): string {
  const extras = Array.from(
    new Set((routing.extra_provider_names ?? []).map((name) => name.trim()).filter(Boolean)),
  );
  const strategy =
    routing.strategy === "round_robin" || routing.strategy === "priority_order"
      ? routing.strategy
      : "primary_first";
  return JSON.stringify({
    strategy,
    extra_provider_names: extras,
  });
}

function comparableAPIKeyValue(
  apiKey: string,
  savedAPIKey: string,
  showApiKey: boolean,
): string {
  if (!showApiKey) return "";
  if (apiKey === "***") return "";
  if (apiKey === "" && savedAPIKey === "***") return "";
  return apiKey;
}

function providerFormSignature(input: {
  displayName: string;
  apiKey: string;
  savedAPIKey: string;
  showApiKey: boolean;
  enabled: boolean;
  embEnabled: boolean;
  embModel: string;
  embApiBase: string;
  routing: ChatGPTOAuthRoutingConfig;
  reasoningEffort: string;
  reasoningFallback: string;
  isOAuth: boolean;
}): string {
  return JSON.stringify({
    displayName: input.displayName,
    apiKey: comparableAPIKeyValue(input.apiKey, input.savedAPIKey, input.showApiKey),
    enabled: input.enabled,
    embEnabled: input.embEnabled,
    embModel: input.embModel,
    embApiBase: input.embApiBase,
    routing: input.isOAuth ? routingSignature(input.routing) : "",
    reasoning: reasoningSignature(input.reasoningEffort, input.reasoningFallback),
  });
}


function reasoningSignature(effort: string, fallback: string): string {
  return JSON.stringify({
    effort: normalizeReasoningEffort(effort) || "off",
    fallback: normalizeReasoningFallback(fallback),
  });
}

export function ProviderOverview({ provider, onUpdate }: ProviderOverviewProps) {
  const { t } = useTranslation("providers");
  const { t: tc } = useTranslation("common");
  const { providers } = useProviders();
  const {
    models: providerModels,
    reasoningDefaults: providerReasoningDefaults,
  } = useProviderModels(provider.id);
  const { statuses } = useChatGPTOAuthProviderStatuses(providers);

  const typeInfo = PROVIDER_TYPES.find((pt) => pt.value === provider.provider_type);
  const typeLabel = typeInfo?.label ?? provider.provider_type;
  const showApiKey = !NO_API_KEY_TYPES.has(provider.provider_type);
  const showEmbedding = !NO_EMBEDDING_TYPES.has(provider.provider_type);
  const isOAuth = provider.provider_type === "chatgpt_oauth";

  const providerByName = useMemo(
    () => new Map(providers.map((item) => [item.name, item])),
    [providers],
  );
  const poolOwnership = useMemo(() => {
    const membersByOwner = new Map<string, string[]>();
    const ownerByMember = new Map<string, string>();
    for (const item of providers) {
      if (item.provider_type !== "chatgpt_oauth") continue;
      const routing = getChatGPTOAuthProviderRouting(item.settings);
      if (!routing || routing.extraProviderNames.length === 0) continue;
      membersByOwner.set(item.name, routing.extraProviderNames);
      for (const memberName of routing.extraProviderNames) {
        if (!ownerByMember.has(memberName)) {
          ownerByMember.set(memberName, item.name);
        }
      }
    }
    return { membersByOwner, ownerByMember };
  }, [providers]);
  const statusByName = useMemo(
    () => new Map(statuses.map((status) => [status.provider.name, status])),
    [statuses],
  );
  const managedByOwnerName = isOAuth
    ? poolOwnership.ownerByMember.get(provider.name)
    : undefined;
  const managedByProvider = managedByOwnerName
    ? providerByName.get(managedByOwnerName)
    : undefined;
  const managedMemberCount = isOAuth
    ? poolOwnership.membersByOwner.get(provider.name)?.length ?? 0
    : 0;
  const canEditPoolRouting = isOAuth && !managedByOwnerName;
  const currentOAuthAvailability = providerStatus(
    provider.name,
    statusByName,
    provider.enabled,
  );

  const initialRouting = getChatGPTOAuthProviderRouting(provider.settings);
  const initialReasoningDefaults = getProviderReasoningDefaults(provider.settings)
    ?? providerReasoningDefaults
    ?? null;
  const initialReasoningEffort = initialReasoningDefaults?.effort ?? "off";
  const initialReasoningFallback = initialReasoningDefaults?.fallback ?? "downgrade";

  const [displayName, setDisplayName] = useState(provider.display_name || "");
  const [apiKey, setApiKey] = useState(provider.api_key || "");
  const [enabled, setEnabled] = useState(provider.enabled);
  const [poolRouting, setPoolRouting] = useState<ChatGPTOAuthRoutingConfig>({
    strategy: initialRouting?.strategy ?? "primary_first",
    extra_provider_names: initialRouting?.extraProviderNames ?? [],
  });
  const selectedPoolProviderNames = useMemo(
    () =>
      canEditPoolRouting
        ? Array.from(
            new Set(
              [provider.name, ...(poolRouting.extra_provider_names ?? [])]
                .filter(Boolean)
                .filter((name) => name === provider.name || providerByName.has(name)),
            ),
          )
        : [provider.name],
    [canEditPoolRouting, poolRouting.extra_provider_names, provider.name, providerByName],
  );

  const initEmb = getEmbeddingSettings(provider.settings);
  const [embEnabled, setEmbEnabled] = useState(initEmb?.enabled ?? false);
  const [embModel, setEmbModel] = useState(initEmb?.model ?? "");
  const [embApiBase, setEmbApiBase] = useState(initEmb?.api_base ?? "");
  const [reasoningThinkingLevel, setReasoningThinkingLevel] = useState(
    deriveLegacyThinkingLevel(initialReasoningEffort),
  );
  const [reasoningEffort, setReasoningEffort] = useState(initialReasoningEffort);
  const [reasoningFallback, setReasoningFallback] = useState(initialReasoningFallback);
  const [reasoningExpert, setReasoningExpert] = useState(
    Boolean(initialReasoningDefaults) && (
      !SIMPLE_REASONING_LEVELS.has(initialReasoningEffort)
      || initialReasoningFallback !== "downgrade"
    ),
  );
  const [reasoningPreviewModel, setReasoningPreviewModel] = useState("");
  const syncedProviderIDRef = useRef(provider.id);
  const reasoningCapableModels = useMemo(
    () => providerModels.filter((model) => (model.reasoning?.levels?.length ?? 0) > 0),
    [providerModels],
  );
  const reasoningPreviewEntry = useMemo(
    () => reasoningCapableModels.find((model) => model.id === reasoningPreviewModel)
      ?? reasoningCapableModels[0]
      ?? null,
    [reasoningCapableModels, reasoningPreviewModel],
  );
  const reasoningPreviewCapability = reasoningPreviewEntry?.reasoning ?? null;
  const showReasoningDefaults = Boolean(initialReasoningDefaults) || reasoningCapableModels.length > 0;
  const savedReasoningSignature = useMemo(
    () => reasoningSignature(
      initialReasoningDefaults?.effort ?? "off",
      initialReasoningDefaults?.fallback ?? "downgrade",
    ),
    [initialReasoningDefaults?.effort, initialReasoningDefaults?.fallback],
  );
  const draftReasoningSignature = useMemo(
    () => reasoningSignature(
      reasoningExpert ? reasoningEffort : reasoningThinkingLevel,
      reasoningExpert ? reasoningFallback : "downgrade",
    ),
    [reasoningEffort, reasoningExpert, reasoningFallback, reasoningThinkingLevel],
  );

  const savedFormSignature = useMemo(
    () =>
      providerFormSignature({
        displayName: provider.display_name || "",
        apiKey: provider.api_key || "",
        savedAPIKey: provider.api_key || "",
        showApiKey,
        enabled: provider.enabled,
        embEnabled: initEmb?.enabled ?? false,
        embModel: initEmb?.model ?? "",
        embApiBase: initEmb?.api_base ?? "",
        routing: {
          strategy: initialRouting?.strategy ?? "primary_first",
          extra_provider_names: initialRouting?.extraProviderNames ?? [],
        },
        reasoningEffort: initialReasoningDefaults?.effort ?? "off",
        reasoningFallback: initialReasoningDefaults?.fallback ?? "downgrade",
        isOAuth,
      }),
    [initEmb?.api_base, initEmb?.enabled, initEmb?.model, initialReasoningDefaults?.effort, initialReasoningDefaults?.fallback, initialRouting?.extraProviderNames, initialRouting?.strategy, isOAuth, provider.api_key, provider.display_name, provider.enabled, showApiKey],
  );
  const savedFormSignatureRef = useRef(savedFormSignature);
  const draftFormSignature = useMemo(
    () =>
      providerFormSignature({
        displayName,
        apiKey,
        savedAPIKey: provider.api_key || "",
        showApiKey,
        enabled,
        embEnabled,
        embModel,
        embApiBase,
        routing: poolRouting,
        reasoningEffort: reasoningExpert ? reasoningEffort : reasoningThinkingLevel,
        reasoningFallback: reasoningExpert ? reasoningFallback : "downgrade",
        isOAuth,
      }),
    [apiKey, displayName, embApiBase, embEnabled, embModel, enabled, isOAuth, poolRouting, provider.api_key, reasoningEffort, reasoningExpert, reasoningFallback, reasoningThinkingLevel, showApiKey],
  );

  useEffect(() => {
    const nextProviderID = provider.id;
    const es = getEmbeddingSettings(provider.settings);
    const routing = getChatGPTOAuthProviderRouting(provider.settings);
    const reasoning = getProviderReasoningDefaults(provider.settings) ?? providerReasoningDefaults;
    const syncFromProvider = () => {
      setEmbEnabled(es?.enabled ?? false);
      setEmbModel(es?.model ?? "");
      setEmbApiBase(es?.api_base ?? "");
      setPoolRouting({
        strategy: routing?.strategy ?? "primary_first",
        extra_provider_names: routing?.extraProviderNames ?? [],
      });
      const nextReasoningEffort = reasoning?.effort ?? "off";
      const nextReasoningFallback = reasoning?.fallback ?? "downgrade";
      setReasoningEffort(nextReasoningEffort);
      setReasoningFallback(nextReasoningFallback);
      setReasoningThinkingLevel(deriveLegacyThinkingLevel(nextReasoningEffort));
      setReasoningExpert(
        !SIMPLE_REASONING_LEVELS.has(nextReasoningEffort)
        || nextReasoningFallback !== "downgrade",
      );
      setDisplayName(provider.display_name || "");
      setApiKey(provider.api_key || "");
      setEnabled(provider.enabled);
    };

    if (nextProviderID !== syncedProviderIDRef.current) {
      syncedProviderIDRef.current = nextProviderID;
      savedFormSignatureRef.current = savedFormSignature;
      syncFromProvider();
      return;
    }

    const previousSavedSignature = savedFormSignatureRef.current;
    if (savedFormSignature === previousSavedSignature) {
      return;
    }

    if (draftFormSignature === previousSavedSignature) {
      syncFromProvider();
    }
    savedFormSignatureRef.current = savedFormSignature;
  }, [draftFormSignature, provider.api_key, provider.display_name, provider.enabled, provider.id, provider.settings, providerReasoningDefaults, savedFormSignature]);

  useEffect(() => {
    if (reasoningCapableModels.length === 0) {
      if (reasoningPreviewModel !== "") setReasoningPreviewModel("");
      return;
    }
    if (reasoningCapableModels.some((model) => model.id === reasoningPreviewModel)) {
      return;
    }
    setReasoningPreviewModel(reasoningCapableModels[0]?.id ?? "");
  }, [reasoningCapableModels, reasoningPreviewModel]);

  const quotaProviderNames = useMemo(
    () => {
      if (!isOAuth) return [];
      return Array.from(
        new Set(
          selectedPoolProviderNames.filter((providerName) => {
            if (!providerName) return false;
            const item = providerByName.get(providerName);
            return (
              providerStatus(providerName, statusByName, item?.enabled) === "ready"
            );
          }),
        ),
      );
    },
    [
      isOAuth,
      providerByName,
      selectedPoolProviderNames,
      statusByName,
    ],
  );
  const {
    quotaByName,
    isLoading: quotasLoading,
    isFetching: quotasFetching,
  } = useChatGPTOAuthProviderQuotas(quotaProviderNames, isOAuth);
  const poolEntries = useMemo<CodexPoolEntry[]>(() => {
    if (!canEditPoolRouting) return [];
    return selectedPoolProviderNames.map((providerName) => {
      const item = providerByName.get(providerName);
      return {
        name: providerName,
        label: item?.display_name || providerName,
        availability: providerStatus(providerName, statusByName, item?.enabled),
        role: providerName === provider.name ? "preferred" : "extra",
        requestCount: 0,
        directSelectionCount: 0,
        failoverServeCount: 0,
        successCount: 0,
        failureCount: 0,
        consecutiveFailures: 0,
        successRate: 0,
        healthScore: 0,
        healthState: "idle",
        providerHref: item?.id ? `/providers/${item.id}` : undefined,
        quota: quotaByName.get(providerName),
      };
    });
  }, [canEditPoolRouting, provider.name, providerByName, quotaByName, selectedPoolProviderNames, statusByName]);

  // Provider-scoped pool activity (only for pool owners)
  const isPoolOwner = canEditPoolRouting && selectedPoolProviderNames.length > 1;
  const {
    data: poolActivity,
    isFetching: poolActivityFetching,
    refetch: refreshPoolActivity,
  } = useProviderCodexPoolActivity(provider.id, 8, isPoolOwner);

  const { verifyEmbedding, embVerifying, embResult, resetEmb } = useProviderVerify();
  useEffect(() => { resetEmb(); }, [embModel, resetEmb]);

  const [saving, setSaving] = useState(false);

  const handleSave = async () => {
    setSaving(true);
    try {
      const nextDisplayName = displayName.trim();
      const nextEmbModel = embModel.trim();
      const nextEmbAPIBase = embApiBase.trim();
      const submittedAPIKey = showApiKey && apiKey && apiKey !== "***" ? apiKey : "";
      const data: ProviderInput = {
        name: provider.name,
        display_name: nextDisplayName || undefined,
        provider_type: provider.provider_type,
        enabled,
      };
      if (submittedAPIKey) {
        data.api_key = submittedAPIKey;
      }

      let nextSettings = { ...((provider.settings || {}) as Record<string, unknown>) };
      if (showEmbedding) {
        nextSettings = {
          ...nextSettings,
          embedding: embEnabled
            ? {
                enabled: true,
                model: nextEmbModel || undefined,
                api_base: nextEmbAPIBase || undefined,
              }
            : { enabled: false },
        };
      }
      if (isOAuth) {
        nextSettings = buildProviderSettingsWithChatGPTOAuthRouting(
          nextSettings,
          poolRouting,
        );
      }
      nextSettings = buildProviderSettingsWithReasoningDefaults(
        nextSettings,
        showReasoningDefaults
          ? {
              effort: reasoningExpert ? reasoningEffort : reasoningThinkingLevel,
              fallback: reasoningExpert ? reasoningFallback : "downgrade",
            }
          : null,
      );
      data.settings = nextSettings;

      await onUpdate(provider.id, data);
      setDisplayName(nextDisplayName);
      setEmbModel(nextEmbModel);
      setEmbApiBase(nextEmbAPIBase);
      if (submittedAPIKey) {
        setApiKey("***");
      }
    } catch {
      // toast shown by hook
    } finally {
      setSaving(false);
    }
  };

  const handleCopyName = () => {
    navigator.clipboard.writeText(provider.name).catch(() => {});
    toast.success(tc("copy"));
  };

  const handleVerifyEmbedding = () => {
    verifyEmbedding(provider.id, embModel.trim() || undefined, undefined);
  };

  const displayNameDirty = displayName !== (provider.display_name || "");
  const enabledDirty = enabled !== provider.enabled;
  const apiKeyDirty =
    showApiKey &&
    comparableAPIKeyValue(apiKey, provider.api_key || "", showApiKey) !== "";
  const embeddingDirty =
    embEnabled !== (initEmb?.enabled ?? false)
    || embModel !== (initEmb?.model ?? "")
    || embApiBase !== (initEmb?.api_base ?? "");
  const routingDirty = isOAuth && routingSignature(poolRouting) !== routingSignature({
    strategy: initialRouting?.strategy ?? "primary_first",
    extra_provider_names: initialRouting?.extraProviderNames ?? [],
  });
  const reasoningDirty = draftReasoningSignature !== savedReasoningSignature;
  const isDirty = displayNameDirty || enabledDirty || apiKeyDirty || embeddingDirty || routingDirty || reasoningDirty;

  return (
    <div className="space-y-4">
      <section className="space-y-4 rounded-lg border p-3 sm:p-4 overflow-hidden">
        <h3 className="text-sm font-medium">{t("detail.identity")}</h3>

        <div className="space-y-2">
          <Label htmlFor="displayName">{t("form.displayName")}</Label>
          <Input
            id="displayName"
            value={displayName}
            onChange={(e) => setDisplayName(e.target.value)}
            placeholder={isOAuth ? t("form.oauthDisplayNamePlaceholder") : t("form.displayNamePlaceholder")}
            className="text-base md:text-sm"
          />
          {isOAuth ? (
            <p className="text-xs text-muted-foreground">{t("form.oauthDisplayNameHint")}</p>
          ) : null}
        </div>

        <div className="space-y-2">
          <Label>{t("detail.providerType")}</Label>
          <div className="flex items-center gap-2">
            <Badge variant="outline">{typeLabel}</Badge>
          </div>
        </div>

        <div className="space-y-2">
          <Label>{isOAuth ? t("form.oauthAlias") : t("form.name")}</Label>
          <div className="flex items-center gap-2">
            <code className="flex-1 rounded-md border bg-muted px-3 py-2 font-mono text-sm text-muted-foreground">
              {provider.name}
            </code>
            <Button type="button" variant="outline" size="icon" className="size-9 shrink-0" onClick={handleCopyName}>
              <Copy className="h-4 w-4" />
            </Button>
          </div>
        </div>
      </section>

      {isOAuth ? (
        <ProviderOAuthAccountSection
          provider={provider}
          managedByProvider={managedByProvider}
          managedMemberCount={managedMemberCount}
          availability={currentOAuthAvailability}
          quota={quotaByName.get(provider.name)}
          quotaLoading={quotasLoading || quotasFetching}
        />
      ) : null}

      {showReasoningDefaults ? (
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
      ) : null}

      {canEditPoolRouting ? (
        <ChatGPTOAuthRoutingSection
          title={t("detail.codexPoolDefaultsTitle")}
          description={t("detail.codexPoolDefaultsDescription")}
          currentProvider={provider.name}
          providers={providers}
          value={poolRouting}
          onChange={setPoolRouting}
          showOverrideMode={false}
          canManageProviders
          quotaByName={quotaByName}
          quotaLoading={quotasLoading || quotasFetching}
          entries={poolEntries}
        />
      ) : null}

      {isPoolOwner ? (
        <ProviderPoolActivitySection
          provider={provider}
          providerCounts={poolActivity.provider_counts}
          recentRequests={poolActivity.recent_requests}
          topAgents={poolActivity.top_agents}
          statsSampleSize={poolActivity.stats_sample_size}
          fetching={poolActivityFetching}
          onRefresh={() => void refreshPoolActivity()}
          providerByName={providerByName}
          statusByName={statusByName}
          quotaByName={quotaByName}
        />
      ) : null}

      {showApiKey ? (
        <section className="space-y-3 rounded-lg border p-3 sm:p-4 overflow-hidden">
          <h3 className="text-sm font-medium">{t("detail.apiKeySection")}</h3>
          <div className="space-y-2">
            <Label htmlFor="apiKey">{t("form.apiKey")}</Label>
            <Input
              id="apiKey"
              type="password"
              value={apiKey}
              onChange={(e) => setApiKey(e.target.value)}
              placeholder={t("form.apiKeyEditPlaceholder")}
              className="text-base md:text-sm"
            />
            <p className="text-xs text-muted-foreground">{t("form.apiKeySetHint")}</p>
          </div>
        </section>
      ) : null}

      {showEmbedding ? (
        <section className="space-y-3 rounded-lg border p-3 sm:p-4 overflow-hidden">
          <h3 className="text-sm font-medium">{t("detail.embeddingSection")}</h3>
          <div className="flex items-center justify-between gap-4">
            <div className="space-y-0.5">
              <Label htmlFor="embEnabled" className="text-sm font-medium">
                {t("embedding.enable")}
              </Label>
              <p className="text-xs text-muted-foreground">{t("embedding.enableDesc")}</p>
            </div>
            <Switch id="embEnabled" checked={embEnabled} onCheckedChange={setEmbEnabled} />
          </div>

          {embEnabled ? (
            <div className="space-y-3 pt-1">
              <div className="space-y-2">
                <Label htmlFor="embModel">{t("embedding.model")}</Label>
                <Input
                  id="embModel"
                  value={embModel}
                  onChange={(e) => setEmbModel(e.target.value)}
                  placeholder="text-embedding-3-small"
                  className="text-base md:text-sm"
                />
              </div>

              <div className="space-y-2">
                <Label>{t("embedding.dimensions")}</Label>
                <p className="text-sm text-muted-foreground">1536</p>
                <p className="text-xs text-muted-foreground">{t("embedding.dimensionsHint")}</p>
              </div>

              <div className="space-y-2">
                <Label htmlFor="embApiBase">{t("embedding.apiBase")}</Label>
                <Input
                  id="embApiBase"
                  value={embApiBase}
                  onChange={(e) => setEmbApiBase(e.target.value)}
                  placeholder={t("embedding.apiBasePlaceholder")}
                  className="text-base md:text-sm"
                />
                <p className="text-xs text-muted-foreground">{t("embedding.apiBaseHint")}</p>
              </div>

              <div className="flex items-center gap-3">
                <Button
                  type="button"
                  variant="outline"
                  size="sm"
                  disabled={embVerifying}
                  onClick={handleVerifyEmbedding}
                >
                  {embVerifying ? <Loader2 className="mr-1.5 h-3.5 w-3.5 animate-spin" /> : null}
                  {t("embedding.verify")}
                </Button>
                {embResult ? (
                  <span
                    className={`flex items-center gap-1 text-xs ${
                      embResult.valid
                        ? embResult.dimension_mismatch
                          ? "text-amber-600 dark:text-amber-400"
                          : "text-success"
                        : "text-destructive"
                    }`}
                  >
                    {embResult.valid ? (
                      <>
                        {embResult.dimension_mismatch ? (
                          <AlertTriangle className="h-3.5 w-3.5" />
                        ) : (
                          <CheckCircle2 className="h-3.5 w-3.5" />
                        )}
                        {embResult.dimension_mismatch
                          ? t("embedding.dimensionsMismatch", { count: embResult.dimensions })
                          : `${embResult.dimensions} dimensions`}
                      </>
                    ) : (
                      <>
                        <XCircle className="h-3.5 w-3.5" />
                        {embResult.error || t("embedding.verifyFailed")}
                      </>
                    )}
                  </span>
                ) : null}
              </div>
            </div>
          ) : null}
        </section>
      ) : null}

      <section className="space-y-3 rounded-lg border p-3 sm:p-4 overflow-hidden">
        <h3 className="text-sm font-medium">{t("detail.statusSection")}</h3>
        <div className="flex items-center justify-between gap-4">
          <div className="space-y-0.5">
            <Label htmlFor="enabled" className="text-sm font-medium">
              {t("form.enabled")}
            </Label>
            <p className="text-xs text-muted-foreground">{t("detail.enabledDesc")}</p>
          </div>
          <Switch id="enabled" checked={enabled} onCheckedChange={setEnabled} />
        </div>
      </section>

      <StickySaveBar
        onSave={handleSave}
        saving={saving}
        disabled={!isDirty}
      />
    </div>
  );
}
