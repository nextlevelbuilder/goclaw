import { useState, useEffect, useCallback } from "react";
import { useTranslation } from "react-i18next";
import {
  Settings2, Loader2, Save, AlertTriangle, Info, ExternalLink,
  Brain, Eye, MessageSquareText, Archive, Clock, Hash, CheckCircle2, XCircle,
} from "lucide-react";
import { Link } from "react-router";
import {
  Dialog, DialogContent, DialogHeader, DialogTitle,
} from "@/components/ui/dialog";
import { Button } from "@/components/ui/button";
import { Label } from "@/components/ui/label";
import { Input } from "@/components/ui/input";
import { Card, CardContent, CardHeader, CardTitle, CardDescription } from "@/components/ui/card";
import { FeatureSwitchGroup } from "@/components/shared/feature-switch-group";
import type { FeatureSwitchItem } from "@/components/shared/feature-switch-group";
import { ProviderModelSelect } from "@/components/shared/provider-model-select";
import { useProviderVerify } from "@/pages/providers/hooks/use-provider-verify";
import { useProviders } from "@/pages/providers/hooks/use-providers";
import { useHttp } from "@/hooks/use-ws";
import { toast } from "@/stores/use-toast-store";

// Curated 1536-dimension embedding models per provider type.
// Verified 1536d embedding models per provider type.
// Only providers with confirmed 1536-dimension output are listed.
const EMBEDDING_MODELS: Record<string, { id: string; name: string }[]> = {
  // OpenAI — native 1536d
  openai_compat: [
    { id: "text-embedding-3-small", name: "text-embedding-3-small (1536d)" },
    { id: "text-embedding-3-large", name: "text-embedding-3-large (3072d → 1536 via dimensions)" },
    { id: "text-embedding-ada-002", name: "text-embedding-ada-002 (1536d)" },
  ],
  // OpenRouter — proxied OpenAI models
  openrouter: [
    { id: "openai/text-embedding-3-small", name: "openai/text-embedding-3-small (1536d)" },
    { id: "openai/text-embedding-3-large", name: "openai/text-embedding-3-large (3072d → 1536)" },
    { id: "openai/text-embedding-ada-002", name: "openai/text-embedding-ada-002 (1536d)" },
  ],
  // Gemini — gemini-embedding-001 (3072d native, truncate to 1536 via dimensions param)
  gemini_native: [
    { id: "gemini-embedding-001", name: "gemini-embedding-001 (3072d → 1536 via dimensions)" },
  ],
  // Mistral — codestral-embed defaults to 1536d (MRL)
  mistral: [
    { id: "codestral-embed", name: "codestral-embed (1536d default)" },
  ],
  // DashScope/Qwen — text-embedding-v3 (custom dimensions support)
  dashscope: [
    { id: "text-embedding-v3", name: "text-embedding-v3 (1536 via dimensions)" },
  ],
  // Cohere — embed-v4 native 1536d
  cohere: [
    { id: "embed-v4", name: "embed-v4 (1536d native)" },
  ],
};
// Fallback for unlisted provider types — no curated models
const DEFAULT_EMBEDDING_MODELS: { id: string; name: string }[] = [];

interface SystemSettingsModalProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
}

interface InitState {
  embProvider: string;
  embModel: string;
  embMaxChunkLen: string;
  embChunkOverlap: string;
  toolStatus: boolean;
  blockReply: boolean;
  intentClassify: boolean;
  compProvider: string;
  compModel: string;
  compThreshold: string;
  compKeepRecent: string;
  compMaxTokens: string;
}

const DEFAULTS: InitState = {
  embProvider: "", embModel: "",
  embMaxChunkLen: "", embChunkOverlap: "",
  toolStatus: true, blockReply: false, intentClassify: true,
  compProvider: "", compModel: "",
  compThreshold: "", compKeepRecent: "", compMaxTokens: "",
};

function parseBool(v: string | undefined, fallback: boolean): boolean {
  if (v === undefined) return fallback;
  return v !== "false" && v !== "0";
}

