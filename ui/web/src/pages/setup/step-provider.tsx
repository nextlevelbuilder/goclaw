import { useMemo, useState } from "react";
import { useTranslation } from "react-i18next";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Card, CardContent } from "@/components/ui/card";
import { TooltipProvider } from "@/components/ui/tooltip";
import { InfoTip } from "@/pages/setup/info-tip";
import { useHttp } from "@/hooks/use-ws";
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
import { OAuthSection } from "@/pages/providers/provider-oauth-section";
import { slugify } from "@/lib/slug";
import type { ProviderData, ProviderInput } from "@/types/provider";

interface StepProviderProps {
  onComplete: (provider: ProviderData) => void;
  existingProvider?: ProviderData | null;
}

export function StepProvider({ onComplete, existingProvider }: StepProviderProps) {
  const { t } = useTranslation("setup");
  const http = useHttp();
  const { createProvider, updateProvider } = useProviders();

  const isEditing = !!existingProvider;

  const [providerType, setProviderType] = useState(existingProvider?.provider_type ?? "openrouter");
  const [name, setName] = useState(existingProvider?.name ?? "openrouter");
  const [apiKey, setApiKey] = useState("");
  const [apiBase, setApiBase] = useState(
    existingProvider?.api_base ?? "https://openrouter.ai/api/v1",
  );
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState("");

  const isOAuth = providerType === "chatgpt_oauth";
  const isCLI = providerType === "claude_cli";
  // Local Ollama uses no API key — the server accepts any non-empty Bearer value internally
  const isOllama = providerType === "ollama";

  const handleTypeChange = (value: string) => {
    setProviderType(value);
    const preset = PROVIDER_TYPES.find((t) => t.value === value);
    setName(value === "chatgpt_oauth" ? "openai-codex" : slugify(value));
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

  const handleOAuthSuccess = async () => {
    setLoading(true);
    setError("");
    try {
      const res = await http.get<{ providers: ProviderData[] }>("/v1/providers");
      const provider = res.providers?.find((p) => p.provider_type === "chatgpt_oauth" && p.name === "openai-codex");
      if (!provider) {
        setError(t("provider.errors.oauthProviderNotFound"));
        return;
      }
      onComplete(provider);
    } catch (err) {
      setError(err instanceof Error ? err.message : t("provider.errors.oauthProviderNotFound"));
    } finally {
      setLoading(false);
    }
  };

  const handleSubmit = async () => {
    if (isOAuth) return;
    if (!isEditing && !isCLI && !isOllama && !apiKey.trim()) { setError(t("provider.errors.apiKeyRequired")); return; }
    setLoading(true);
    setError("");
    try {
      if (isEditing) {
        const patch: Record<string, unknown> = {
          name: name.trim(),
          provider_type: providerType,
          api_base: apiBase.trim() || undefined,
        };
        // Only include api_key if user entered a new one
        if (apiKey.trim()) patch.api_key = apiKey.trim();
        await updateProvider(existingProvider!.id, patch as Partial<ProviderInput>);
        onComplete({ ...existingProvider!, ...patch } as ProviderData);
      } else {
        const provider = await createProvider({
          name: name.trim(),
          provider_type: providerType,
          api_base: apiBase.trim() || undefined,
          api_key: isCLI || isOllama || isOAuth ? undefined : apiKey.trim(),
          enabled: true,
        }) as ProviderData;
        onComplete(provider);
      }
    } catch (err) {
      setError(err instanceof Error ? err.message : t("provider.errors.failedCreate"));
    } finally {
      setLoading(false);
    }
  };

  return (
    <Card className="py-0 gap-0">
      <CardContent className="space-y-4 px-6 py-5">
        <TooltipProvider>
          <div className="space-y-1">
            <h2 className="text-lg font-semibold">{t("provider.title")}</h2>
            <p className="text-sm text-muted-foreground">
              {isOAuth
                ? t("provider.descriptionOauth")
                : isCLI
                ? t("provider.descriptionCli")
                : t("provider.description")}
            </p>
          </div>

          <div className="grid grid-cols-1 gap-4 sm:grid-cols-2">
            <div className="space-y-2">
              <Label className="inline-flex items-center gap-1.5">
                {t("provider.providerType")}
                <InfoTip text={t("provider.providerTypeHint")} />
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
                {t("provider.name")}
                <InfoTip text={t("provider.nameHint")} />
              </Label>
              <Input
                value={name}
                onChange={(e) => setName(slugify(e.target.value))}
                disabled={isOAuth}
              />
            </div>
          </div>

          {isOAuth ? (
            <OAuthSection
              onSuccess={handleOAuthSuccess}
              authenticatedActionLabel={t("model.continue")}
            />
          ) : isCLI ? (
            <CLISection open={true} />
          ) : (
            <>
              <div className="space-y-2">
                <Label className="inline-flex items-center gap-1.5">
                  {t("provider.apiKey")}
                  <InfoTip text={t("provider.apiKeyHint")} />
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
                  {t("provider.apiBase")}
                  <InfoTip text={t("provider.apiBaseHint")} />
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

          {!isOAuth && (
            <div className="flex justify-end">
              <Button onClick={handleSubmit} disabled={loading || (!isEditing && !isCLI && !isOllama && !apiKey.trim())}>
                {loading
                  ? isEditing ? t("provider.updating", "Updating...") : t("provider.creating")
                  : isEditing ? t("provider.update", "Update") : t("provider.create")}
              </Button>
            </div>
          )}
        </TooltipProvider>
      </CardContent>
    </Card>
  );
}
