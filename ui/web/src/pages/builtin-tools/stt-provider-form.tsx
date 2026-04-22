import { useState } from "react";
import { useTranslation } from "react-i18next";
import { AlertTriangle, Loader2 } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import {
  DialogHeader,
  DialogTitle,
  DialogDescription,
  DialogFooter,
} from "@/components/ui/dialog";

type SttProviderName = "elevenlabs" | "proxy";

const ALL_STT_PROVIDERS: SttProviderName[] = ["elevenlabs", "proxy"];

interface SttSettings {
  providers: SttProviderName[];
  elevenlabs: { api_key: string; default_language: string };
  proxy: { url: string; api_key: string; tenant_id: string };
  whatsapp_enabled: boolean;
}

interface Props {
  initialSettings: Record<string, unknown>;
  onSave: (settings: Record<string, unknown>) => Promise<void>;
  onCancel: () => void;
}

function resolveInitial(raw: Record<string, unknown>): SttSettings {
  const el = (raw.elevenlabs as Record<string, unknown>) ?? {};
  const px = (raw.proxy as Record<string, unknown>) ?? {};
  return {
    providers: (raw.providers as SttProviderName[]) ?? ["elevenlabs", "proxy"],
    elevenlabs: {
      api_key: (el.api_key as string) ?? "",
      default_language: (el.default_language as string) ?? "en",
    },
    proxy: {
      url: (px.url as string) ?? "",
      api_key: (px.api_key as string) ?? "",
      tenant_id: (px.tenant_id as string) ?? "",
    },
    whatsapp_enabled: (raw.whatsapp_enabled as boolean) ?? false,
  };
}

export function buildSttPayload(s: SttSettings): Record<string, unknown> {
  return {
    providers: s.providers,
    elevenlabs: { api_key: s.elevenlabs.api_key, default_language: s.elevenlabs.default_language },
    proxy: { url: s.proxy.url, api_key: s.proxy.api_key, tenant_id: s.proxy.tenant_id },
    whatsapp_enabled: s.whatsapp_enabled,
  };
}

export function validateSttProviders(providers: string[]): boolean {
  return providers.length > 0;
}

