import { useState } from "react";
import { useTranslation } from "react-i18next";
import { Loader2 } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Label } from "@/components/ui/label";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import {
  DialogHeader,
  DialogTitle,
  DialogDescription,
  DialogFooter,
} from "@/components/ui/dialog";

type TtsProvider = "openai" | "elevenlabs" | "edge" | "minimax";
type AutoMode = "off" | "inbound" | "tagged" | "always";

interface Props {
  initialSettings: Record<string, unknown>;
  onSave: (settings: Record<string, unknown>) => Promise<void>;
  onCancel: () => void;
}

const TTS_PROVIDERS: TtsProvider[] = ["openai", "elevenlabs", "edge", "minimax"];
const AUTO_MODES: AutoMode[] = ["off", "inbound", "tagged", "always"];

const AUTO_MODE_DESC_KEYS: Record<AutoMode, string> = {
  off: "builtin.ttsForm.autoOffDesc",
  inbound: "builtin.ttsForm.autoInboundDesc",
  tagged: "builtin.ttsForm.autoTaggedDesc",
  always: "builtin.ttsForm.autoAlwaysDesc",
};

const AUTO_MODE_LABEL_KEYS: Record<AutoMode, string> = {
  off: "builtin.ttsForm.autoOff",
  inbound: "builtin.ttsForm.autoInbound",
  tagged: "builtin.ttsForm.autoTagged",
  always: "builtin.ttsForm.autoAlways",
};

export function TtsProviderForm({ initialSettings, onSave, onCancel }: Props) {
  const { t } = useTranslation("tools");

  const [primary, setPrimary] = useState<TtsProvider>(
    (initialSettings.primary as TtsProvider) ?? "openai",
  );
  const [auto, setAuto] = useState<AutoMode>(
    (initialSettings.auto as AutoMode) ?? "inbound",
  );
  const [saving, setSaving] = useState(false);

  const handleSave = async () => {
    setSaving(true);
    try {
      await onSave({ primary, auto });
    } catch {
      // toast shown by hook
    } finally {
      setSaving(false);
    }
  };

  return (
    <div>
      <DialogHeader>
        <DialogTitle>{t("builtin.ttsForm.title")}</DialogTitle>
        <DialogDescription>{t("builtin.ttsForm.description")}</DialogDescription>
      </DialogHeader>

      <div className="space-y-5 my-4">
        {/* Primary provider */}
        <div className="space-y-1.5">
          <Label className="text-sm font-medium">{t("builtin.ttsForm.primary")}</Label>
          <Select value={primary} onValueChange={(v) => setPrimary(v as TtsProvider)}>
            <SelectTrigger className="text-base md:text-sm">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              {TTS_PROVIDERS.map((p) => (
                <SelectItem key={p} value={p} className="text-base md:text-sm">
                  {p.charAt(0).toUpperCase() + p.slice(1)}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        </div>

        {/* Auto mode */}
        <div className="space-y-1.5">
          <Label className="text-sm font-medium">{t("builtin.ttsForm.auto")}</Label>
          <Select value={auto} onValueChange={(v) => setAuto(v as AutoMode)}>
            <SelectTrigger className="text-base md:text-sm">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              {AUTO_MODES.map((mode) => (
                <SelectItem key={mode} value={mode} className="text-base md:text-sm">
                  <div>
                    <div className="font-medium">{t(AUTO_MODE_LABEL_KEYS[mode])}</div>
                    <div className="text-xs text-muted-foreground">{t(AUTO_MODE_DESC_KEYS[mode])}</div>
                  </div>
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        </div>
      </div>

      <DialogFooter>
        <Button variant="outline" onClick={onCancel}>
          {t("builtin.ttsForm.cancel")}
        </Button>
        <Button onClick={handleSave} disabled={saving}>
          {saving && <Loader2 className="h-4 w-4 animate-spin" />}
          {saving ? t("builtin.ttsForm.saving") : t("builtin.ttsForm.save")}
        </Button>
      </DialogFooter>
    </div>
  );
}
