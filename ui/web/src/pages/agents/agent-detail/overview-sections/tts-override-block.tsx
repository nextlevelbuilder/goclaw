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
import { DynamicParamForm } from "@/components/dynamic-param-form";
import type { ParamValue } from "@/components/dynamic-param-form";
import { useTtsCapabilities } from "@/api/tts-capabilities";
import { PROVIDER_MODEL_CATALOG } from "@/data/tts-providers";
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
  /** Generic tts_params stored in agents.other_config.tts_params (e.g. {speed:1.2}) */
  ttsParams: Record<string, ParamValue>;
  onTtsParamsChange: (params: Record<string, ParamValue>) => void;
}

/**
 * Build generic→native and native→generic mappings from the overridable params
 * slice. Each param carries its own native key (param.key) and generic alias
 * (param.agent_overridable_as), so no separate hard-coded table is needed.
 * Finding #9: single source of truth is the capabilities API response.
 */
export function buildAdapterMaps(
  overridableParams: Array<{ key: string; agent_overridable_as?: string }>,
): {
  genericToNative: Record<string, string>;
  nativeToGeneric: Record<string, string>;
} {
  const genericToNative: Record<string, string> = {};
  const nativeToGeneric: Record<string, string> = {};
  for (const p of overridableParams) {
    if (p.agent_overridable_as) {
      genericToNative[p.agent_overridable_as] = p.key;
      nativeToGeneric[p.key] = p.agent_overridable_as;
    }
  }
  return { genericToNative, nativeToGeneric };
}

/**
 * Convert stored generic params (e.g. {speed: 1.2}) to capability-native form
 * state (e.g. {voice_settings.speed: 1.2} for ElevenLabs).
 * Called at load time. Mapping derived from capabilities, not a hard-coded table.
 */
export function genericToNativeFormState(
  genericParams: Record<string, ParamValue>,
  overridableParams: Array<{ key: string; agent_overridable_as?: string }>,
): Record<string, ParamValue> {
  const { genericToNative } = buildAdapterMaps(overridableParams);
  const out: Record<string, ParamValue> = {};
  for (const [generic, val] of Object.entries(genericParams)) {
    const native = genericToNative[generic];
    if (native !== undefined) {
      out[native] = val;
    }
  }
  return out;
}

/**
 * Convert capability-native form state back to generic keys for storage.
 * Called at save time (inside handleSave in prompt-settings-section.tsx).
 */
export function nativeFormStateToGeneric(
  nativeState: Record<string, ParamValue>,
  overridableParams: Array<{ key: string; agent_overridable_as?: string }>,
): Record<string, ParamValue> {
  const { nativeToGeneric } = buildAdapterMaps(overridableParams);
  const out: Record<string, ParamValue> = {};
  for (const [native, val] of Object.entries(nativeState)) {
    const generic = nativeToGeneric[native];
    if (generic !== undefined) {
      out[generic] = val;
    }
  }
  return out;
}

/**
 * Rendered inside the TTS subsection of PromptSettingsSection when global TTS is configured.
 * Manages: inheritance chip, override checkbox, VoicePicker, model Select, inline test button.
 * Also renders the fine-tune (tts_params) section — filtered to agent_overridable params only
 * (Finding #9: single source of truth from capabilities API).
 *
 * Key design: agent storage uses GENERIC keys (speed, emotion, style). The DynamicParamForm
 * uses CAPABILITY-NATIVE keys (voice_settings.speed for ElevenLabs). A bidirectional adapter
 * converts at load (generic→native) and save (native→generic) boundaries.
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
  ttsParams,
  onTtsParamsChange,
}: Props) {
  const { t } = useTranslation("tts");
  const [testing, setTesting] = useState(false);

  // Fetch capabilities for the current provider to find agent-overridable params.
  // Finding #9: capabilities API is the single source of truth — agent_overridable_as
  // encodes both the overridability flag and the generic key alias, so no separate
  // hard-coded lookup table is needed in the UI.
  const { data: allCaps } = useTtsCapabilities();
  const providerCaps = allCaps?.find((c) => c.provider === globalProvider);
  const overridableParams = (providerCaps?.params ?? []).filter(
    (p) => (p.agent_overridable_as ?? "") !== "",
  );

  // Form state uses CAPABILITY-NATIVE keys. Convert from stored generic keys on each render.
  // Mapping is derived from overridableParams (no hard-coded lookup table).
  const nativeFormState = genericToNativeFormState(ttsParams, overridableParams);

  const handleParamChange = (nativeKey: string, val: ParamValue) => {
    const updated = { ...nativeFormState, [nativeKey]: val };
    // Convert native form state → generic keys for parent storage.
    const generic = nativeFormStateToGeneric(updated, overridableParams);
    onTtsParamsChange(generic);
  };

  const providerLabel = globalProvider.charAt(0).toUpperCase() + globalProvider.slice(1);
  const models = PROVIDER_MODEL_CATALOG[globalProvider as TtsProviderId] ?? [];
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
      onTtsParamsChange({});
    }
  };

  return (
    <div className="space-y-3">
      {/* Inheritance info chip */}
      <p className="text-xs text-muted-foreground bg-muted/50 rounded px-2 py-1 inline-block">
        {t("override.inherits", {
          provider: providerLabel,
          voice: globalProvider === "elevenlabs" ? t("voice_label") : "–",
          model: models[0]?.value ?? "–",
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

          {/* Fine-tune section — agent_overridable params only (Finding #9).
              Hidden entirely for providers with no overridable params (edge, gemini). */}
          {overridableParams.length > 0 && (
            <div className="space-y-2 border-t pt-2">
              <p className="text-xs font-medium text-muted-foreground uppercase tracking-wide">
                {t("override.params.title")}
              </p>
              <DynamicParamForm
                schema={overridableParams}
                value={nativeFormState}
                onChange={handleParamChange}
              />
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
