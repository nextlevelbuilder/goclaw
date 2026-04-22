import { useState, useEffect } from "react";
import { useTranslation } from "react-i18next";
import { Button } from "@/components/ui/button";
import { toast } from "@/stores/use-toast-store";
import { useAuthStore } from "@/stores/use-auth-store";
import type { AgentData } from "@/types/agent";
import { readPromptMode } from "../agent-display-utils";
import { PromptModeCards, type PromptMode } from "../../prompt-mode-cards";
import { useTtsConfig } from "@/pages/tts/hooks/use-tts-config";
import { TtsEmptyState } from "./tts-empty-state";
import { TtsOverrideBlock } from "./tts-override-block";
import { getProviderDefinition, type TtsProviderId } from "@/data/tts-providers";
import type { TtsModelOption } from "@/data/tts-providers";

/**
 * Pure helper — exported for unit testing.
 * Returns true when TTS is configured globally and the TTS subsection should render.
 */
export function shouldRenderTTSSection(globalTts: { provider?: string }): boolean {
  return !!globalTts.provider;
}

/**
 * Pure helper — exported for unit testing.
 * Returns model options for a given provider id from the catalog.
 */
export function getModelOptions(providerId: string): TtsModelOption[] {
  const def = getProviderDefinition(providerId as TtsProviderId);
  return def?.models ?? [];
}

interface Props {
  agent: AgentData;
  onUpdate: (updates: Record<string, unknown>) => Promise<void>;
}

export function PromptSettingsSection({ agent, onUpdate }: Props) {
  const { t } = useTranslation("agents");
  const { t: tTts } = useTranslation("tts");
  const isOwner = useAuthStore((s) => s.isOwner);
  const { tts: globalTts, synthesize } = useTtsConfig();
  const globalProvider = globalTts.provider;

  const otherConfig = (agent.other_config ?? {}) as Record<string, unknown>;
  const savedMode = readPromptMode(agent) as PromptMode;
  const savedVoiceId = (otherConfig.tts_voice_id as string) ?? "";
  const savedModelId = (otherConfig.tts_model_id as string) ?? "";

  const [mode, setMode] = useState<PromptMode>(savedMode);
  const [ttsVoiceId, setTtsVoiceId] = useState<string>(savedVoiceId);
  const [ttsModelId, setTtsModelId] = useState<string>(savedModelId);
  const [override, setOverride] = useState<boolean>(!!(savedVoiceId || savedModelId));
  const [saving, setSaving] = useState(false);

  useEffect(() => {
    const cfg = (agent.other_config ?? {}) as Record<string, unknown>;
    const voice = (cfg.tts_voice_id as string) ?? "";
    const model = (cfg.tts_model_id as string) ?? "";
    setMode(readPromptMode(agent) as PromptMode);
    setTtsVoiceId(voice);
    setTtsModelId(model);
    setOverride(!!(voice || model));
  }, [agent.other_config]);

  const savedOverride = !!(savedVoiceId || savedModelId);
  const dirty =
    mode !== savedMode ||
    ttsVoiceId !== savedVoiceId ||
    ttsModelId !== savedModelId ||
    override !== savedOverride;

  const handleSave = async () => {
    setSaving(true);
    try {
      const bag = { ...otherConfig };
      if (mode && mode !== "full") {
        bag.prompt_mode = mode;
      } else {
        delete bag.prompt_mode;
      }
      // Write TTS override only when checkbox enabled and value provided
      if (override && ttsVoiceId) {
        bag.tts_voice_id = ttsVoiceId;
      } else {
        delete bag.tts_voice_id;
      }
      if (override && ttsModelId) {
        bag.tts_model_id = ttsModelId;
      } else {
        delete bag.tts_model_id;
      }
      await onUpdate({ other_config: bag });
      const modeRank: Record<string, number> = { none: 0, minimal: 1, task: 2, full: 3 };
      if ((modeRank[mode] ?? 3) > (modeRank[savedMode] ?? 3)) {
        toast.info(
          t(
            "detail.prompt.upgradeWarning",
            "Mode upgraded. Some files may need regeneration — use Resummon or Edit with AI in the Files tab.",
          ),
        );
      }
    } finally {
      setSaving(false);
    }
  };

  return (
    <section className="space-y-3 rounded-lg border p-3 sm:p-4">
      <div className="flex items-center justify-between">
        <h3 className="text-sm font-medium">{t("detail.prompt.title")}</h3>
        {dirty && (
          <Button size="sm" onClick={handleSave} disabled={saving}>
            {saving ? t("saving", "Saving...") : t("save", "Save")}
          </Button>
        )}
      </div>

      <PromptModeCards value={mode} onChange={setMode} />

      {/* TTS subsection */}
      <div className="space-y-3 border-t pt-3">
        <h4 className="text-xs font-medium text-muted-foreground uppercase tracking-wide">
          {tTts("title")}
        </h4>
        {!globalProvider ? (
          <TtsEmptyState isOwner={isOwner} />
        ) : (
          <TtsOverrideBlock
            globalProvider={globalProvider}
            voiceId={ttsVoiceId}
            modelId={ttsModelId}
            onVoiceChange={setTtsVoiceId}
            onModelChange={setTtsModelId}
            overrideEnabled={override}
            onOverrideChange={setOverride}
            synthesize={synthesize}
          />
        )}
      </div>
    </section>
  );
}
