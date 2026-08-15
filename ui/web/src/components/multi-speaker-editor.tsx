/**
 * MultiSpeakerEditor — configures up to 2 Gemini TTS speakers.
 * Exports pure-logic helpers (addSpeaker, removeSpeaker, updateSpeakerVoice)
 * so they can be unit-tested without a DOM environment.
 */
import { useTranslation } from "react-i18next";
import { X, Plus } from "lucide-react";
import { cn } from "@/lib/utils";

// ---- Pure logic exports (testable without React) ----

export interface SpeakerVoice {
  speaker: string;
  voiceId: string;
}

/** Maximum speakers Gemini TTS supports in multi-speaker mode. */
export const MAX_GEMINI_SPEAKERS = 2;

/** Returns a new array with speaker appended, capped at MAX_GEMINI_SPEAKERS. */
export function addSpeaker(speakers: SpeakerVoice[], entry: SpeakerVoice): SpeakerVoice[] {
  if (speakers.length >= MAX_GEMINI_SPEAKERS) return speakers;
  return [...speakers, entry];
}

/** Returns a new array with the speaker at `index` removed. */
export function removeSpeaker(speakers: SpeakerVoice[], index: number): SpeakerVoice[] {
  return speakers.filter((_, i) => i !== index);
}

/** Returns a new array with the voiceId at `index` replaced. */
export function updateSpeakerVoice(
  speakers: SpeakerVoice[],
  index: number,
  voiceId: string
): SpeakerVoice[] {
  return speakers.map((s, i) => (i === index ? { ...s, voiceId } : s));
}

// ---- React component ----

interface MultiSpeakerEditorProps {
  speakers: SpeakerVoice[];
  voices: string[];          // available voice IDs for the select
  onChange: (speakers: SpeakerVoice[]) => void;
  readonly?: boolean;
  className?: string;
}

export function MultiSpeakerEditor({
  speakers,
  voices,
  onChange,
  readonly = false,
  className,
}: MultiSpeakerEditorProps) {
  const { t } = useTranslation("tts");
  const atMax = speakers.length >= MAX_GEMINI_SPEAKERS;

  const handleAdd = () => {
    if (readonly || atMax) return;
    const next = addSpeaker(speakers, { speaker: "", voiceId: voices[0] ?? "" });
    onChange(next);
  };

  const handleRemove = (idx: number) => {
    if (readonly) return;
    onChange(removeSpeaker(speakers, idx));
  };

  const handleNameChange = (idx: number, name: string) => {
    if (readonly) return;
    onChange(speakers.map((s, i) => (i === idx ? { ...s, speaker: name } : s)));
  };

  const handleVoiceChange = (idx: number, voiceId: string) => {
    if (readonly) return;
    onChange(updateSpeakerVoice(speakers, idx, voiceId));
  };

  return (
    <div className={cn("space-y-2", className)}>
      <p className="text-xs text-muted-foreground">{t("gemini.multiSpeakerHint")}</p>

      {speakers.map((s, idx) => (
        <div key={idx} className="flex items-center gap-2">
          <input
            type="text"
            value={s.speaker}
            onChange={(e) => handleNameChange(idx, e.target.value)}
            placeholder={t("gemini.speakerName")}
            disabled={readonly}
            className="flex-1 rounded-md border border-input bg-background px-3 py-1.5 text-sm text-base md:text-sm"
          />
          <select
            value={s.voiceId}
            onChange={(e) => handleVoiceChange(idx, e.target.value)}
            disabled={readonly}
            className="flex-1 rounded-md border border-input bg-background px-2 py-1.5 text-sm text-base md:text-sm"
          >
            {voices.map((v) => (
              <option key={v} value={v}>{v}</option>
            ))}
          </select>
          {!readonly && (
            <button
              type="button"
              onClick={() => handleRemove(idx)}
              className="shrink-0 rounded p-1 hover:bg-destructive/10 text-destructive"
              title={t("gemini.removeSpeaker")}
            >
              <X className="h-4 w-4" />
            </button>
          )}
        </div>
      ))}

      {!readonly && (
        <button
          type="button"
          onClick={handleAdd}
          disabled={atMax}
          className={cn(
            "flex items-center gap-1 text-sm text-primary hover:underline",
            atMax && "cursor-not-allowed opacity-50"
          )}
        >
          <Plus className="h-4 w-4" />
          {atMax ? t("gemini.maxSpeakersReached") : t("gemini.addSpeaker")}
        </button>
      )}
    </div>
  );
}
