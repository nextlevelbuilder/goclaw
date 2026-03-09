import { useState, useEffect } from "react";
import {
  DialogHeader,
  DialogTitle,
  DialogDescription,
  DialogFooter,
} from "@/components/ui/dialog";
import { Button } from "@/components/ui/button";
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
    } finally {
      setSaving(false);
    }
  };

  return (
    <>
      <DialogHeader>
        <DialogTitle>Knowledge Graph Settings</DialogTitle>
        <DialogDescription>
          Configure entity extraction from memory writes. Requires a provider/model capable of structured JSON output.
        </DialogDescription>
      </DialogHeader>

      <div className="space-y-4 py-2">
        <ProviderModelSelect
          provider={settings.extraction_provider}
          onProviderChange={(v) => setSettings((s) => ({ ...s, extraction_provider: v }))}
          model={settings.extraction_model}
          onModelChange={(v) => setSettings((s) => ({ ...s, extraction_model: v }))}
          providerLabel="Extraction Provider"
          modelLabel="Extraction Model"
          providerTip="LLM provider used to extract entities and relations from text."
          modelTip="Model ID for extraction. Should support structured JSON output."
        />

        <div className="grid gap-1.5">
          <Label htmlFor="kg-min-conf" className="text-sm">Min Confidence</Label>
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
            Entities below this confidence score are discarded (0.0–1.0).
          </p>
        </div>

        <div className="flex items-center justify-between rounded-md border p-3">
          <div>
            <Label htmlFor="kg-auto-extract" className="text-sm font-medium">Auto-extract on memory write</Label>
            <p className="text-xs text-muted-foreground mt-0.5">
              Automatically extract entities when agents write to memory files.
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
        <Button variant="outline" onClick={onCancel}>Cancel</Button>
        <Button onClick={handleSave} disabled={saving}>
          {saving ? "Saving..." : "Save"}
        </Button>
      </DialogFooter>
    </>
  );
}