export function SystemSettingsModal({ open, onOpenChange }: SystemSettingsModalProps) {
  const { t } = useTranslation("system-settings");
  const http = useHttp();
  const { providers } = useProviders();

  const [loading, setLoading] = useState(false);
  const [saving, setSaving] = useState(false);
  const [init, setInit] = useState<InitState>(DEFAULTS);

  // Embedding
  const [embProvider, setEmbProvider] = useState("");
  const [embModel, setEmbModel] = useState("");
  const [embMaxChunkLen, setEmbMaxChunkLen] = useState("");
  const [embChunkOverlap, setEmbChunkOverlap] = useState("");
  const { verifyEmbedding, embVerifying, embResult, resetEmb } = useProviderVerify();

  // UX Behavior
  const [toolStatus, setToolStatus] = useState(true);
  const [blockReply, setBlockReply] = useState(false);
  const [intentClassify, setIntentClassify] = useState(true);

  // Pending Compaction
  const [compProvider, setCompProvider] = useState("");
  const [compModel, setCompModel] = useState("");
  const [compThreshold, setCompThreshold] = useState("");
  const [compKeepRecent, setCompKeepRecent] = useState("");
  const [compMaxTokens, setCompMaxTokens] = useState("");

  const applyConfigs = useCallback((configs: Record<string, string>) => {
    const s: InitState = {
      embProvider: configs["embedding.provider"] ?? "",
      embModel: configs["embedding.model"] ?? "",
      embMaxChunkLen: configs["embedding.max_chunk_len"] ?? "",
      embChunkOverlap: configs["embedding.chunk_overlap"] ?? "",
      toolStatus: parseBool(configs["gateway.tool_status"], true),
      blockReply: parseBool(configs["gateway.block_reply"], false),
      intentClassify: parseBool(configs["gateway.intent_classify"], true),
      compProvider: configs["compaction.provider"] ?? "",
      compModel: configs["compaction.model"] ?? "",
      compThreshold: configs["compaction.threshold"] ?? "",
      compKeepRecent: configs["compaction.keep_recent"] ?? "",
      compMaxTokens: configs["compaction.max_tokens"] ?? "",
    };
    setInit(s);
    setEmbProvider(s.embProvider);
    setEmbModel(s.embModel);
    setEmbMaxChunkLen(s.embMaxChunkLen);
    setEmbChunkOverlap(s.embChunkOverlap);
    setToolStatus(s.toolStatus);
    setBlockReply(s.blockReply);
    setIntentClassify(s.intentClassify);
    setCompProvider(s.compProvider);
    setCompModel(s.compModel);
    setCompThreshold(s.compThreshold);
    setCompKeepRecent(s.compKeepRecent);
    setCompMaxTokens(s.compMaxTokens);
    resetEmb();
  }, [resetEmb]);

  useEffect(() => {
    if (!open) return;
    setLoading(true);
    http
      .get<Record<string, string>>("/v1/system-configs")
      .then(applyConfigs)
      .catch((err) => toast.error(err instanceof Error ? err.message : t("loadFailed")))
      .finally(() => setLoading(false));
  }, [open, http, applyConfigs, t]);

  useEffect(() => { resetEmb(); }, [embProvider, embModel, resetEmb]);

  const embChanged = embProvider !== init.embProvider || embModel !== init.embModel;
  const embVerified = embResult?.valid === true;
  const saveDisabled = saving || (embChanged && !embVerified);

  const selectedEmbProviderData = providers.find((p) => p.name === embProvider);
  const embExtraModels = selectedEmbProviderData
    ? (EMBEDDING_MODELS[selectedEmbProviderData.provider_type] ?? DEFAULT_EMBEDDING_MODELS)
    : DEFAULT_EMBEDDING_MODELS;

  const handleVerifyEmb = () => {
    if (!selectedEmbProviderData) return;
    // Always request 1536 dims — pgvector schema requires vector(1536).
    verifyEmbedding(selectedEmbProviderData.id, embModel.trim() || undefined, 1536);
  };

  const handleSave = async () => {
    setSaving(true);
    try {
      const updates: Record<string, string> = {};
      if (embProvider !== init.embProvider) updates["embedding.provider"] = embProvider;
      if (embModel !== init.embModel) updates["embedding.model"] = embModel;
      if (embMaxChunkLen !== init.embMaxChunkLen) updates["embedding.max_chunk_len"] = embMaxChunkLen;
      if (embChunkOverlap !== init.embChunkOverlap) updates["embedding.chunk_overlap"] = embChunkOverlap;
      if (toolStatus !== init.toolStatus) updates["gateway.tool_status"] = String(toolStatus);
      if (blockReply !== init.blockReply) updates["gateway.block_reply"] = String(blockReply);
      if (intentClassify !== init.intentClassify) updates["gateway.intent_classify"] = String(intentClassify);
      if (compProvider !== init.compProvider) updates["compaction.provider"] = compProvider;
      if (compModel !== init.compModel) updates["compaction.model"] = compModel;
      if (compThreshold !== init.compThreshold) updates["compaction.threshold"] = compThreshold;
      if (compKeepRecent !== init.compKeepRecent) updates["compaction.keep_recent"] = compKeepRecent;
      if (compMaxTokens !== init.compMaxTokens) updates["compaction.max_tokens"] = compMaxTokens;

      for (const [key, value] of Object.entries(updates)) {
        await http.put(`/v1/system-configs/${key}`, { value });
      }
      toast.success(t("saved"));
      onOpenChange(false);
    } catch (err) {
      toast.error(err instanceof Error ? err.message : t("saveFailed"));
    } finally {
      setSaving(false);
    }
  };

  // UX Behavior switch items — reuses FeatureSwitchGroup pattern from config page
  const uxItems: FeatureSwitchItem[] = [
    {
      icon: Eye,
      iconClass: "text-blue-500",
      label: t("ux.toolStatus"),
      hint: t("ux.toolStatusHint"),
      checked: toolStatus,
      onCheckedChange: setToolStatus,
      infoWhenOn: t("ux.toolStatusInfo"),
      infoClass: "border-blue-200 bg-blue-50 text-blue-700 dark:border-blue-800 dark:bg-blue-950/30 dark:text-blue-300",
    },
    {
      icon: MessageSquareText,
      iconClass: "text-emerald-500",
      label: t("ux.blockReply"),
      hint: t("ux.blockReplyHint"),
      checked: blockReply,
      onCheckedChange: setBlockReply,
      infoWhenOn: t("ux.blockReplyInfo"),
      infoClass: "border-emerald-200 bg-emerald-50 text-emerald-700 dark:border-emerald-800 dark:bg-emerald-950/30 dark:text-emerald-300",
    },
    {
      icon: Brain,
      iconClass: "text-orange-500",
      label: t("ux.intentClassify"),
      hint: t("ux.intentClassifyHint"),
      checked: intentClassify,
      onCheckedChange: setIntentClassify,
      infoWhenOn: t("ux.intentClassifyInfo"),
      infoClass: "border-orange-200 bg-orange-50 text-orange-700 dark:border-orange-800 dark:bg-orange-950/30 dark:text-orange-300",
    },
  ];

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="flex max-h-[90vh] w-[95vw] flex-col sm:max-w-2xl">
        <DialogHeader>
          <DialogTitle className="flex items-center gap-2">
            <Settings2 className="h-5 w-5" />
            {t("title")}
          </DialogTitle>
        </DialogHeader>

        {loading ? (
          <div className="flex flex-1 items-center justify-center py-12">
            <Loader2 className="h-6 w-6 animate-spin text-muted-foreground" />
          </div>
        ) : (
          <div className="min-h-0 flex-1 space-y-4 overflow-y-auto -mx-4 px-4 sm:-mx-6 sm:px-6">
            {/* ── Embedding ── */}
            <Card className="border-blue-200 dark:border-blue-800">
              <CardHeader>
                <CardTitle className="flex items-center gap-2 text-base">
                  <Brain className="h-4 w-4 text-blue-500" />
                  {t("embedding.title")}
                </CardTitle>
                <CardDescription>{t("embedding.description")}</CardDescription>
              </CardHeader>
              <CardContent className="space-y-4 pt-0">
                <div className="flex items-start gap-2 rounded-md border border-blue-200 bg-blue-50 px-3 py-2 text-xs text-blue-700 dark:border-blue-800 dark:bg-blue-950/30 dark:text-blue-300">
                  <Info className="mt-0.5 h-3.5 w-3.5 shrink-0" />
                  <div className="space-y-1">
                    <p>{t("embedding.importance")}</p>
                    <p className="opacity-75">{t("embedding.supportedProviders")}</p>
                  </div>
                </div>

                <ProviderModelSelect
                  provider={embProvider}
                  onProviderChange={(v) => { setEmbProvider(v); setEmbModel(""); }}
                  model={embModel}
                  onModelChange={setEmbModel}
                  allowEmpty
                  showVerify={false}
                  extraModels={embExtraModels}
                  modelFilter="embed"
                  providerLabel={t("embedding.provider")}
                  modelLabel={t("embedding.model")}
                  providerTip={t("embedding.providerTip")}
                  modelTip={t("embedding.modelTip")}
                  providerPlaceholder={t("embedding.providerPlaceholder")}
                  modelPlaceholder={t("embedding.modelPlaceholder")}
                />

                <div className="flex items-center gap-3">
                  <Button
                    type="button"
                    variant="outline"
                    size="sm"
                    disabled={!selectedEmbProviderData || !embModel.trim() || embVerifying}
                    onClick={handleVerifyEmb}
                  >
                    {embVerifying ? <Loader2 className="mr-1.5 h-3.5 w-3.5 animate-spin" /> : null}
                    {t("embedding.verify")}
                  </Button>
                  {embResult && (
                    <span className={`flex items-center gap-1 text-xs ${
                      embResult.valid
                        ? embResult.dimension_mismatch
                          ? "text-amber-600 dark:text-amber-400"
                          : "text-emerald-600 dark:text-emerald-400"
                        : "text-destructive"
                    }`}>
                      {embResult.valid ? (
                        <>
                          {embResult.dimension_mismatch ? (
                            <AlertTriangle className="h-3.5 w-3.5" />
                          ) : (
                            <CheckCircle2 className="h-3.5 w-3.5" />
                          )}
                          {embResult.dimension_mismatch
                            ? t("embedding.dimensionsMismatch", { count: embResult.dimensions })
                            : t("embedding.dimensions", { count: embResult.dimensions })}
                        </>
                      ) : (
                        <>
                          <XCircle className="h-3.5 w-3.5" />
                          {embResult.error || t("embedding.verifyFailed")}
                        </>
                      )}
                    </span>
                  )}
                </div>

                <div className="grid grid-cols-1 gap-4 sm:grid-cols-2">
                  <div className="space-y-1.5">
                    <Label htmlFor="embMaxChunkLen" className="text-xs">{t("embedding.maxChunkLen")}</Label>
                    <Input id="embMaxChunkLen" type="number" placeholder="1000" value={embMaxChunkLen} onChange={(e) => setEmbMaxChunkLen(e.target.value)} className="text-base md:text-sm" />
                    <p className="text-xs text-muted-foreground">{t("embedding.maxChunkLenHint")}</p>
                  </div>
                  <div className="space-y-1.5">
                    <Label htmlFor="embChunkOverlap" className="text-xs">{t("embedding.chunkOverlap")}</Label>
                    <Input id="embChunkOverlap" type="number" placeholder="200" value={embChunkOverlap} onChange={(e) => setEmbChunkOverlap(e.target.value)} className="text-base md:text-sm" />
                    <p className="text-xs text-muted-foreground">{t("embedding.chunkOverlapHint")}</p>
                  </div>
                </div>
              </CardContent>
            </Card>

            {/* ── UX Behavior — uses FeatureSwitchGroup (same as config page) ── */}
            <FeatureSwitchGroup
              title={t("ux.title")}
              description={t("ux.description")}
              items={uxItems}
            />

            {/* ── Pending Compaction ── */}
            <Card>
              <CardHeader>
                <CardTitle className="text-base">{t("compaction.title")}</CardTitle>
                <CardDescription>{t("compaction.description")}</CardDescription>
              </CardHeader>
              <CardContent className="space-y-0 pt-0">
                <div className="border-b pb-4">
                  <ProviderModelSelect
                    provider={compProvider}
                    onProviderChange={setCompProvider}
                    model={compModel}
                    onModelChange={setCompModel}
                    allowEmpty
                    providerPlaceholder={t("compaction.providerPlaceholder")}
                  />
                </div>

                <div className="flex items-start justify-between gap-4 border-b py-4">
                  <div className="flex items-start gap-3">
                    <Archive className="mt-0.5 h-4 w-4 shrink-0 text-orange-500" />
                    <div className="space-y-0.5">
                      <Label className="text-sm font-medium">{t("compaction.threshold")}</Label>
                      <p className="text-xs text-muted-foreground">{t("compaction.thresholdHint")}</p>
                    </div>
                  </div>
                  <Input
                    type="number" value={compThreshold}
                    onChange={(e) => setCompThreshold(e.target.value)}
                    placeholder="200" min={1}
                    className="w-24 shrink-0 text-base md:text-sm"
                  />
                </div>

                <div className="flex items-start justify-between gap-4 border-b py-4">
                  <div className="flex items-start gap-3">
                    <Clock className="mt-0.5 h-4 w-4 shrink-0 text-blue-500" />
                    <div className="space-y-0.5">
                      <Label className="text-sm font-medium">{t("compaction.keepRecent")}</Label>
                      <p className="text-xs text-muted-foreground">{t("compaction.keepRecentHint")}</p>
                    </div>
                  </div>
                  <Input
                    type="number" value={compKeepRecent}
                    onChange={(e) => setCompKeepRecent(e.target.value)}
                    placeholder="40" min={1}
                    className="w-24 shrink-0 text-base md:text-sm"
                  />
                </div>

                <div className="flex items-start justify-between gap-4 border-b py-4">
                  <div className="flex items-start gap-3">
                    <Hash className="mt-0.5 h-4 w-4 shrink-0 text-orange-500" />
                    <div className="space-y-0.5">
                      <Label className="text-sm font-medium">{t("compaction.maxTokens")}</Label>
                      <p className="text-xs text-muted-foreground">{t("compaction.maxTokensHint")}</p>
                    </div>
                  </div>
                  <Input
                    type="number" value={compMaxTokens}
                    onChange={(e) => setCompMaxTokens(e.target.value)}
                    placeholder="4096" min={256}
                    className="w-24 shrink-0 text-base md:text-sm"
                  />
                </div>

                <div className="flex items-start gap-2 rounded-md border border-orange-200 bg-orange-50 px-3 py-2 mt-4 text-xs text-orange-700 dark:border-orange-800 dark:bg-orange-950/30 dark:text-orange-300">
                  <Info className="mt-0.5 h-3.5 w-3.5 shrink-0" />
                  <span>{t("compaction.info")}</span>
                </div>
              </CardContent>
            </Card>
          </div>
        )}

        {/* Footer */}
        <div className="flex flex-col gap-3 border-t pt-4 shrink-0">
          {embChanged && !embVerified && (
            <p className="flex items-center gap-1.5 text-xs text-amber-600 dark:text-amber-400">
              <AlertTriangle className="h-3.5 w-3.5 shrink-0" />
              {t("embedding.verifyRequired")}
            </p>
          )}
          <div className="flex items-center justify-between gap-2">
            <Link
              to="/config"
              onClick={() => onOpenChange(false)}
              className="flex items-center gap-1 text-xs text-muted-foreground hover:text-foreground transition-colors"
            >
              <ExternalLink className="h-3 w-3" />
              {t("moreConfig")}
            </Link>
            <div className="flex gap-2">
              <Button variant="outline" onClick={() => onOpenChange(false)} disabled={saving}>
                {t("cancel")}
              </Button>
              <Button onClick={handleSave} disabled={saveDisabled}>
                {saving ? <Loader2 className="mr-2 h-4 w-4 animate-spin" /> : <Save className="mr-2 h-4 w-4" />}
                {saving ? t("saving") : t("save")}
              </Button>
            </div>
          </div>
        </div>
      </DialogContent>
    </Dialog>
  );
}
