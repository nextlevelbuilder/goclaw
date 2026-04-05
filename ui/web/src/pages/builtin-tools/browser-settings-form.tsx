import { useState, useEffect } from "react";
import { useTranslation } from "react-i18next";
import {
  DialogHeader,
  DialogTitle,
  DialogDescription,
  DialogFooter,
} from "@/components/ui/dialog";
import { Button } from "@/components/ui/button";
import { Loader2 } from "lucide-react";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";

interface BrowserSettings {
  public_url: string;
}

const defaultSettings: BrowserSettings = {
  public_url: "",
};

interface Props {
  initialSettings: Record<string, unknown>;
  onSave: (settings: Record<string, unknown>) => Promise<void>;
  onCancel: () => void;
}

export function BrowserSettingsForm({ initialSettings, onSave, onCancel }: Props) {
  const { t } = useTranslation("tools");
  const [settings, setSettings] = useState<BrowserSettings>(defaultSettings);
  const [saving, setSaving] = useState(false);

  useEffect(() => {
    setSettings({
      ...defaultSettings,
      public_url: String(initialSettings.public_url ?? ""),
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

  return (
    <>
      <DialogHeader>
        <DialogTitle>{t("builtin.browserSettings.title")}</DialogTitle>
        <DialogDescription>
          {t("builtin.browserSettings.description")}
        </DialogDescription>
      </DialogHeader>

      <div className="space-y-4 py-2">
        <div className="grid gap-1.5">
          <Label htmlFor="browser-public-url" className="text-sm">
            {t("builtin.browserSettings.publicUrl")}
          </Label>
          <Input
            id="browser-public-url"
            type="url"
            value={settings.public_url}
            onChange={(e) => setSettings((s) => ({ ...s, public_url: e.target.value }))}
            placeholder="https://goclaw.example.com"
            className="text-base md:text-sm"
          />
          <p className="text-xs text-muted-foreground">
            {t("builtin.browserSettings.publicUrlHint")}
          </p>
        </div>
      </div>

      <DialogFooter>
        <Button variant="outline" onClick={onCancel}>{t("builtin.browserSettings.cancel")}</Button>
        <Button onClick={handleSave} disabled={saving}>
          {saving && <Loader2 className="h-4 w-4 animate-spin" />}
          {saving ? t("builtin.browserSettings.saving") : t("builtin.browserSettings.save")}
        </Button>
      </DialogFooter>
    </>
  );
}
