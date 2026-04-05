import { useState, useEffect, useRef } from "react";
import { CheckCircle2 } from "lucide-react";
import { useTranslation } from "react-i18next";
import { Button } from "@/components/ui/button";
import { Label } from "@/components/ui/label";
import { Badge } from "@/components/ui/badge";
import { Card, CardContent } from "@/components/ui/card";
import { Combobox } from "@/components/ui/combobox";
import { TooltipProvider } from "@/components/ui/tooltip";
import { InfoTip } from "@/pages/setup/info-tip";
import { useProviderModels } from "@/pages/providers/hooks/use-provider-models";
import { useProviderVerify } from "@/pages/providers/hooks/use-provider-verify";
import type { ProviderData } from "@/types/provider";

const VERIFY_TIMEOUT_SECS = 30;

interface StepModelProps {
  provider: ProviderData;
  onComplete: (model: string) => void;
  onBack?: () => void;
  initialModel?: string | null;
}

export function StepModel({ provider, onComplete, onBack, initialModel }: StepModelProps) {
  const { t } = useTranslation("setup");
  const { models, loading: modelsLoading } = useProviderModels(provider.id);
  const { verify, verifying, result: verifyResult, reset: resetVerify } = useProviderVerify();

  const [model, setModel] = useState(initialModel ?? "");
  const [error, setError] = useState("");
  const [countdown, setCountdown] = useState(0);
  const timerRef = useRef<ReturnType<typeof setInterval> | null>(null);

  // Reset verification when model changes
  useEffect(() => { resetVerify(); setError(""); }, [model, resetVerify]);

  // Countdown timer — starts when verifying, stops on result
  useEffect(() => {
    if (verifying) {
      setCountdown(VERIFY_TIMEOUT_SECS);
      timerRef.current = setInterval(() => {
        setCountdown((prev) => (prev <= 1 ? 0 : prev - 1));
      }, 1000);
    } else {
      setCountdown(0);
      if (timerRef.current) {
        clearInterval(timerRef.current);
        timerRef.current = null;
      }
    }
    return () => {
      if (timerRef.current) {
        clearInterval(timerRef.current);
        timerRef.current = null;
      }
    };
  }, [verifying]);

  const isVerified = verifyResult?.valid === true;

  const handleVerify = async () => {
    if (!model.trim()) return;
    setError("");
    const res = await verify(provider.id, model.trim());
    if (!res?.valid) {
      setError(res?.error || t("model.verificationFailed"));
    }
  };

  const providerLabel = provider.display_name || provider.name;

  const verifyButtonLabel = verifying
    ? `${t("model.verifying")} (${countdown}s)`
    : isVerified
      ? t("model.verified")
      : t("model.verify");

  return (
    <Card className="py-0 gap-0">
      <CardContent className="space-y-4 px-6 py-5">
        <TooltipProvider>
          <div className="space-y-1">
            <h2 className="text-lg font-semibold">{t("model.title")}</h2>
            <p className="text-sm text-muted-foreground">
              {t("model.description")}
            </p>
          </div>

          {/* Provider summary */}
          <div className="flex items-center gap-2">
            <span className="text-sm text-muted-foreground">{t("model.provider")}</span>
            <Badge variant="secondary">{providerLabel}</Badge>
          </div>

          <div className="space-y-2">
            <Label className="inline-flex items-center gap-1.5">
              {t("model.model")}
              <InfoTip text={t("model.modelHint")} />
            </Label>
            <Combobox
              value={model}
              onChange={setModel}
              options={models.map((m) => ({ value: m.id, label: m.name || m.id }))}
              placeholder={modelsLoading ? t("model.loadingModels") : t("model.selectModel")}
            />
            {!modelsLoading && models.length === 0 && (
              <p className="text-xs text-muted-foreground">
                {t("model.noModelsHint")}
              </p>
            )}
          </div>

          {error && <p className="text-sm text-destructive">{error}</p>}

          {isVerified && (
            <div className="flex items-center gap-2 rounded-md border border-emerald-200 bg-emerald-50 p-3 dark:border-emerald-900 dark:bg-emerald-950">
              <CheckCircle2 className="h-5 w-5 text-emerald-600 dark:text-emerald-400" />
              <div>
                <p className="text-sm font-medium text-emerald-700 dark:text-emerald-300">{t("model.modelVerified")}</p>
                <p className="text-xs text-muted-foreground">
                  {t("model.modelVerifiedDesc", { model })}
                </p>
              </div>
            </div>
          )}

          <div className={`flex ${onBack ? "justify-between" : "justify-end"} gap-2`}>
            {onBack && (
              <Button variant="secondary" onClick={onBack}>
                ← {t("common.back")}
              </Button>
            )}
            <div className="flex gap-2">
              <Button
                variant="outline"
                onClick={handleVerify}
                disabled={!model.trim() || verifying || isVerified}
              >
                {verifyButtonLabel}
              </Button>
              <Button onClick={() => onComplete(model.trim())} disabled={!isVerified}>
                {t("model.continue")}
              </Button>
            </div>
          </div>
        </TooltipProvider>
      </CardContent>
    </Card>
  );
}
