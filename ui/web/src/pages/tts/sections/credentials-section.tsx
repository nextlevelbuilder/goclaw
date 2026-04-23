/**
 * Credentials section — Step 2 of the TTS configuration flow.
 * Renders provider-specific credential inputs (API key, base URL, group ID).
 * Edge TTS is skipped (requiresApiKey = false).
 * Includes a "Test connection" button that calls synthesize() with a short sample.
 */
import { useState } from "react";
import { useTranslation } from "react-i18next";
import { FlaskConical, Save } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { toast } from "@/stores/use-toast-store";
import type { TtsConfig, TtsProviderConfig, TestConnectionParams, TestConnectionResult } from "../hooks/use-tts-config";

interface Props {
  provider: string;
  draft: TtsConfig;
  onUpdate: (
    providerKey: keyof Pick<TtsConfig, "openai" | "elevenlabs" | "edge" | "minimax" | "gemini">,
    patch: Partial<TtsProviderConfig>,
  ) => void;
  testConnection: (params: TestConnectionParams) => Promise<TestConnectionResult>;
  onSave: () => Promise<void>;
  saving?: boolean;
  dirty?: boolean;
}

export function CredentialsSection({ provider, draft, onUpdate, testConnection, onSave, saving, dirty }: Props) {
  const { t } = useTranslation("tts");
  const [testing, setTesting] = useState(false);

  // Edge TTS is free — no credentials to configure
  if (!provider || provider === "edge") return null;

  const handleTestConnection = async () => {
    setTesting(true);
    try {
      // Build params from draft credentials — test with unsaved config
      const cfg = draft[provider as keyof Pick<typeof draft, "openai" | "elevenlabs" | "minimax" | "gemini">];
      // Don't send masked API key — backend will reject it
      const apiKey = cfg?.api_key === "***" ? undefined : cfg?.api_key;
      const params: TestConnectionParams = {
        provider,
        api_key: apiKey,
        api_base: cfg?.api_base || cfg?.base_url,
        voice_id: cfg?.voice_id || cfg?.voice,
        model_id: cfg?.model_id || cfg?.model,
        group_id: (cfg as { group_id?: string })?.group_id,
      };
      const result = await testConnection(params);
      toast.success(t("testConnection.success", "Connection successful"), `${result.latency_ms}ms`);
    } catch (err) {
      const msg = err instanceof Error ? err.message : String(err);
      toast.error(t("testConnection.failed", "Connection failed"), msg);
    } finally {
      setTesting(false);
    }
  };

  return (
    <Card className="gap-3">
      <CardHeader>
        <CardTitle className="text-base">
          2. {t("providerSettings", { provider: t(`providers.${provider}`) })}
        </CardTitle>
      </CardHeader>
      <CardContent className="space-y-4">
        {provider === "openai" && (
          <>
            <div className="grid gap-1.5">
              <Label htmlFor="oai-key">{t("openai.apiKey")}</Label>
              <Input id="oai-key" type="password" className="text-base md:text-sm"
                value={draft.openai.api_key ?? ""}
                onChange={(e) => onUpdate("openai", { api_key: e.target.value })}
                placeholder="sk-..." />
            </div>
            <div className="grid gap-1.5">
              <Label htmlFor="oai-base">{t("openai.apiBase")}</Label>
              <Input id="oai-base" className="text-base md:text-sm"
                value={draft.openai.api_base ?? ""}
                onChange={(e) => onUpdate("openai", { api_base: e.target.value })}
                placeholder="https://api.openai.com/v1" />
            </div>
          </>
        )}

        {provider === "elevenlabs" && (
          <>
            <div className="grid gap-1.5">
              <Label htmlFor="el-key">{t("elevenlabs.apiKey")}</Label>
              <Input id="el-key" type="password" className="text-base md:text-sm"
                value={draft.elevenlabs.api_key ?? ""}
                onChange={(e) => onUpdate("elevenlabs", { api_key: e.target.value })}
                placeholder="xi-..." />
            </div>
            <div className="grid gap-1.5">
              <Label htmlFor="el-base">{t("elevenlabs.baseUrl")}</Label>
              <Input id="el-base" className="text-base md:text-sm"
                value={draft.elevenlabs.base_url ?? ""}
                onChange={(e) => onUpdate("elevenlabs", { base_url: e.target.value })}
                placeholder="https://api.elevenlabs.io" />
            </div>
          </>
        )}

        {provider === "minimax" && (
          <>
            <div className="grid gap-1.5">
              <Label htmlFor="mm-key">{t("minimax.apiKey")}</Label>
              <Input id="mm-key" type="password" className="text-base md:text-sm"
                value={draft.minimax.api_key ?? ""}
                onChange={(e) => onUpdate("minimax", { api_key: e.target.value })}
                placeholder="eyJh..." />
            </div>
            <div className="grid gap-1.5">
              <Label htmlFor="mm-group">{t("minimax.groupId")}</Label>
              <Input id="mm-group" className="text-base md:text-sm"
                value={draft.minimax.group_id ?? ""}
                onChange={(e) => onUpdate("minimax", { group_id: e.target.value })}
                placeholder={t("minimax.groupIdPlaceholder")} />
            </div>
            <div className="grid gap-1.5">
              <Label htmlFor="mm-base">{t("minimax.apiBase")}</Label>
              <Input id="mm-base" className="text-base md:text-sm"
                value={draft.minimax.api_base ?? ""}
                onChange={(e) => onUpdate("minimax", { api_base: e.target.value })}
                placeholder="https://api.minimax.io/v1" />
            </div>
          </>
        )}

        {provider === "gemini" && (
          <>
            <div className="grid gap-1.5">
              <Label htmlFor="gm-key">{t("gemini.apiKey", "API Key")}</Label>
              <Input id="gm-key" type="password" className="text-base md:text-sm"
                value={draft.gemini.api_key ?? ""}
                onChange={(e) => onUpdate("gemini", { api_key: e.target.value })}
                placeholder="AIza..." />
            </div>
            <div className="grid gap-1.5">
              <Label htmlFor="gm-base">{t("gemini.apiBase", "API Base URL")}</Label>
              <Input id="gm-base" className="text-base md:text-sm"
                value={draft.gemini.api_base ?? ""}
                onChange={(e) => onUpdate("gemini", { api_base: e.target.value })}
                placeholder="https://generativelanguage.googleapis.com" />
            </div>
          </>
        )}

        <div className="flex gap-2">
          <Button
            type="button"
            variant="outline"
            size="sm"
            className="h-9 gap-1.5"
            disabled={testing}
            onClick={handleTestConnection}
          >
            <FlaskConical className="h-3.5 w-3.5" />
            {testing ? t("testConnection.testing", "Testing…") : t("testConnection.label", "Test connection")}
          </Button>
          <Button
            type="button"
            size="sm"
            className="h-9 gap-1.5"
            disabled={saving || !dirty}
            onClick={onSave}
          >
            <Save className="h-3.5 w-3.5" />
            {saving ? t("saving") : t("save")}
          </Button>
        </div>
      </CardContent>
    </Card>
  );
}
