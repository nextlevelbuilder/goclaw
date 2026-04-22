/**
 * TTS configuration page — 4-step guided flow:
 *   1. Provider selection (ProviderSetup)
 *   2. Credentials (CredentialsSection) — skipped for Edge (no API key)
 *   3. Voice, Model & dynamic params (DynamicParamForm from capabilities)
 *   4. Test Playground
 *   + Advanced collapsible: auto mode, reply mode, limits (BehaviorSection)
 *
 * State is lifted here: draft + dirty tracking. Sections are controlled components.
 * Save flow: POST /v1/tts/config via useTtsConfig().save().
 * Params blob is merged into the provider sub-config before save (dual-write).
 */
import { useState, useEffect } from "react";
import { useTranslation } from "react-i18next";
import { RefreshCw, Save } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Label } from "@/components/ui/label";
import { PageHeader } from "@/components/shared/page-header";
import { TableSkeleton } from "@/components/shared/loading-skeleton";
import { useTtsConfig, type TtsConfig, type TtsProviderConfig } from "./hooks/use-tts-config";
import { useTtsCapabilities } from "@/api/tts-capabilities";
import { useMinLoading } from "@/hooks/use-min-loading";
import { useDeferredLoading } from "@/hooks/use-deferred-loading";
import { ProviderSetup } from "./sections/provider-setup";
import { CredentialsSection } from "./sections/credentials-section";
import { TestPlayground } from "./sections/test-playground";
import { BehaviorSection } from "./sections/behavior-section";
import { VoicePicker } from "@/components/voice-picker";
import { DynamicParamForm } from "@/components/dynamic-param-form";
import type { ParamValue } from "@/components/dynamic-param-form";
import type { TtsProviderId } from "@/data/tts-providers";

import { MultiSpeakerEditor } from "@/components/multi-speaker-editor";
import type { SpeakerVoice } from "@/components/multi-speaker-editor";

// Per-provider helpers: extract voice/model id from the provider sub-config
function getVoiceId(draft: TtsConfig): string {
  switch (draft.provider) {
    case "openai": return draft.openai.voice ?? "";
    case "elevenlabs": return draft.elevenlabs.voice_id ?? "";
    case "edge": return draft.edge.voice ?? "";
    case "minimax": return draft.minimax.voice_id ?? "";
    case "gemini": return draft.gemini.voice ?? "";
    default: return "";
  }
}

function getModelId(draft: TtsConfig): string {
  switch (draft.provider) {
    case "openai": return draft.openai.model ?? "";
    case "elevenlabs": return draft.elevenlabs.model_id ?? "";
    case "minimax": return draft.minimax.model ?? "";
    case "gemini": return draft.gemini.model ?? "";
    default: return "";
  }
}

type ProviderKey = keyof Pick<TtsConfig, "openai" | "elevenlabs" | "edge" | "minimax" | "gemini">;

function voicePatch(provider: string, value: string): [ProviderKey, Partial<TtsProviderConfig>] | null {
  switch (provider) {
    case "openai": return ["openai", { voice: value }];
    case "elevenlabs": return ["elevenlabs", { voice_id: value }];
    case "edge": return ["edge", { voice: value }];
    case "minimax": return ["minimax", { voice_id: value }];
    case "gemini": return ["gemini", { voice: value }];
    default: return null;
  }
}

function modelPatch(provider: string, value: string): [ProviderKey, Partial<TtsProviderConfig>] | null {
  switch (provider) {
    case "openai": return ["openai", { model: value }];
    case "elevenlabs": return ["elevenlabs", { model_id: value }];
    case "minimax": return ["minimax", { model: value }];
    case "gemini": return ["gemini", { model: value }];
    default: return null;
  }
}

function isCredentialsSaved(provider: string, tts: TtsConfig): boolean {
  if (provider === "edge") return true;
  const cfg = tts[provider as ProviderKey];
  return !!cfg?.api_key;
}