export function SttProviderForm({ initialSettings, onSave, onCancel }: Props) {
  const { t } = useTranslation("tools");
  const init = resolveInitial(initialSettings);

  const [providers, setProviders] = useState<SttProviderName[]>(init.providers);
  const [elApiKey, setElApiKey] = useState(init.elevenlabs.api_key);
  const [elLang, setElLang] = useState(init.elevenlabs.default_language);
  const [proxyUrl, setProxyUrl] = useState(init.proxy.url);
  const [proxyApiKey, setProxyApiKey] = useState(init.proxy.api_key);
  const [proxyTenantId, setProxyTenantId] = useState(init.proxy.tenant_id);
  const [whatsappEnabled, setWhatsappEnabled] = useState(init.whatsapp_enabled);
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState("");

  const toggleProvider = (name: SttProviderName) => {
    setProviders((prev) =>
      prev.includes(name) ? prev.filter((p) => p !== name) : [...prev, name],
    );
  };

  const handleSave = async () => {
    if (!validateSttProviders(providers)) {
      setError(t("builtin.sttForm.providersRequiredError"));
      return;
    }
    setError("");
    setSaving(true);
    try {
      await onSave(
        buildSttPayload({ providers, elevenlabs: { api_key: elApiKey, default_language: elLang }, proxy: { url: proxyUrl, api_key: proxyApiKey, tenant_id: proxyTenantId }, whatsapp_enabled: whatsappEnabled }),
      );
    } catch {
      // toast shown by hook
    } finally {
      setSaving(false);
    }
  };

  return (
    <div>
      <DialogHeader>
        <DialogTitle>{t("builtin.sttForm.title")}</DialogTitle>
        <DialogDescription>{t("builtin.sttForm.description")}</DialogDescription>
      </DialogHeader>

      <div className="space-y-5 my-4">
        {/* Providers */}
        <div className="space-y-1.5">
          <Label className="text-sm font-medium">{t("builtin.sttForm.providersLabel")}</Label>
          <div className="flex flex-wrap gap-2">
            {ALL_STT_PROVIDERS.map((p) => (
              <label key={p} className="flex items-center gap-1.5 cursor-pointer select-none">
                <input
                  type="checkbox"
                  checked={providers.includes(p)}
                  onChange={() => toggleProvider(p)}
                  className="h-4 w-4"
                />
                <span className="text-base md:text-sm capitalize">{p}</span>
              </label>
            ))}
          </div>
          {error && <p className="text-sm text-destructive">{error}</p>}
        </div>

        {/* ElevenLabs section */}
        <div className="space-y-3">
          <p className="text-sm font-semibold">{t("builtin.sttForm.elevenlabsSectionTitle")}</p>
          <div className="space-y-1.5">
            <Label className="text-sm font-medium">{t("builtin.sttForm.elevenlabsApiKeyLabel")}</Label>
            <Input
              type="password"
              value={elApiKey}
              onChange={(e) => setElApiKey(e.target.value)}
              placeholder="xi-..."
              className="text-base md:text-sm"
            />
          </div>
          <div className="space-y-1.5">
            <Label className="text-sm font-medium">{t("builtin.sttForm.elevenlabsDefaultLanguageLabel")}</Label>
            <Input
              type="text"
              value={elLang}
              onChange={(e) => setElLang(e.target.value)}
              placeholder="en"
              className="text-base md:text-sm"
            />
          </div>
        </div>

        {/* Proxy section */}
        <div className="space-y-3">
          <p className="text-sm font-semibold">{t("builtin.sttForm.proxySectionTitle")}</p>
          <div className="space-y-1.5">
            <Label className="text-sm font-medium">{t("builtin.sttForm.proxyUrlLabel")}</Label>
            <Input
              type="url"
              value={proxyUrl}
              onChange={(e) => setProxyUrl(e.target.value)}
              placeholder="https://..."
              className="text-base md:text-sm"
            />
          </div>
          <div className="space-y-1.5">
            <Label className="text-sm font-medium">{t("builtin.sttForm.proxyApiKeyLabel")}</Label>
            <Input
              type="password"
              value={proxyApiKey}
              onChange={(e) => setProxyApiKey(e.target.value)}
              className="text-base md:text-sm"
            />
          </div>
          <div className="space-y-1.5">
            <Label className="text-sm font-medium">{t("builtin.sttForm.proxyTenantIdLabel")}</Label>
            <Input
              type="text"
              value={proxyTenantId}
              onChange={(e) => setProxyTenantId(e.target.value)}
              className="text-base md:text-sm"
            />
          </div>
        </div>

        {/* WhatsApp toggle */}
        <div className="space-y-2">
          {/* Privacy banner */}
          <div className="flex items-start gap-2 rounded-md border border-amber-200 bg-amber-50 px-3 py-2 text-sm text-amber-800 dark:border-amber-800 dark:bg-amber-950/30 dark:text-amber-300">
            <AlertTriangle className="mt-0.5 h-4 w-4 shrink-0" />
            <span data-testid="whatsapp-privacy-banner">{t("builtin.sttForm.whatsappPrivacyWarning")}</span>
          </div>
          <label className="flex items-center gap-2 cursor-pointer select-none">
            <input
              type="checkbox"
              checked={whatsappEnabled}
              onChange={(e) => setWhatsappEnabled(e.target.checked)}
              className="h-4 w-4"
            />
            <span className="text-base md:text-sm font-medium">{t("builtin.sttForm.whatsappEnabledLabel")}</span>
          </label>
        </div>
      </div>

      <DialogFooter>
        <Button variant="outline" onClick={onCancel}>
          {t("builtin.sttForm.cancel")}
        </Button>
        <Button onClick={handleSave} disabled={saving}>
          {saving && <Loader2 className="h-4 w-4 animate-spin" />}
          {saving ? t("builtin.sttForm.saving") : t("builtin.sttForm.save")}
        </Button>
      </DialogFooter>
    </div>
  );
}
