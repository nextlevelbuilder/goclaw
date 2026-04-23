/**
 * AudioTagPicker — desktop mirror of web component.
 * Renders in readonly mode by default when mounted from desktop TTS page.
 */
import { useState } from "react";
import { useTranslation } from "react-i18next";
import {
  GEMINI_AUDIO_TAGS,
  filterTagsByCategory,
  searchTags,
  insertTagAtCaret,
  type AudioTag,
} from "../data/gemini-audio-tags";

type Category = AudioTag["category"] | "all";

interface AudioTagPickerProps {
  onInsert?: (tag: string) => void;
  caretPos?: number;
  textValue?: string;
  onTextChange?: (newText: string) => void;
  readonly?: boolean;
  className?: string;
}

const CATEGORIES: { key: Category; labelKey: string }[] = [
  { key: "all", labelKey: "gemini.filterAll" },
  { key: "emotion", labelKey: "gemini.filterEmotion" },
  { key: "pacing", labelKey: "gemini.filterPacing" },
  { key: "effect", labelKey: "gemini.filterEffect" },
  { key: "voice_quality", labelKey: "gemini.filterVoiceQuality" },
];

export function AudioTagPicker({
  onInsert,
  caretPos = 0,
  textValue = "",
  onTextChange,
  readonly = true, // desktop defaults to readonly until Phase C wires it
  className,
}: AudioTagPickerProps) {
  const { t } = useTranslation("tts");
  const [query, setQuery] = useState("");
  const [activeCategory, setActiveCategory] = useState<Category>("all");

  const tags: AudioTag[] =
    query.trim()
      ? searchTags(query)
      : activeCategory === "all"
        ? GEMINI_AUDIO_TAGS
        : filterTagsByCategory(activeCategory);

  const handleInsert = (tag: string) => {
    if (readonly) return;
    if (onTextChange && textValue !== undefined) {
      onTextChange(insertTagAtCaret(textValue, tag, caretPos));
    }
    onInsert?.(tag);
  };

  return (
    <div className={className} style={{ display: "flex", flexDirection: "column", gap: "0.5rem" }}>
      <p style={{ fontSize: "0.75rem", opacity: 0.7 }}>{t("gemini.audioTagsHint")}</p>

      <input
        type="text"
        value={query}
        onChange={(e) => setQuery(e.target.value)}
        placeholder={t("gemini.searchPlaceholder")}
        disabled={readonly}
      />

      {!query.trim() && (
        <div style={{ display: "flex", flexWrap: "wrap", gap: "0.25rem" }}>
          {CATEGORIES.map(({ key, labelKey }) => (
            <button
              key={key}
              type="button"
              onClick={() => setActiveCategory(key)}
              style={{
                fontWeight: activeCategory === key ? "bold" : "normal",
                fontSize: "0.75rem",
              }}
            >
              {t(labelKey)}
            </button>
          ))}
        </div>
      )}

      <div style={{ display: "flex", flexWrap: "wrap", gap: "0.375rem", maxHeight: "10rem", overflowY: "auto" }}>
        {tags.map((tag) => (
          <button
            key={tag.tag}
            type="button"
            title={tag.description}
            onClick={() => handleInsert(tag.tag)}
            disabled={readonly}
            style={{
              fontFamily: "monospace",
              fontSize: "0.75rem",
              opacity: readonly ? 0.6 : 1,
              cursor: readonly ? "default" : "pointer",
            }}
          >
            {tag.tag}
          </button>
        ))}
      </div>
    </div>
  );
}
