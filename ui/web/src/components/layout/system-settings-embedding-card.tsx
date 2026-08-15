import { useTranslation } from "react-i18next";
import { Brain, Info, Loader2, AlertTriangle, CheckCircle2, XCircle } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Label } from "@/components/ui/label";
import { Input } from "@/components/ui/input";
import { Card, CardContent, CardHeader, CardTitle, CardDescription } from "@/components/ui/card";
import { ProviderModelSelect } from "@/components/shared/provider-model-select";

interface EmbVerifyResult {
  valid: boolean;
  dimensions?: number;
  dimension_mismatch?: boolean;
  error?: string;
}

interface SystemSettingsEmbeddingCardProps {
  embProvider: string;
  setEmbProvider: (v: string) => void;
  embModel: string;
  setEmbModel: (v: string) => void;
  embMaxChunkLen: string;
  setEmbMaxChunkLen: (v: string) => void;
  embChunkOverlap: string;
  setEmbChunkOverlap: (v: string) => void;
  extraModels: { id: string; name: string }[];
  onVerify: () => void;
  verifying: boolean;
  verifyResult: EmbVerifyResult | null;
  canVerify: boolean;
}

export function SystemSettingsEmbeddingCard({
  embProvider, setEmbProvider,
  embModel, setEmbModel,
  embMaxChunkLen, setEmbMaxChunkLen,
  embChunkOverlap, setEmbChunkOverlap,
  extraModels, onVerify, verifying, verifyResult, canVerify,
}: SystemSettingsEmbeddingCardProps) {
  const { t } = useTranslation("system-settings");

  return (
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
          extraModels={extraModels}
          modelFilter="embed"
          providerLabel={t("embedding.provider")}
          modelLabel={t("embedding.model")}
          providerTip={t("embedding.providerTip")}
          modelTip={t("embedding.modelTip")}
          providerPlaceholder={t("embedding.providerPlaceholder")}
          modelPlaceholder={t("embedding.modelPlaceholder")}
        />

        <div className="flex items-center gap-3">
          <Button type="button" variant="outline" size="sm" disabled={!canVerify || verifying} onClick={onVerify}>
            {verifying ? <Loader2 className="mr-1.5 h-3.5 w-3.5 animate-spin" /> : null}
            {t("embedding.verify")}
          </Button>
          {verifyResult && (
            <span className={`flex items-center gap-1 text-xs ${
              verifyResult.valid
                ? verifyResult.dimension_mismatch ? "text-amber-600 dark:text-amber-400" : "text-emerald-600 dark:text-emerald-400"
                : "text-destructive"
            }`}>
              {verifyResult.valid ? (
                <>
                  {verifyResult.dimension_mismatch ? <AlertTriangle className="h-3.5 w-3.5" /> : <CheckCircle2 className="h-3.5 w-3.5" />}
                  {verifyResult.dimension_mismatch
                    ? t("embedding.dimensionsMismatch", { count: verifyResult.dimensions })
                    : t("embedding.dimensions", { count: verifyResult.dimensions })}
                </>
              ) : (
                <><XCircle className="h-3.5 w-3.5" />{verifyResult.error || t("embedding.verifyFailed")}</>
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
  );
}
