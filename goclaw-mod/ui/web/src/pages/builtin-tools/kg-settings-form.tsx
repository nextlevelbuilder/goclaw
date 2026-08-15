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
import { Switch } from "@/components/ui/switch";
import { ProviderModelSelect } from "@/components/shared/provider-model-select";

interface KGSettings {
  extract_on_memory_write: boolean;
  extraction_provider: string;
  extraction_model: string;
  min_confidence: number;
}

const defaultSettings: KGSettings = {
  extract_on_memory_write: false,
  extraction_provider: "",
  extraction_model: "",
  min_confidence: 0.75,
};

interface Props {
  initialSettings: Record<string, unknown>;
  onSave: (settings: Record<string, unknown>) => Promise<void>;
  onCancel: () => void;
}

export function KGSettingsForm({ initialSettings, onSave, onCancel }: Props) {
  const { t } = useTranslation("tools");
  const [settings, setSettings] = useState<KGSettings>(defaultSettings);
  const [saving, setSaving] = useState(false);

  useEffect(() => {
    setSettings({
      ...defaultSettings,
      ...initialSettings,
      min_confidence: Number(initialSettings.min_confidence) || defaultSettings.min_confidence,
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
        <DialogTitle>{t("builtin.kgSettings.title")}</DialogTitle>
        <DialogDescription>
          {t("builtin.kgSettings.description")}
        </DialogDescription>
      </DialogHeader>

      <div className="space-y-4 py-2">
        <ProviderModelSelect
          provider={settings.extraction_provider}
          onProviderChange={(v) => setSettings((s) => ({ ...s, extraction_provider: v }))}
          model={settings.extraction_model}
          onModelChange={(v) => setSettings((s) => ({ ...s, extraction_model: v }))}
          providerLabel={t("builtin.kgSettings.extractionProvider")}
          modelLabel={t("builtin.kgSettings.extractionModel")}
          providerTip={t("builtin.kgSettings.providerTip")}
          modelTip={t("builtin.kgSettings.modelTip")}
        />

        <div className="grid gap-1.5">
          <Label htmlFor="kg-min-conf" className="text-sm">{t("builtin.kgSettings.minConfidence")}</Label>
          <Input
            id="kg-min-conf"
            type="number"
            min={0}
            max={1}
            step={0.05}
            value={settings.min_confidence}
            onChange={(e) => setSettings((s) => ({ ...s, min_confidence: Number(e.target.value) || 0.75 }))}
            className="max-w-[120px]"
          />
          <p className="text-xs text-muted-foreground">
            {t("builtin.kgSettings.minConfidenceHint")}
          </p>
        </div>

        <div className="flex items-center justify-between rounded-md border p-3">
          <div>
            <Label htmlFor="kg-auto-extract" className="text-sm font-medium">{t("builtin.kgSettings.autoExtract")}</Label>
            <p className="text-xs text-muted-foreground mt-0.5">
              {t("builtin.kgSettings.autoExtractHint")}
            </p>
          </div>
          <Switch
            id="kg-auto-extract"
            checked={settings.extract_on_memory_write}
            onCheckedChange={(v) => setSettings((s) => ({ ...s, extract_on_memory_write: v }))}
          />
        </div>
      </div>

      <DialogFooter>
        <Button variant="outline" onClick={onCancel}>{t("builtin.kgSettings.cancel")}</Button>
        <Button onClick={handleSave} disabled={saving}>
          {saving && <Loader2 className="h-4 w-4 animate-spin" />}
          {saving ? t("builtin.kgSettings.saving") : t("builtin.kgSettings.save")}
        </Button>
      </DialogFooter>
    </>
  );
}
