import { useState, useEffect } from "react";
import { CheckCircle2 } from "lucide-react";
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

interface StepModelProps {
  provider: ProviderData;
  onComplete: (model: string) => void;
}

export function StepModel({ provider, onComplete }: StepModelProps) {
  const { models, loading: modelsLoading } = useProviderModels(provider.id);
  const { verify, verifying, result: verifyResult, reset: resetVerify } = useProviderVerify();

  const [model, setModel] = useState("");
  const [error, setError] = useState("");

  // Reset verification when model changes
  useEffect(() => { resetVerify(); setError(""); }, [model, resetVerify]);

  const isVerified = verifyResult?.valid === true;

  const handleVerify = async () => {
    if (!model.trim()) return;
    setError("");
    const res = await verify(provider.id, model.trim());
    if (!res?.valid) {
      setError(res?.error || "Verification failed. Check your API key and model.");
    }
  };

  const providerLabel = provider.display_name || provider.name;

  return (
    <Card>
      <CardContent className="space-y-4 pt-6">
        <TooltipProvider>
          <div className="space-y-1">
            <h2 className="text-lg font-semibold">Select & Verify Model</h2>
            <p className="text-sm text-muted-foreground">
              Choose a model and verify that your provider connection works.
            </p>
          </div>

          {/* Provider summary */}
          <div className="flex items-center gap-2">
            <span className="text-sm text-muted-foreground">Provider:</span>
            <Badge variant="secondary">{providerLabel}</Badge>
          </div>

          <div className="space-y-2">
            <Label className="inline-flex items-center gap-1.5">
              Model *
              <InfoTip text="The AI model to use. Select from the list or type a model ID manually if not listed." />
            </Label>
            <Combobox
              value={model}
              onChange={setModel}
              options={models.map((m) => ({ value: m.id, label: m.name || m.id }))}
              placeholder={modelsLoading ? "Loading models..." : "Select or type a model ID"}
            />
            {!modelsLoading && models.length === 0 && (
              <p className="text-xs text-muted-foreground">
                This provider doesn't list models — type the model ID manually.
              </p>
            )}
          </div>

          {error && <p className="text-sm text-destructive">{error}</p>}

          {isVerified && (
            <div className="flex items-center gap-2 rounded-md border border-emerald-200 bg-emerald-50 p-3 dark:border-emerald-900 dark:bg-emerald-950">
              <CheckCircle2 className="h-5 w-5 text-emerald-600 dark:text-emerald-400" />
              <div>
                <p className="text-sm font-medium text-emerald-700 dark:text-emerald-300">Model verified</p>
                <p className="text-xs text-muted-foreground">
                  <code className="font-mono">{model}</code> is working correctly
                </p>
              </div>
            </div>
          )}

          <div className="flex justify-end gap-2">
            <Button
              variant="outline"
              onClick={handleVerify}
              disabled={!model.trim() || verifying || isVerified}
            >
              {verifying ? "Verifying..." : isVerified ? "Verified" : "Verify"}
            </Button>
            <Button onClick={() => onComplete(model.trim())} disabled={!isVerified}>
              Continue
            </Button>
          </div>
        </TooltipProvider>
      </CardContent>
    </Card>
  );
}
