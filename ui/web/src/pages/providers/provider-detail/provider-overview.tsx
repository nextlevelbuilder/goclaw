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
import { Switch } from "@/components/ui/switch";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { PROVIDER_TYPES } from "@/constants/providers";
import { toast } from "@/stores/use-toast-store";
import { useProviders } from "../hooks/use-providers";
import { useProviderVerify } from "../hooks/use-provider-verify";
import { ProviderOAuthAccountSection } from "./provider-oauth-account-section";
import {
  buildProviderSettingsWithChatGPTOAuthRouting,
  getChatGPTOAuthProviderRouting,
  getEmbeddingSettings,
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
  embDimensions: string;
  routing: ChatGPTOAuthRoutingConfig;
  isOAuth: boolean;
}): string {
  return JSON.stringify({
    displayName: input.displayName,
    apiKey: comparableAPIKeyValue(input.apiKey, input.savedAPIKey, input.showApiKey),
    enabled: input.enabled,
    embEnabled: input.embEnabled,
    embModel: input.embModel,
    embApiBase: input.embApiBase,
    embDimensions: input.embDimensions,
    routing: input.isOAuth ? routingSignature(input.routing) : "",
  });
}

export function ProviderOverview({ provider, onUpdate }: ProviderOverviewProps) {
  const { t } = useTranslation("providers");
  const { t: tc } = useTranslation("common");
  const { providers } = useProviders();
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
  const [embDimensions, setEmbDimensions] = useState(initEmb?.dimensions ? String(initEmb.dimensions) : "");
  const syncedProviderIDRef = useRef(provider.id);

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
        embDimensions: initEmb?.dimensions ? String(initEmb.dimensions) : "",
        routing: {
          strategy: initialRouting?.strategy ?? "primary_first",
          extra_provider_names: initialRouting?.extraProviderNames ?? [],
        },
        isOAuth,
      }),
    [initEmb?.api_base, initEmb?.dimensions, initEmb?.enabled, initEmb?.model, initialRouting?.extraProviderNames, initialRouting?.strategy, isOAuth, provider.api_key, provider.display_name, provider.enabled, showApiKey],
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
        embDimensions,
        routing: poolRouting,
        isOAuth,
      }),
    [apiKey, displayName, embApiBase, embDimensions, embEnabled, embModel, enabled, isOAuth, poolRouting, provider.api_key, showApiKey],
  );

  useEffect(() => {
    const nextProviderID = provider.id;
    const es = getEmbeddingSettings(provider.settings);
    const routing = getChatGPTOAuthProviderRouting(provider.settings);
    const syncFromProvider = () => {
      setEmbEnabled(es?.enabled ?? false);
      setEmbModel(es?.model ?? "");
      setEmbApiBase(es?.api_base ?? "");
      setEmbDimensions(es?.dimensions ? String(es.dimensions) : "");
      setPoolRouting({
        strategy: routing?.strategy ?? "primary_first",
        extra_provider_names: routing?.extraProviderNames ?? [],
      });
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
  }, [draftFormSignature, provider.api_key, provider.display_name, provider.enabled, provider.id, provider.settings, savedFormSignature]);

  const quotaProviderNames = useMemo(
    () => {
      if (!isOAuth) return [];
      const candidateNames = [
        provider.name,
        ...(canEditPoolRouting ? poolRouting.extra_provider_names ?? [] : []),
      ];
      return Array.from(
        new Set(
          candidateNames.filter((providerName) => {
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
      canEditPoolRouting,
      isOAuth,
      poolRouting.extra_provider_names,
      provider.name,
      providerByName,
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
    return quotaProviderNames.map((providerName) => {
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
  }, [canEditPoolRouting, provider.name, providerByName, quotaByName, quotaProviderNames, statusByName]);

  // Provider-scoped pool activity (only for pool owners)
  const isPoolOwner = canEditPoolRouting && poolEntries.length > 0;
  const {
    data: poolActivity,
    isFetching: poolActivityFetching,
    refetch: refreshPoolActivity,
  } = useProviderCodexPoolActivity(provider.id, 8, isPoolOwner);

  const { verifyEmbedding, embVerifying, embResult, resetEmb } = useProviderVerify();
  useEffect(() => { resetEmb(); }, [embModel, embDimensions, resetEmb]);

  const [saving, setSaving] = useState(false);

  const handleSave = async () => {
    setSaving(true);
    try {
      const nextDisplayName = displayName.trim();
      const nextEmbModel = embModel.trim();
      const nextEmbAPIBase = embApiBase.trim();
      const parsedDims = embDimensions ? parseInt(embDimensions, 10) : 0;
      const nextEmbDimensions = parsedDims > 0 ? String(parsedDims) : "";
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
                dimensions: parsedDims > 0 ? parsedDims : undefined,
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
      data.settings = nextSettings;

      await onUpdate(provider.id, data);
      setDisplayName(nextDisplayName);
      setEmbModel(nextEmbModel);
      setEmbApiBase(nextEmbAPIBase);
      setEmbDimensions(nextEmbDimensions);
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
    const parsedDims = embDimensions ? parseInt(embDimensions, 10) : 0;
    verifyEmbedding(provider.id, embModel.trim() || undefined, parsedDims > 0 ? parsedDims : undefined);
  };

  const displayNameDirty = displayName !== (provider.display_name || "");
  const enabledDirty = enabled !== provider.enabled;
  const apiKeyDirty =
    showApiKey &&
    comparableAPIKeyValue(apiKey, provider.api_key || "", showApiKey) !== "";
  const embeddingDirty =
    embEnabled !== (initEmb?.enabled ?? false)
    || embModel !== (initEmb?.model ?? "")
    || embApiBase !== (initEmb?.api_base ?? "")
    || embDimensions !== (initEmb?.dimensions ? String(initEmb.dimensions) : "");
  const routingDirty = isOAuth && routingSignature(poolRouting) !== routingSignature({
    strategy: initialRouting?.strategy ?? "primary_first",
    extra_provider_names: initialRouting?.extraProviderNames ?? [],
  });
  const isDirty = displayNameDirty || enabledDirty || apiKeyDirty || embeddingDirty || routingDirty;

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
                <Label htmlFor="embDimensions">{t("embedding.dimensions")}</Label>
                <Input
                  id="embDimensions"
                  type="number"
                  value={embDimensions}
                  onChange={(e) => setEmbDimensions(e.target.value)}
                  placeholder="1536"
                  min={1}
                  className="text-base md:text-sm"
                />
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
