/**
 * MultiSpeakerEditor — desktop mirror of web component.
 * Renders in readonly mode by default when mounted from desktop TTS page.
 * Exports pure-logic helpers shared with tests.
 */
import { useTranslation } from "react-i18next";

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
  voices: string[];
  onChange: (speakers: SpeakerVoice[]) => void;
  readonly?: boolean;
  className?: string;
}

export function MultiSpeakerEditor({
  speakers,
  voices,
  onChange,
  readonly = true, // desktop defaults to readonly until Phase C wires it
  className,
}: MultiSpeakerEditorProps) {
  const { t } = useTranslation("tts");
  const atMax = speakers.length >= MAX_GEMINI_SPEAKERS;

  const handleAdd = () => {
    if (readonly || atMax) return;
    onChange(addSpeaker(speakers, { speaker: "", voiceId: voices[0] ?? "" }));
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
    <div className={className} style={{ display: "flex", flexDirection: "column", gap: "0.5rem" }}>
      <p style={{ fontSize: "0.75rem", opacity: 0.7 }}>{t("gemini.multiSpeakerHint")}</p>

      {speakers.map((s, idx) => (
        <div key={idx} style={{ display: "flex", gap: "0.5rem", alignItems: "center" }}>
          <input
            type="text"
            value={s.speaker}
            onChange={(e) => handleNameChange(idx, e.target.value)}
            placeholder={t("gemini.speakerName")}
            disabled={readonly}
            style={{ flex: 1 }}
          />
          <select
            value={s.voiceId}
            onChange={(e) => handleVoiceChange(idx, e.target.value)}
            disabled={readonly}
            style={{ flex: 1 }}
          >
            {voices.map((v) => (
              <option key={v} value={v}>{v}</option>
            ))}
          </select>
          {!readonly && (
            <button type="button" onClick={() => handleRemove(idx)}>
              {t("gemini.removeSpeaker")}
            </button>
          )}
        </div>
      ))}

      {!readonly && (
        <button type="button" onClick={handleAdd} disabled={atMax}>
          {atMax ? t("gemini.maxSpeakersReached") : t("gemini.addSpeaker")}
        </button>
      )}
    </div>
  );
}
