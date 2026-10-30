import { useState } from "react";
import { useTranslation } from "react-i18next";
import { Cpu, ShieldAlert, Loader2, CheckCircle2, XCircle } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Label } from "@/components/ui/label";
import { Input } from "@/components/ui/input";
import { Switch } from "@/components/ui/switch";
import { Card, CardContent, CardHeader, CardTitle, CardDescription } from "@/components/ui/card";
import { useHttp } from "@/hooks/use-ws";

export interface DecisionModelConfigState {
  enabled: boolean;
  endpoint: string;
  model: string;
  timeout_ms: number;
  max_state_chars: number;
  api_key: string;
}

export const DECISION_MODEL_DEFAULTS: DecisionModelConfigState = {
  enabled: false,
  endpoint: "https://api.typesafe.ai/v1/systemone",
  model: "jev-1.13.0",
  timeout_ms: 1000,
  max_state_chars: 4000,
  api_key: "",
};

interface VerifyResult {
  valid: boolean;
  model?: string;
  choice?: string;
  confidence?: number;
  latency_ms?: number;
  error?: string;
}

interface Props {
  value: DecisionModelConfigState;
  onChange: (patch: Partial<DecisionModelConfigState>) => void;
}

export function SystemSettingsDecisionModelCard({ value, onChange }: Props) {
  const { t } = useTranslation("system-settings");
  const http = useHttp();
  const [verifying, setVerifying] = useState(false);
  const [verifyResult, setVerifyResult] = useState<VerifyResult | null>(null);

  const handleVerify = async () => {
    setVerifying(true);
    setVerifyResult(null);
    try {
      const res = await http.post<VerifyResult>("/v1/decision-model/verify", {
        endpoint: value.endpoint || undefined,
        model: value.model || undefined,
        timeout_ms: value.timeout_ms || undefined,
        max_state_chars: value.max_state_chars || undefined,
        api_key: value.api_key || undefined,
      });
      setVerifyResult(res);
    } catch (err) {
      setVerifyResult({
        valid: false,
        error: err instanceof Error ? err.message : t("decisionModel.verifyFailed"),
      });
    } finally {
      setVerifying(false);
    }
  };

  return (
    <Card className="border-purple-200 dark:border-purple-800">
      <CardHeader>
        <div className="flex items-center justify-between">
          <div className="space-y-1">
            <CardTitle className="flex items-center gap-2 text-base">
              <Cpu className="h-4 w-4 text-purple-500" />
              {t("decisionModel.title")}
            </CardTitle>
            <CardDescription>{t("decisionModel.description")}</CardDescription>
          </div>
          <Switch
            checked={value.enabled}
            onCheckedChange={(enabled) => onChange({ enabled })}
            aria-label={t("decisionModel.enabled")}
          />
        </div>
      </CardHeader>
      <CardContent className="space-y-4 pt-0">
        {/* Egress warning is ALWAYS visible, even when disabled, so operators see the privacy boundary before opting in. */}
        <div className="flex items-start gap-2 rounded-md border border-amber-300 bg-amber-50 px-3 py-2 text-xs text-amber-800 dark:border-amber-800 dark:bg-amber-950/40 dark:text-amber-200">
          <ShieldAlert className="mt-0.5 h-4 w-4 shrink-0 text-amber-600 dark:text-amber-400" />
          <div className="space-y-1">
            <p className="font-semibold">{t("decisionModel.egressWarningTitle")}</p>
            <p className="opacity-90">{t("decisionModel.egressWarningBody")}</p>
          </div>
        </div>

        <div className="space-y-1.5">
          <Label htmlFor="dmEndpoint" className="text-xs">{t("decisionModel.endpoint")}</Label>
          <Input
            id="dmEndpoint"
            type="url"
            placeholder={DECISION_MODEL_DEFAULTS.endpoint}
            value={value.endpoint}
            onChange={(e) => onChange({ endpoint: e.target.value })}
            className="text-base md:text-sm font-mono"
          />
          <p className="text-xs text-muted-foreground">{t("decisionModel.endpointHint")}</p>
        </div>

        <div className="grid grid-cols-1 gap-4 sm:grid-cols-2">
          <div className="space-y-1.5">
            <Label htmlFor="dmModel" className="text-xs">{t("decisionModel.model")}</Label>
            <Input
              id="dmModel"
              placeholder={DECISION_MODEL_DEFAULTS.model}
              value={value.model}
              onChange={(e) => onChange({ model: e.target.value })}
              className="text-base md:text-sm font-mono"
            />
            <p className="text-xs text-muted-foreground">{t("decisionModel.modelHint")}</p>
          </div>
          <div className="space-y-1.5">
            <Label htmlFor="dmAPIKey" className="text-xs">{t("decisionModel.apiKey")}</Label>
            <Input
              id="dmAPIKey"
              type="password"
              placeholder={value.api_key === "***" ? "***" : t("decisionModel.apiKeyPlaceholder")}
              value={value.api_key}
              onChange={(e) => onChange({ api_key: e.target.value })}
              className="text-base md:text-sm font-mono"
              autoComplete="new-password"
            />
            <p className="text-xs text-muted-foreground">{t("decisionModel.apiKeyHint")}</p>
          </div>
        </div>

        <div className="grid grid-cols-1 gap-4 sm:grid-cols-2">
          <div className="space-y-1.5">
            <Label htmlFor="dmTimeout" className="text-xs">{t("decisionModel.timeout")}</Label>
            <Input
              id="dmTimeout"
              type="number"
              min={100}
              max={5000}
              step={100}
              placeholder="1000"
              value={value.timeout_ms || ""}
              onChange={(e) => onChange({ timeout_ms: Number(e.target.value) || 1000 })}
              className="text-base md:text-sm"
            />
            <p className="text-xs text-muted-foreground">{t("decisionModel.timeoutHint")}</p>
          </div>
          <div className="space-y-1.5">
            <Label htmlFor="dmMaxChars" className="text-xs">{t("decisionModel.maxStateChars")}</Label>
            <Input
              id="dmMaxChars"
              type="number"
              min={200}
              max={20000}
              step={500}
              placeholder="4000"
              value={value.max_state_chars || ""}
              onChange={(e) => onChange({ max_state_chars: Number(e.target.value) || 4000 })}
              className="text-base md:text-sm"
            />
            <p className="text-xs text-muted-foreground">{t("decisionModel.maxStateCharsHint")}</p>
          </div>
        </div>

        <div className="flex items-center gap-3 pt-1">
          <Button
            type="button"
            variant="outline"
            size="sm"
            disabled={verifying}
            onClick={handleVerify}
          >
            {verifying ? <Loader2 className="mr-1.5 h-3.5 w-3.5 animate-spin" /> : null}
            {t("decisionModel.verify")}
          </Button>
          {verifyResult && (
            <span
              className={`flex items-center gap-1 text-xs ${
                verifyResult.valid ? "text-emerald-600 dark:text-emerald-400" : "text-destructive"
              }`}
            >
              {verifyResult.valid ? (
                <>
                  <CheckCircle2 className="h-3.5 w-3.5" />
                  {t("decisionModel.verifySuccess", {
                    choice: verifyResult.choice,
                    confidence: Math.round((verifyResult.confidence ?? 0) * 100),
                    latency: verifyResult.latency_ms,
                  })}
                </>
              ) : (
                <>
                  <XCircle className="h-3.5 w-3.5" />
                  {verifyResult.error || t("decisionModel.verifyFailed")}
                </>
              )}
            </span>
          )}
        </div>
      </CardContent>
    </Card>
  );
}
