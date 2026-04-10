import { useState, useEffect, useCallback } from "react";
import { useTranslation } from "react-i18next";
import { Settings2, Loader2, Save, AlertTriangle, Info, ExternalLink, Network, Cog } from "lucide-react";
import { Link } from "react-router";
import { Dialog, DialogContent, DialogHeader, DialogTitle } from "@/components/ui/dialog";
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
import { EMBEDDING_MODELS, DEFAULT_EMBEDDING_MODELS, DEFAULTS, parseBool, type InitState } from "./system-settings-constants";
import { SystemSettingsEmbeddingCard } from "./system-settings-embedding-card";
import { SystemSettingsCompactionCard } from "./system-settings-compaction-card";
import { Eye, MessageSquareText, Brain } from "lucide-react";

interface SystemSettingsModalProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
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

  // Compaction
  const [compProvider, setCompProvider] = useState("");
  const [compModel, setCompModel] = useState("");
  const [compThreshold, setCompThreshold] = useState("");
  const [compKeepRecent, setCompKeepRecent] = useState("");
  const [compMaxTokens, setCompMaxTokens] = useState("");

  // Knowledge Graph
  const [kgProvider, setKgProvider] = useState("");
  const [kgModel, setKgModel] = useState("");
  const [kgMinConfidence, setKgMinConfidence] = useState("0.75");

  // Background Workers
  const [bgProvider, setBgProvider] = useState("");
  const [bgModel, setBgModel] = useState("");

  const applyConfigs = useCallback((
    configs: Record<string, string>,
    kgSettings?: { extraction_provider?: string; extraction_model?: string; min_confidence?: number },
  ) => {
    const s: InitState = {
      embProvider: configs["embedding.provider"] ?? "", embModel: configs["embedding.model"] ?? "",
      embMaxChunkLen: configs["embedding.max_chunk_len"] ?? "", embChunkOverlap: configs["embedding.chunk_overlap"] ?? "",
      toolStatus: parseBool(configs["gateway.tool_status"], true), blockReply: parseBool(configs["gateway.block_reply"], false),
      intentClassify: parseBool(configs["gateway.intent_classify"], true),
      compProvider: configs["compaction.provider"] ?? "", compModel: configs["compaction.model"] ?? "",
      compThreshold: configs["compaction.threshold"] ?? "", compKeepRecent: configs["compaction.keep_recent"] ?? "",
      compMaxTokens: configs["compaction.max_tokens"] ?? "",
      kgProvider: kgSettings?.extraction_provider ?? "", kgModel: kgSettings?.extraction_model ?? "",
      kgMinConfidence: String(kgSettings?.min_confidence ?? 0.75),
      bgProvider: configs["background.provider"] ?? "", bgModel: configs["background.model"] ?? "",
    };
    setInit(s);
    setEmbProvider(s.embProvider); setEmbModel(s.embModel); setEmbMaxChunkLen(s.embMaxChunkLen); setEmbChunkOverlap(s.embChunkOverlap);
    setToolStatus(s.toolStatus); setBlockReply(s.blockReply); setIntentClassify(s.intentClassify);
    setCompProvider(s.compProvider); setCompModel(s.compModel); setCompThreshold(s.compThreshold); setCompKeepRecent(s.compKeepRecent); setCompMaxTokens(s.compMaxTokens);
    setKgProvider(s.kgProvider); setKgModel(s.kgModel); setKgMinConfidence(s.kgMinConfidence);
    setBgProvider(s.bgProvider); setBgModel(s.bgModel);
    resetEmb();
  }, [resetEmb]);

  useEffect(() => {
    if (!open) return;
    setLoading(true);
    Promise.all([
      http.get<Record<string, string>>("/v1/system-configs"),
      http.get<{ settings?: Record<string, unknown> }>("/v1/tools/builtin/knowledge_graph_search")
        .then((r) => r.settings as { extraction_provider?: string; extraction_model?: string; min_confidence?: number } | undefined)
        .catch(() => undefined),
    ])
      .then(([configs, kgSettings]) => applyConfigs(configs, kgSettings))
      .catch((err) => toast.error(err instanceof Error ? err.message : t("loadFailed")))
      .finally(() => setLoading(false));
  }, [open, http, applyConfigs, t]);

  useEffect(() => { resetEmb(); }, [embProvider, embModel, resetEmb]);

  const embChanged = embProvider !== init.embProvider || embModel !== init.embModel;
  const embVerified = embResult?.valid === true;
  const saveDisabled = saving || (embChanged && !embVerified);
  const selectedEmbProviderData = providers.find((p) => p.name === embProvider);
  const embExtraModels = selectedEmbProviderData ? (EMBEDDING_MODELS[selectedEmbProviderData.provider_type] ?? DEFAULT_EMBEDDING_MODELS) : DEFAULT_EMBEDDING_MODELS;

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
      if (bgProvider !== init.bgProvider) updates["background.provider"] = bgProvider;
      if (bgModel !== init.bgModel) updates["background.model"] = bgModel;
      for (const [key, value] of Object.entries(updates)) await http.put(`/v1/system-configs/${key}`, { value });
      const kgChanged = kgProvider !== init.kgProvider || kgModel !== init.kgModel || kgMinConfidence !== init.kgMinConfidence;
      if (kgChanged) {
        await http.put("/v1/tools/builtin/knowledge_graph_search", {
          settings: { extraction_provider: kgProvider, extraction_model: kgModel, min_confidence: Number(kgMinConfidence) || 0.75, extract_on_memory_write: !!(kgProvider && kgModel) },
        });
      }
      toast.success(t("saved")); onOpenChange(false);
    } catch (err) { toast.error(err instanceof Error ? err.message : t("saveFailed")); }
    finally { setSaving(false); }
  };

  const uxItems: FeatureSwitchItem[] = [
    { icon: Eye, iconClass: "text-blue-500", label: t("ux.toolStatus"), hint: t("ux.toolStatusHint"), checked: toolStatus, onCheckedChange: setToolStatus, infoWhenOn: t("ux.toolStatusInfo"), infoClass: "border-blue-200 bg-blue-50 text-blue-700 dark:border-blue-800 dark:bg-blue-950/30 dark:text-blue-300" },
    { icon: MessageSquareText, iconClass: "text-emerald-500", label: t("ux.blockReply"), hint: t("ux.blockReplyHint"), checked: blockReply, onCheckedChange: setBlockReply, infoWhenOn: t("ux.blockReplyInfo"), infoClass: "border-emerald-200 bg-emerald-50 text-emerald-700 dark:border-emerald-800 dark:bg-emerald-950/30 dark:text-emerald-300" },
    { icon: Brain, iconClass: "text-orange-500", label: t("ux.intentClassify"), hint: t("ux.intentClassifyHint"), checked: intentClassify, onCheckedChange: setIntentClassify, infoWhenOn: t("ux.intentClassifyInfo"), infoClass: "border-orange-200 bg-orange-50 text-orange-700 dark:border-orange-800 dark:bg-orange-950/30 dark:text-orange-300" },
  ];

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="flex max-h-[90vh] w-[95vw] flex-col sm:max-w-3xl">
        <DialogHeader>
          <DialogTitle className="flex items-center gap-2">
            <Settings2 className="h-5 w-5" />{t("title")}
          </DialogTitle>
        </DialogHeader>

        {loading ? (
          <div className="flex flex-1 items-center justify-center py-12"><Loader2 className="h-6 w-6 animate-spin text-muted-foreground" /></div>
        ) : (
          <div className="min-h-0 flex-1 space-y-4 overflow-y-auto -mx-4 px-4 sm:-mx-6 sm:px-6">
            <SystemSettingsEmbeddingCard
              embProvider={embProvider} setEmbProvider={setEmbProvider}
              embModel={embModel} setEmbModel={setEmbModel}
              embMaxChunkLen={embMaxChunkLen} setEmbMaxChunkLen={setEmbMaxChunkLen}
              embChunkOverlap={embChunkOverlap} setEmbChunkOverlap={setEmbChunkOverlap}
              extraModels={embExtraModels}
              onVerify={() => { if (selectedEmbProviderData) verifyEmbedding(selectedEmbProviderData.id, embModel.trim() || undefined, 1536); }}
              verifying={embVerifying} verifyResult={embResult}
              canVerify={!!selectedEmbProviderData && !!embModel.trim()}
            />

            {/* Knowledge Graph */}
            <Card className="border-violet-200 dark:border-violet-800">
              <CardHeader>
                <CardTitle className="flex items-center gap-2 text-base"><Network className="h-4 w-4 text-violet-500" />{t("kg.title")}</CardTitle>
                <CardDescription>{t("kg.description")}</CardDescription>
              </CardHeader>
              <CardContent className="space-y-4 pt-0">
                <ProviderModelSelect provider={kgProvider} onProviderChange={(v) => { setKgProvider(v); setKgModel(""); }} model={kgModel} onModelChange={setKgModel} allowEmpty providerLabel={t("kg.provider")} modelLabel={t("kg.model")} providerTip={t("kg.providerTip")} modelTip={t("kg.modelTip")} providerPlaceholder={t("kg.providerPlaceholder")} modelPlaceholder={t("kg.modelPlaceholder")} />
                <div className="space-y-1.5">
                  <Label htmlFor="kgMinConf" className="text-xs">{t("kg.minConfidence")}</Label>
                  <Input id="kgMinConf" type="number" min={0} max={1} step={0.05} placeholder="0.75" value={kgMinConfidence} onChange={(e) => setKgMinConfidence(e.target.value)} className="max-w-[120px] text-base md:text-sm" />
                  <p className="text-xs text-muted-foreground">{t("kg.minConfidenceHint")}</p>
                </div>
                <div className="flex items-start gap-2 rounded-md border border-violet-200 bg-violet-50 px-3 py-2 text-xs text-violet-700 dark:border-violet-800 dark:bg-violet-950/30 dark:text-violet-300">
                  <Info className="mt-0.5 h-3.5 w-3.5 shrink-0" /><span>{t("kg.info")}</span>
                </div>
              </CardContent>
            </Card>

            {/* Background Workers */}
            <Card className="border-slate-200 dark:border-slate-700">
              <CardHeader>
                <CardTitle className="flex items-center gap-2 text-base"><Cog className="h-4 w-4 text-slate-500" />{t("bg.title")}</CardTitle>
                <CardDescription>{t("bg.description")}</CardDescription>
              </CardHeader>
              <CardContent className="space-y-4 pt-0">
                <ProviderModelSelect provider={bgProvider} onProviderChange={(v) => { setBgProvider(v); setBgModel(""); }} model={bgModel} onModelChange={setBgModel} allowEmpty providerLabel={t("bg.provider")} modelLabel={t("bg.model")} providerTip={t("bg.providerTip")} modelTip={t("bg.modelTip")} providerPlaceholder={t("bg.providerPlaceholder")} modelPlaceholder={t("bg.modelPlaceholder")} />
                <div className="flex items-start gap-2 rounded-md border border-slate-200 bg-slate-50 px-3 py-2 text-xs text-slate-700 dark:border-slate-700 dark:bg-slate-950/30 dark:text-slate-300">
                  <Info className="mt-0.5 h-3.5 w-3.5 shrink-0" /><span>{t("bg.info")}</span>
                </div>
              </CardContent>
            </Card>

            <FeatureSwitchGroup title={t("ux.title")} description={t("ux.description")} items={uxItems} />

            <SystemSettingsCompactionCard
              compProvider={compProvider} setCompProvider={setCompProvider}
              compModel={compModel} setCompModel={setCompModel}
              compThreshold={compThreshold} setCompThreshold={setCompThreshold}
              compKeepRecent={compKeepRecent} setCompKeepRecent={setCompKeepRecent}
              compMaxTokens={compMaxTokens} setCompMaxTokens={setCompMaxTokens}
            />
          </div>
        )}

        {/* Footer */}
        <div className="flex flex-col gap-3 border-t pt-4 shrink-0">
          {embChanged && !embVerified && (
            <p className="flex items-center gap-1.5 text-xs text-amber-600 dark:text-amber-400">
              <AlertTriangle className="h-3.5 w-3.5 shrink-0" />{t("embedding.verifyRequired")}
            </p>
          )}
          <div className="flex items-center justify-between gap-2">
            <Link to="/config" onClick={() => onOpenChange(false)} className="flex items-center gap-1 text-xs text-muted-foreground hover:text-foreground transition-colors">
              <ExternalLink className="h-3 w-3" />{t("moreConfig")}
            </Link>
            <div className="flex gap-2">
              <Button variant="outline" onClick={() => onOpenChange(false)} disabled={saving}>{t("cancel")}</Button>
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
