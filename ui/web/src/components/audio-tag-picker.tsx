/**
 * AudioTagPicker — browse and insert Gemini audio style tags.
 * Tags are inline markers inserted at caret position in a target textarea.
 * Supports readonly prop for desktop viewer mode.
 */
import { useState } from "react";
import { useTranslation } from "react-i18next";
import { cn } from "@/lib/utils";
import {
  GEMINI_AUDIO_TAGS,
  filterTagsByCategory,
  searchTags,
  insertTagAtCaret,
  type AudioTag,
} from "../data/gemini-audio-tags";

type Category = AudioTag["category"] | "all";

interface AudioTagPickerProps {
  /** Called when user clicks a tag. Passes the tag string to insert. */
  onInsert?: (tag: string) => void;
  /** Current caret position in the target textarea (for onInsert context). */
  caretPos?: number;
  /** Current value of the target textarea (for direct insert). */
  textValue?: string;
  /** Called with the new full text value after tag insertion. */
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
  readonly = false,
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
    <div className={cn("space-y-2", className)}>
      <p className="text-xs text-muted-foreground">{t("gemini.audioTagsHint")}</p>

      {/* Search */}
      <input
        type="text"
        value={query}
        onChange={(e) => setQuery(e.target.value)}
        placeholder={t("gemini.searchPlaceholder")}
        disabled={readonly}
        className="w-full rounded-md border border-input bg-background px-3 py-1.5 text-sm text-base md:text-sm"
      />

      {/* Category filter (hidden when searching) */}
      {!query.trim() && (
        <div className="flex flex-wrap gap-1">
          {CATEGORIES.map(({ key, labelKey }) => (
            <button
              key={key}
              type="button"
              onClick={() => setActiveCategory(key)}
              className={cn(
                "rounded-full px-2 py-0.5 text-xs border transition-colors",
                activeCategory === key
                  ? "bg-primary text-primary-foreground border-primary"
                  : "border-border hover:bg-muted"
              )}
            >
              {t(labelKey)}
            </button>
          ))}
        </div>
      )}

      {/* Tag grid */}
      <div className="flex flex-wrap gap-1.5 max-h-40 overflow-y-auto">
        {tags.map((tag) => (
          <button
            key={tag.tag}
            type="button"
            title={tag.description}
            onClick={() => handleInsert(tag.tag)}
            disabled={readonly}
            className={cn(
              "rounded px-2 py-0.5 text-xs border border-border font-mono transition-colors",
              readonly
                ? "cursor-default opacity-60"
                : "hover:bg-primary hover:text-primary-foreground hover:border-primary cursor-pointer"
            )}
          >
            {tag.tag}
          </button>
        ))}
      </div>
    </div>
  );
}
