import { useState, useEffect } from "react";
import { useTranslation } from "react-i18next";
import {
  DialogHeader,
  DialogTitle,
  DialogDescription,
  DialogFooter,
} from "@/components/ui/dialog";
import { Button } from "@/components/ui/button";
import { Loader2, ChevronDown, ChevronUp } from "lucide-react";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { GWSOAuthWizard } from "./gws-oauth-wizard";

interface GWSSettings {
  client_id: string;
  client_secret: string;
  refresh_token: string;
}

const defaultSettings: GWSSettings = {
  client_id: "",
  client_secret: "",
  refresh_token: "",
};

interface Props {
  initialSettings: Record<string, unknown>;
  onSave: (settings: Record<string, unknown>) => Promise<void>;
  onCancel: () => void;
}

export function GWSSettingsForm({ initialSettings, onSave, onCancel }: Props) {
  const { t } = useTranslation("tools");
  const [settings, setSettings] = useState<GWSSettings>(defaultSettings);
  const [saving, setSaving] = useState(false);
  const [showManual, setShowManual] = useState(false);

  useEffect(() => {
    setSettings({
      ...defaultSettings,
      ...initialSettings,
      client_id: String(initialSettings.client_id ?? ""),
      client_secret: String(initialSettings.client_secret ?? ""),
      refresh_token: String(initialSettings.refresh_token ?? ""),
    });
  }, [initialSettings]);

  const handleSave = async () => {
    setSaving(true);
    try {
      await onSave(settings as unknown as Record<string, unknown>);
    } catch {
      // toast shown by hook
    } finally {
      setSaving(false);
    }
  };

  // Called by GWSOAuthWizard after successful token exchange.
  // Backend already saved the full refresh_token — just close dialog, don't overwrite with preview.
  const handleAuthorized = (_previewToken: string) => {
    onCancel(); // close dialog — settings already saved by backend
  };

  const isValid =
    settings.client_id.trim() !== "" &&
    settings.client_secret.trim() !== "" &&
    settings.refresh_token.trim() !== "";

  return (
    <>
      <DialogHeader>
        <DialogTitle>Google Workspace Settings</DialogTitle>
        <DialogDescription>
          Authorize Google access using your OAuth Client credentials.
        </DialogDescription>
      </DialogHeader>

      <div className="space-y-4 py-2">
        <div className="grid gap-1.5">
          <Label htmlFor="gws-client-id" className="text-sm">Client ID *</Label>
          <Input
            id="gws-client-id"
            type="text"
            placeholder="xxxxx.apps.googleusercontent.com"
            value={settings.client_id}
            onChange={(e) => setSettings((s) => ({ ...s, client_id: e.target.value }))}
          />
        </div>

        <div className="grid gap-1.5">
          <Label htmlFor="gws-client-secret" className="text-sm">Client Secret *</Label>
          <Input
            id="gws-client-secret"
            type="password"
            placeholder="GOCSPX-..."
            value={settings.client_secret}
            onChange={(e) => setSettings((s) => ({ ...s, client_secret: e.target.value }))}
          />
        </div>

        <GWSOAuthWizard
          clientId={settings.client_id}
          clientSecret={settings.client_secret}
          onAuthorized={handleAuthorized}
        />

        {/* Advanced: manual refresh token fallback */}
        <div>
          <button
            type="button"
            className="flex items-center gap-1 text-xs text-muted-foreground hover:text-foreground"
            onClick={() => setShowManual((v) => !v)}
          >
            {showManual ? <ChevronUp className="h-3 w-3" /> : <ChevronDown className="h-3 w-3" />}
            Advanced: paste Refresh Token manually
          </button>
          {showManual && (
            <div className="mt-2 grid gap-1.5">
              <Label htmlFor="gws-refresh-token" className="text-sm">Refresh Token</Label>
              <Input
                id="gws-refresh-token"
                type="password"
                placeholder="1//0xxx..."
                value={settings.refresh_token}
                onChange={(e) => setSettings((s) => ({ ...s, refresh_token: e.target.value }))}
              />
            </div>
          )}
        </div>
      </div>

      <DialogFooter>
        <Button variant="outline" onClick={onCancel}>{t("builtin.kgSettings.cancel")}</Button>
        <Button onClick={handleSave} disabled={saving || !isValid}>
          {saving && <Loader2 className="h-4 w-4 animate-spin" />}
          {saving ? t("builtin.kgSettings.saving") : t("builtin.kgSettings.save")}
        </Button>
      </DialogFooter>
    </>
  );
}
