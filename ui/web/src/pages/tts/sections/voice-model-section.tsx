/**
 * Voice & Model section — Step 3 of the TTS configuration flow.
 * Uses the provider-aware VoicePicker (Phase 01) for voice selection.
 * Model select is driven by catalog; hidden when provider has no models (Edge).
 */
import { useTranslation } from "react-i18next";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Label } from "@/components/ui/label";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { VoicePicker } from "@/components/voice-picker";
import { getProviderDefinition, type TtsProviderId } from "@/data/tts-providers";

interface Props {
  provider: string;
  voiceId: string;
  modelId: string;
  onVoiceChange: (id: string) => void;
  onModelChange: (id: string) => void;
}

export function VoiceModelSection({
  provider,
  voiceId,
  modelId,
  onVoiceChange,
  onModelChange,
}: Props) {
  const { t } = useTranslation("tts");

  const def = provider ? getProviderDefinition(provider) : null;
  const models = def?.models ?? [];
  // Cast to TtsProviderId | "" — empty string shows the disabled empty-state in VoicePicker
  const pickerProvider = (provider as TtsProviderId | "") || "";

  // Edge uses "voice" field (not voice_id) — but VoicePicker drives a single string
  // The parent maps onVoiceChange to the correct provider sub-field
  return (
    <Card>
      <CardHeader className="pb-3">
        <CardTitle className="text-base">3. {t("voice_label")} &amp; {t("model_label")}</CardTitle>
      </CardHeader>
      <CardContent className="space-y-4">
        <div className="grid gap-1.5">
          <Label>{t("voice_label")}</Label>
          <VoicePicker
            provider={pickerProvider}
            value={voiceId}
            onChange={onVoiceChange}
          />
        </div>

        {models.length > 0 && (
          <div className="grid gap-1.5">
            <Label>{t("model_label")}</Label>
            <Select value={modelId || ""} onValueChange={onModelChange}>
              <SelectTrigger className="w-full text-base md:text-sm">
                <SelectValue placeholder={t("model_placeholder")} />
              </SelectTrigger>
              <SelectContent>
                {models.map((m) => (
                  <SelectItem key={m.value} value={m.value}>
                    {m.label}
                    {m.description && (
                      <span className="ml-1.5 text-xs text-muted-foreground">
                        {m.description}
                      </span>
                    )}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </div>
        )}
      </CardContent>
    </Card>
  );
}
