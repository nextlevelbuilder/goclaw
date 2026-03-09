import { useState, useMemo } from "react";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Card, CardContent } from "@/components/ui/card";
import { TooltipProvider } from "@/components/ui/tooltip";
import { InfoTip } from "@/pages/setup/info-tip";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { PROVIDER_TYPES } from "@/constants/providers";
import { useProviders } from "@/pages/providers/hooks/use-providers";
import { CLISection } from "@/pages/providers/provider-cli-section";
import { slugify } from "@/lib/slug";
import type { ProviderData } from "@/types/provider";

interface StepProviderProps {
  onComplete: (provider: ProviderData) => void;
}

export function StepProvider({ onComplete }: StepProviderProps) {
  const { createProvider } = useProviders();

  const [providerType, setProviderType] = useState("openrouter");
  const [name, setName] = useState("openrouter");
  const [apiKey, setApiKey] = useState("");
  const [apiBase, setApiBase] = useState("https://openrouter.ai/api/v1");
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState("");

  const isCLI = providerType === "claude_cli";

  const handleTypeChange = (value: string) => {
    setProviderType(value);
    const preset = PROVIDER_TYPES.find((t) => t.value === value);
    setName(slugify(value));
    setApiBase(preset?.apiBase || "");
    setApiKey("");
    setError("");
  };

  const apiBasePlaceholder = useMemo(
    () => PROVIDER_TYPES.find((t) => t.value === providerType)?.placeholder
      || PROVIDER_TYPES.find((t) => t.value === providerType)?.apiBase
      || "https://api.example.com/v1",
    [providerType],
  );

  const handleCreate = async () => {
    if (!isCLI && !apiKey.trim()) { setError("API key is required"); return; }
    setLoading(true);
    setError("");
    try {
      const provider = await createProvider({
        name: name.trim(),
        provider_type: providerType,
        api_base: apiBase.trim() || undefined,
        api_key: isCLI ? undefined : apiKey.trim(),
        enabled: true,
      }) as ProviderData;
      onComplete(provider);
    } catch (err) {
      setError(err instanceof Error ? err.message : "Failed to create provider");
    } finally {
      setLoading(false);
    }
  };

  return (
    <Card>
      <CardContent className="space-y-4 pt-6">
        <TooltipProvider>
          <div className="space-y-1">
            <h2 className="text-lg font-semibold">Configure LLM Provider</h2>
            <p className="text-sm text-muted-foreground">
              {isCLI
                ? "Connect using your local Claude CLI installation. No API key needed."
                : "Connect to an AI provider to power your agents. You'll need an API key."}
            </p>
          </div>

          <div className="grid grid-cols-1 gap-4 sm:grid-cols-2">
            <div className="space-y-2">
              <Label className="inline-flex items-center gap-1.5">
                Provider Type
                <InfoTip text="The LLM service you want to connect. OpenRouter is recommended for access to multiple models." />
              </Label>
              <Select value={providerType} onValueChange={handleTypeChange}>
                <SelectTrigger><SelectValue /></SelectTrigger>
                <SelectContent>
                  {PROVIDER_TYPES.map((t) => (
                    <SelectItem key={t.value} value={t.value}>{t.label}</SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </div>
            <div className="space-y-2">
              <Label className="inline-flex items-center gap-1.5">
                Name
                <InfoTip text="Internal identifier for this provider. Auto-generated from provider type." />
              </Label>
              <Input value={name} onChange={(e) => setName(slugify(e.target.value))} />
            </div>
          </div>

          {isCLI ? (
            <CLISection open={true} />
          ) : (
            <>
              <div className="space-y-2">
                <Label className="inline-flex items-center gap-1.5">
                  API Key *
                  <InfoTip text="Your provider's secret key. Encrypted server-side and never exposed in API responses." />
                </Label>
                <Input
                  type="password"
                  value={apiKey}
                  onChange={(e) => setApiKey(e.target.value)}
                  placeholder="sk-..."
                />
              </div>

              <div className="space-y-2">
                <Label className="inline-flex items-center gap-1.5">
                  API Base URL
                  <InfoTip text="The endpoint URL for API requests. Auto-filled based on provider type. Override only if using a custom proxy." />
                </Label>
                <Input
                  value={apiBase}
                  onChange={(e) => setApiBase(e.target.value)}
                  placeholder={apiBasePlaceholder}
                />
              </div>
            </>
          )}

          {error && <p className="text-sm text-destructive">{error}</p>}

          <div className="flex justify-end">
            <Button onClick={handleCreate} disabled={loading || (!isCLI && !apiKey.trim())}>
              {loading ? "Creating..." : "Create Provider"}
            </Button>
          </div>
        </TooltipProvider>
      </CardContent>
    </Card>
  );
}
