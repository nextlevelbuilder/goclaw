import { useTranslation } from "react-i18next";
import { Loader2, CheckCircle2, XCircle, AlertTriangle } from "lucide-react";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Switch } from "@/components/ui/switch";
import { Button } from "@/components/ui/button";

import type { VerifyResult } from "../hooks/use-provider-verify";

interface ProviderEmbeddingSectionProps {
  embEnabled: boolean;
  setEmbEnabled: (v: boolean) => void;
  embModel: string;
  setEmbModel: (v: string) => void;
  embApiBase: string;
  setEmbApiBase: (v: string) => void;
  onVerify: () => void;
  verifying: boolean;
  verifyResult: VerifyResult | null;
}

export function ProviderEmbeddingSection({
  embEnabled,
  setEmbEnabled,
  embModel,
  setEmbModel,
  embApiBase,
  setEmbApiBase,
  onVerify,
  verifying,
  verifyResult,
}: ProviderEmbeddingSectionProps) {
  const { t } = useTranslation("providers");

  return (
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
              disabled={verifying}
              onClick={onVerify}
            >
              {verifying ? <Loader2 className="mr-1.5 h-3.5 w-3.5 animate-spin" /> : null}
              {t("embedding.verify")}
            </Button>
            {verifyResult ? (
              <span
                className={`flex items-center gap-1 text-xs ${
                  verifyResult.valid
                    ? verifyResult.dimension_mismatch
                      ? "text-amber-600 dark:text-amber-400"
                      : "text-success"
                    : "text-destructive"
                }`}
              >
                {verifyResult.valid ? (
                  <>
                    {verifyResult.dimension_mismatch ? (
                      <AlertTriangle className="h-3.5 w-3.5" />
                    ) : (
                      <CheckCircle2 className="h-3.5 w-3.5" />
                    )}
                    {verifyResult.dimension_mismatch
                      ? t("embedding.dimensionsMismatch", { count: verifyResult.dimensions })
                      : `${verifyResult.dimensions} dimensions`}
                  </>
                ) : (
                  <>
                    <XCircle className="h-3.5 w-3.5" />
                    {verifyResult.error || t("embedding.verifyFailed")}
                  </>
                )}
              </span>
            ) : null}
          </div>
        </div>
      ) : null}
    </section>
  );
}