export function TtsPage() {
  const { t } = useTranslation("tts");
  const { t: tc } = useTranslation("common");
  const { tts, loading, saving, error, refresh, save, synthesize, testConnection } = useTtsConfig();
  const { data: caps = [] } = useTtsCapabilities();
  const spinning = useMinLoading(loading);

  const [draft, setDraft] = useState<TtsConfig>(tts);
  const showSkeleton = useDeferredLoading(loading && !draft.provider);
  const [dirty, setDirty] = useState(false);

  // Per-provider dynamic params state (maps param key → value)
  const [paramsState, setParamsState] = useState<Record<string, ParamValue>>({});
  // Gemini multi-speaker state
  const [speakers, setSpeakers] = useState<SpeakerVoice[]>([]);

  useEffect(() => {
    setDraft(tts);
    setDirty(false);
    // Initialize params from saved blob when provider changes
    if (tts.provider) {
      const key = tts.provider as ProviderKey;
      const saved = tts[key]?.params ?? {};
      setParamsState(saved as Record<string, ParamValue>);
    }
  }, [tts]);

  const update = (patch: Partial<TtsConfig>) => {
    setDraft((prev) => ({ ...prev, ...patch }));
    setDirty(true);
  };

  const updateProvider = (key: ProviderKey, patch: Partial<TtsProviderConfig>) => {
    setDraft((prev) => ({ ...prev, [key]: { ...prev[key], ...patch } }));
    setDirty(true);
  };

  const handleVoiceChange = (value: string) => {
    const p = voicePatch(draft.provider, value);
    if (p) updateProvider(p[0], p[1]);
  };

  const handleModelChange = (value: string) => {
    const p = modelPatch(draft.provider, value);
    if (p) updateProvider(p[0], p[1]);
  };

  const handleParamChange = (key: string, val: ParamValue) => {
    setParamsState((prev) => ({ ...prev, [key]: val }));
    setDirty(true);
  };

  const handleSave = async () => {
    // Merge params blob into provider sub-config before saving
    const providerKey = draft.provider as ProviderKey;
    const enriched: TtsConfig = providerKey
      ? {
          ...draft,
          [providerKey]: {
            ...draft[providerKey],
            params: paramsState,
          },
        }
      : draft;
    await save(enriched);
    setDirty(false);
  };

  // Find capabilities for current provider
  const providerCaps = caps.find((c) => c.provider === draft.provider);
  const paramSchemas = providerCaps?.params ?? [];
  const models = providerCaps?.models ?? [];
  const customFeatures = providerCaps?.custom_features ?? {};

  if (showSkeleton) {
    return (
      <div className="p-4 sm:p-6 pb-10">
        <PageHeader title={t("title")} description={t("description")} />
        <div className="mt-4"><TableSkeleton rows={3} /></div>
      </div>
    );
  }

  return (
    <div className="p-4 sm:p-6 pb-10 space-y-4">
      <PageHeader
        title={t("title")}
        description={t("description")}
        actions={
          <div className="flex gap-2">
            <Button variant="outline" size="sm" onClick={refresh} disabled={spinning} className="gap-1">
              <RefreshCw className={"h-3.5 w-3.5" + (spinning ? " animate-spin" : "")} /> {tc("refresh")}
            </Button>
            <Button size="sm" onClick={handleSave} disabled={!dirty || saving} className="gap-1">
              <Save className="h-3.5 w-3.5" /> {saving ? t("saving") : t("save")}
            </Button>
          </div>
        }
      />

      <ProviderSetup provider={draft.provider} onChange={(v) => update({ provider: v })} />

      {draft.provider && (
        <CredentialsSection
          provider={draft.provider}
          draft={draft}
          onUpdate={updateProvider}
          testConnection={testConnection}
          onSave={handleSave}
          saving={saving}
          dirty={dirty}
        />
      )}

      {draft.provider && isCredentialsSaved(draft.provider, tts) && (
        <Card className="gap-3">
          <CardHeader>
            <CardTitle className="text-base">3. {t("voice_label")} &amp; {t("general.title", "Settings")}</CardTitle>
          </CardHeader>
          <CardContent className="space-y-4">
            {/* Voice picker */}
            <div className="grid gap-1.5">
              <Label>{t("voice_label")}</Label>
              <VoicePicker
                provider={(draft.provider as TtsProviderId) || ""}
                value={getVoiceId(draft)}
                onChange={handleVoiceChange}
              />
            </div>

            {/* Model select — driven by capabilities */}
            {models.length > 0 && (
              <div className="grid gap-1.5">
                <Label>{t("model_label")}</Label>
                <select
                  className="border rounded px-2 py-1 text-sm w-full h-9"
                  value={getModelId(draft)}
                  onChange={(e) => handleModelChange(e.target.value)}
                >
                  {models.map((m) => (
                    <option key={m} value={m}>{m}</option>
                  ))}
                </select>
              </div>
            )}

            {/* Custom slot: multi-speaker (Gemini) */}
            {Boolean(customFeatures["multi_speaker"]) && draft.provider === "gemini" && (
              <MultiSpeakerEditor
                speakers={speakers}
                voices={providerCaps?.voices?.map((v) => v.voice_id) ?? []}
                onChange={(s) => { setSpeakers(s); setDirty(true); }}
              />
            )}

            {/* Dynamic param form from capabilities */}
            {paramSchemas.length > 0 && (
              <DynamicParamForm
                schema={paramSchemas}
                value={paramsState}
                onChange={handleParamChange}
              />
            )}
          </CardContent>
        </Card>
      )}

      {draft.provider && isCredentialsSaved(draft.provider, tts) && (
        <TestPlayground
          provider={draft.provider}
          voiceId={getVoiceId(draft)}
          modelId={getModelId(draft)}
          synthesize={synthesize}
          showAudioTags={Boolean(customFeatures["audio_tags"])}
        />
      )}

      <BehaviorSection draft={draft} onUpdate={update} />

      {error && <p className="text-sm text-destructive">{error}</p>}

      {dirty && (
        <div className="flex justify-end">
          <Button onClick={handleSave} disabled={saving} className="gap-1">
            <Save className="h-3.5 w-3.5" /> {saving ? t("saving") : t("saveChanges")}
          </Button>
        </div>
      )}
    </div>
  );
}
