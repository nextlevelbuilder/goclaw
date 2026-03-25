import { useState, useEffect } from "react";
import { useTranslation } from "react-i18next";
import { Copy, Loader2, CheckCircle2, XCircle, AlertTriangle } from "lucide-react";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Switch } from "@/components/ui/switch";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { StickySaveBar } from "@/components/shared/sticky-save-bar";
import { PROVIDER_TYPES } from "@/constants/providers";
import { toast } from "@/stores/use-toast-store";
import { useProviderVerify } from "../hooks/use-provider-verify";
import { getEmbeddingSettings } from "@/types/provider";
import type { ProviderData, ProviderInput } from "@/types/provider";

interface ProviderOverviewProps {
  provider: ProviderData;
  onUpdate: (id: string, data: ProviderInput) => Promise<void>;
}

const NO_API_KEY_TYPES = new Set(["claude_cli", "acp", "chatgpt_oauth"]);
const NO_EMBEDDING_TYPES = new Set(["claude_cli", "acp", "chatgpt_oauth", "suno", "anthropic_native"]);

export function ProviderOverview({ provider, onUpdate }: ProviderOverviewProps) {
  const { t } = useTranslation("providers");
  const { t: tc } = useTranslation("common");

  const typeInfo = PROVIDER_TYPES.find((pt) => pt.value === provider.provider_type);
  const typeLabel = typeInfo?.label ?? provider.provider_type;
  const showApiKey = !NO_API_KEY_TYPES.has(provider.provider_type);
  const showEmbedding = !NO_EMBEDDING_TYPES.has(provider.provider_type);

  // Identity
  const [displayName, setDisplayName] = useState(provider.display_name || "");

  // API Key
  const [apiKey, setApiKey] = useState(provider.api_key || "");

  // Status
  const [enabled, setEnabled] = useState(provider.enabled);

  // Embedding
  const initEmb = getEmbeddingSettings(provider.settings);
  const [embEnabled, setEmbEnabled] = useState(initEmb?.enabled ?? false);
  const [embModel, setEmbModel] = useState(initEmb?.model ?? "");
  const [embApiBase, setEmbApiBase] = useState(initEmb?.api_base ?? "");
  const [embDimensions, setEmbDimensions] = useState(initEmb?.dimensions ? String(initEmb.dimensions) : "");

  // Re-sync embedding state when provider changes (e.g. after save)
  useEffect(() => {
    const es = getEmbeddingSettings(provider.settings);
    setEmbEnabled(es?.enabled ?? false);
    setEmbModel(es?.model ?? "");
    setEmbApiBase(es?.api_base ?? "");
    setEmbDimensions(es?.dimensions ? String(es.dimensions) : "");
  }, [provider.settings]);

  // Verify embedding
  const { verifyEmbedding, embVerifying, embResult, resetEmb } = useProviderVerify();
  useEffect(() => { resetEmb(); }, [embModel, embDimensions, resetEmb]);

  // Save state
  const [saving, setSaving] = useState(false);

  const handleSave = async () => {
    setSaving(true);
    try {
      const data: ProviderInput = {
        name: provider.name,
        display_name: displayName.trim() || undefined,
        provider_type: provider.provider_type,
        enabled,
      };
      // Only include api_key if changed from the masked value
      if (showApiKey && apiKey && apiKey !== "***") {
        data.api_key = apiKey;
      }
      // Merge embedding settings into existing settings
      if (showEmbedding) {
        const existing = (provider.settings || {}) as Record<string, unknown>;
        const parsedDims = embDimensions ? parseInt(embDimensions, 10) : 0;
        data.settings = {
          ...existing,
          embedding: embEnabled
            ? {
                enabled: true,
                model: embModel.trim() || undefined,
                api_base: embApiBase.trim() || undefined,
                dimensions: parsedDims > 0 ? parsedDims : undefined,
              }
            : { enabled: false },
        };
      }
      await onUpdate(provider.id, data);
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

  return (
    <div className="space-y-4">
      {/* Identity */}
      <section className="space-y-4 rounded-lg border p-3 sm:p-4 overflow-hidden">
        <h3 className="text-sm font-medium">{t("detail.identity")}</h3>

        <div className="space-y-2">
          <Label htmlFor="displayName">{t("form.displayName")}</Label>
          <Input
            id="displayName"
            value={displayName}
            onChange={(e) => setDisplayName(e.target.value)}
            placeholder={t("form.displayNamePlaceholder")}
            className="text-base md:text-sm"
          />
        </div>

        <div className="space-y-2">
          <Label>{t("detail.providerType")}</Label>
          <div className="flex items-center gap-2">
            <Badge variant="outline">{typeLabel}</Badge>
          </div>
        </div>

        <div className="space-y-2">
          <Label>{t("form.name")}</Label>
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

      {/* API Key */}
      {showApiKey && (
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
      )}

      {/* Embedding */}
      {showEmbedding && (
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

          {embEnabled && (
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
                {embResult && (
                  <span className={`flex items-center gap-1 text-xs ${
                    embResult.valid
                      ? embResult.dimension_mismatch
                        ? "text-amber-600 dark:text-amber-400"
                        : "text-success"
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
                          : `${embResult.dimensions} dimensions`}
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
            </div>
          )}
        </section>
      )}

      {/* Status */}
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
      />
    </div>
  );
}
