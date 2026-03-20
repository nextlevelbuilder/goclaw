import { useState } from "react";
import { useTranslation } from "react-i18next";
import { Copy } from "lucide-react";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Switch } from "@/components/ui/switch";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { StickySaveBar } from "@/components/shared/sticky-save-bar";
import { PROVIDER_TYPES } from "@/constants/providers";
import { toast } from "@/stores/use-toast-store";
import type { ProviderData, ProviderInput } from "@/types/provider";

interface ProviderOverviewProps {
  provider: ProviderData;
  onUpdate: (id: string, data: ProviderInput) => Promise<void>;
}

const NO_API_KEY_TYPES = new Set(["claude_cli", "acp", "chatgpt_oauth"]);

export function ProviderOverview({ provider, onUpdate }: ProviderOverviewProps) {
  const { t } = useTranslation("providers");
  const { t: tc } = useTranslation("common");

  const typeInfo = PROVIDER_TYPES.find((pt) => pt.value === provider.provider_type);
  const typeLabel = typeInfo?.label ?? provider.provider_type;
  const showApiKey = !NO_API_KEY_TYPES.has(provider.provider_type);

  // Identity
  const [displayName, setDisplayName] = useState(provider.display_name || "");

  // API Key
  const [apiKey, setApiKey] = useState(provider.api_key || "");

  // Status
  const [enabled, setEnabled] = useState(provider.enabled);

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
