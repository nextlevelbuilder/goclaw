import { useState } from "react";
import { PlayCircleIcon } from "lucide-react";
import { useTranslation } from "react-i18next";
import { Button } from "@/components/ui/button";
import { Label } from "@/components/ui/label";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { toast } from "@/stores/use-toast-store";
import { VoicePicker } from "@/components/voice-picker";
import { getProviderDefinition } from "@/data/tts-providers";
import type { TtsProviderId } from "@/data/tts-providers";
import type { SynthesizeParams } from "@/pages/tts/hooks/use-tts-config";

interface Props {
  globalProvider: string;
  voiceId: string;
  modelId: string;
  onVoiceChange: (v: string) => void;
  onModelChange: (v: string) => void;
  /** Whether agent-level override is enabled (checkbox driven by parent) */
  overrideEnabled: boolean;
  onOverrideChange: (v: boolean) => void;
  synthesize: (params: SynthesizeParams) => Promise<Blob>;
}

/**
 * Rendered inside the TTS subsection of PromptSettingsSection when global TTS is configured.
 * Manages: inheritance chip, override checkbox, VoicePicker, model Select, inline test button.
 */
export function TtsOverrideBlock({
  globalProvider,
  voiceId,
  modelId,
  onVoiceChange,
  onModelChange,
  overrideEnabled,
  onOverrideChange,
  synthesize,
}: Props) {
  const { t } = useTranslation("tts");
  const [testing, setTesting] = useState(false);

  const def = getProviderDefinition(globalProvider);
  const providerLabel = globalProvider.charAt(0).toUpperCase() + globalProvider.slice(1);
  const models = def?.models ?? [];
  const hasModels = models.length > 0;

  const canTest = overrideEnabled && !!voiceId && (hasModels ? !!modelId : true) && !!globalProvider;

  const handleTest = async () => {
    if (!canTest) return;
    setTesting(true);
    try {
      const blob = await synthesize({
        text: t("test.sample_text"),
        provider: globalProvider,
        voice_id: voiceId,
        model_id: modelId || undefined,
      });
      const url = URL.createObjectURL(blob);
      const audio = new Audio(url);
      audio.onended = () => URL.revokeObjectURL(url);
      audio.onerror = () => URL.revokeObjectURL(url);
      await audio.play();
    } catch (err) {
      toast.error(err instanceof Error ? err.message : "Test failed");
    } finally {
      setTesting(false);
    }
  };

  const handleOverrideChange = (checked: boolean) => {
    onOverrideChange(checked);
    if (!checked) {
      onVoiceChange("");
      onModelChange("");
    }
  };

  return (
    <div className="space-y-3">
      {/* Inheritance info chip */}
      <p className="text-xs text-muted-foreground bg-muted/50 rounded px-2 py-1 inline-block">
        {t("override.inherits", {
          provider: providerLabel,
          voice: globalProvider === "elevenlabs" ? t("voice_label") : (def?.defaultVoice ?? "–"),
          model: def?.defaultModel ?? "–",
        })}
      </p>

      {/* Override checkbox */}
      <label className="flex items-center gap-2 cursor-pointer select-none">
        <input
          type="checkbox"
          className="size-4 rounded accent-primary"
          checked={overrideEnabled}
          onChange={(e) => handleOverrideChange(e.target.checked)}
        />
        <span className="text-sm">{t("override.label")}</span>
      </label>

      {overrideEnabled && (
        <div className="space-y-2 pl-6">
          {/* Voice picker — provider-aware */}
          <div className="space-y-1">
            <Label className="text-xs text-muted-foreground">{t("voice_label")}</Label>
            <VoicePicker
              provider={(globalProvider as TtsProviderId) || undefined}
              value={voiceId || undefined}
              onChange={onVoiceChange}
            />
          </div>

          {/* Model select — catalog-driven; hidden for providers with no models (edge) */}
          {hasModels && (
            <div className="space-y-1">
              <Label className="text-xs text-muted-foreground">{t("model_label")}</Label>
              <Select value={modelId} onValueChange={onModelChange}>
                <SelectTrigger className="w-full text-base md:text-sm">
                  <SelectValue placeholder={t("model_placeholder")} />
                </SelectTrigger>
                <SelectContent>
                  {models.map((m) => (
                    <SelectItem key={m.value} value={m.value}>
                      {m.label}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </div>
          )}

          {/* Inline test button */}
          <Button
            type="button"
            size="sm"
            variant="outline"
            disabled={!canTest || testing}
            onClick={handleTest}
            className="min-h-[44px] sm:min-h-9 gap-1.5"
          >
            <PlayCircleIcon className="size-4" />
            {testing ? "..." : t("test.button")}
          </Button>
        </div>
      )}
    </div>
  );
}
