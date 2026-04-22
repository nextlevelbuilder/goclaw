/**
 * TTS configuration page — 4-step guided flow:
 *   1. Provider selection (ProviderSetup)
 *   2. Credentials (CredentialsSection) — skipped for Edge (no API key)
 *   3. Voice & Model (VoiceModelSection)
 *   4. Test Playground (TestPlayground)
 *   + Advanced collapsible: auto mode, reply mode, limits (BehaviorSection)
 *
 * State is lifted here: draft + dirty tracking. Sections are controlled components.
 * Save flow unchanged: CONFIG_PATCH WS call via useTtsConfig().save().
 */
import { useState, useEffect } from "react";
import { useTranslation } from "react-i18next";
import { RefreshCw, Save } from "lucide-react";
import { Button } from "@/components/ui/button";
import { PageHeader } from "@/components/shared/page-header";
import { TableSkeleton } from "@/components/shared/loading-skeleton";
import { useTtsConfig, type TtsConfig, type TtsProviderConfig } from "./hooks/use-tts-config";
import { useMinLoading } from "@/hooks/use-min-loading";
import { useDeferredLoading } from "@/hooks/use-deferred-loading";
import { ProviderSetup } from "./sections/provider-setup";
import { CredentialsSection } from "./sections/credentials-section";
import { VoiceModelSection } from "./sections/voice-model-section";
import { TestPlayground } from "./sections/test-playground";
import { BehaviorSection } from "./sections/behavior-section";

// Per-provider helpers: extract voice/model id from the provider sub-config
function getVoiceId(draft: TtsConfig): string {
  switch (draft.provider) {
    case "openai": return draft.openai.voice ?? "";
    case "elevenlabs": return draft.elevenlabs.voice_id ?? "";
    case "edge": return draft.edge.voice ?? "";
    case "minimax": return draft.minimax.voice_id ?? "";
    default: return "";
  }
}

function getModelId(draft: TtsConfig): string {
  switch (draft.provider) {
    case "openai": return draft.openai.model ?? "";
    case "elevenlabs": return draft.elevenlabs.model_id ?? "";
    case "minimax": return draft.minimax.model ?? "";
    default: return "";
  }
}

type ProviderKey = keyof Pick<TtsConfig, "openai" | "elevenlabs" | "edge" | "minimax">;

function voicePatch(provider: string, value: string): [ProviderKey, Partial<TtsProviderConfig>] | null {
  switch (provider) {
    case "openai": return ["openai", { voice: value }];
    case "elevenlabs": return ["elevenlabs", { voice_id: value }];
    case "edge": return ["edge", { voice: value }];
    case "minimax": return ["minimax", { voice_id: value }];
    default: return null;
  }
}

function modelPatch(provider: string, value: string): [ProviderKey, Partial<TtsProviderConfig>] | null {
  switch (provider) {
    case "openai": return ["openai", { model: value }];
    case "elevenlabs": return ["elevenlabs", { model_id: value }];
    case "minimax": return ["minimax", { model: value }];
    default: return null;
  }
}

// Check if provider credentials are saved (to show lower sections)
function isCredentialsSaved(provider: string, tts: TtsConfig): boolean {
  if (provider === "edge") return true; // Edge doesn't require API key
  const cfg = tts[provider as ProviderKey];
  return !!cfg?.api_key;
}

export function TtsPage() {
  const { t } = useTranslation("tts");
  const { t: tc } = useTranslation("common");
  const { tts, loading, saving, error, refresh, save, synthesize, testConnection } = useTtsConfig();
  const spinning = useMinLoading(loading);

  const [draft, setDraft] = useState<TtsConfig>(tts);
  const showSkeleton = useDeferredLoading(loading && !draft.provider);
  const [dirty, setDirty] = useState(false);

  useEffect(() => {
    setDraft(tts);
    setDirty(false);
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

  const handleSave = async () => {
    await save(draft);
    setDirty(false);
  };

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
        <VoiceModelSection
          provider={draft.provider}
          voiceId={getVoiceId(draft)}
          modelId={getModelId(draft)}
          onVoiceChange={handleVoiceChange}
          onModelChange={handleModelChange}
        />
      )}

      {draft.provider && isCredentialsSaved(draft.provider, tts) && (
        <TestPlayground
          provider={draft.provider}
          voiceId={getVoiceId(draft)}
          modelId={getModelId(draft)}
          synthesize={synthesize}
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
