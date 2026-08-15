/**
 * Gemini TTS audio tag catalog.
 *
 * Source: https://ai.google.dev/gemini-api/docs/speech-generation (April 2026)
 * Note: Google does not publish an exhaustive list. This catalog includes all
 * officially documented tags plus commonly effective ones from community usage.
 * Tag count: 42 curated entries across 4 categories.
 *
 * Tags are inline text markers inserted directly in the synthesis text, e.g.:
 *   "Hello [laughs] how are you?"
 * No special API field — Gemini parses them from the `text` payload.
 */

export interface AudioTag {
  tag: string;          // the literal marker, e.g. "[laughs]"
  category: "emotion" | "pacing" | "effect" | "voice_quality";
  description: string;
}

export const GEMINI_AUDIO_TAGS: AudioTag[] = [
  // --- Emotion ---
  { tag: "[laughs]",       category: "emotion", description: "Natural laughter" },
  { tag: "[laughs softly]",category: "emotion", description: "Soft, gentle laughter" },
  { tag: "[giggles]",      category: "emotion", description: "Light giggles" },
  { tag: "[crying]",       category: "emotion", description: "Tearful, emotional tone" },
  { tag: "[excited]",      category: "emotion", description: "High-energy, enthusiastic" },
  { tag: "[excitedly]",    category: "emotion", description: "Speaking with excitement" },
  { tag: "[amazed]",       category: "emotion", description: "Surprised and impressed" },
  { tag: "[curious]",      category: "emotion", description: "Inquisitive tone" },
  { tag: "[serious]",      category: "emotion", description: "Grave, solemn tone" },
  { tag: "[panicked]",     category: "emotion", description: "Anxious, rushed delivery" },
  { tag: "[mischievously]",category: "emotion", description: "Playfully scheming tone" },
  { tag: "[sarcastic]",    category: "emotion", description: "Sarcastic delivery" },
  { tag: "[bored]",        category: "emotion", description: "Disinterested, flat tone" },
  { tag: "[reluctantly]",  category: "emotion", description: "Unwilling, hesitant" },
  { tag: "[trembling]",    category: "emotion", description: "Shaky, fearful voice" },
  { tag: "[gasp]",         category: "emotion", description: "Audible sharp intake of breath" },
  { tag: "[sighs]",        category: "emotion", description: "Audible exhale / sigh" },
  { tag: "[shouting]",     category: "emotion", description: "Loud, forceful delivery" },

  // --- Pacing ---
  { tag: "[very fast]",    category: "pacing", description: "Much faster than normal" },
  { tag: "[fast]",         category: "pacing", description: "Slightly faster delivery" },
  { tag: "[very slow]",    category: "pacing", description: "Much slower, deliberate pace" },
  { tag: "[slow]",         category: "pacing", description: "Slightly slower delivery" },
  { tag: "[pause]",        category: "pacing", description: "Brief pause in speech" },
  { tag: "[long pause]",   category: "pacing", description: "Longer pause / silence" },
  { tag: "[drawn out]",    category: "pacing", description: "Stretched, elongated syllables" },
  { tag: "[sarcastically, one painfully slow word at a time]",
                            category: "pacing", description: "Extremely slow sarcastic delivery" },

  // --- Effect ---
  { tag: "[cough]",        category: "effect", description: "Coughing sound" },
  { tag: "[yawn]",         category: "effect", description: "Yawning sound" },
  { tag: "[sniffles]",     category: "effect", description: "Sniffling, like after crying" },
  { tag: "[clears throat]",category: "effect", description: "Throat clearing sound" },
  { tag: "[gasps]",        category: "effect", description: "Sharp gasp" },
  { tag: "[exhales]",      category: "effect", description: "Audible exhale" },

  // --- Voice quality ---
  { tag: "[whispers]",     category: "voice_quality", description: "Quiet, hushed whisper" },
  { tag: "[whisper]",      category: "voice_quality", description: "Whispered tone" },
  { tag: "[tired]",        category: "voice_quality", description: "Weary, low-energy voice" },
  { tag: "[like a cartoon dog]",
                            category: "voice_quality", description: "Cartoonish dog-like voice" },
  { tag: "[like dracula]", category: "voice_quality", description: "Dramatic Transylvanian accent" },
  { tag: "[robotic]",      category: "voice_quality", description: "Mechanical, synthesized quality" },
  { tag: "[sing-song]",    category: "voice_quality", description: "Musical, lilting quality" },
  { tag: "[monotone]",     category: "voice_quality", description: "Flat, emotionless delivery" },
  { tag: "[breathy]",      category: "voice_quality", description: "Soft, airy vocal quality" },
  { tag: "[gruff]",        category: "voice_quality", description: "Rough, low, gravelly voice" },
];

/** Return tags filtered by category (undefined = all). */
export function filterTagsByCategory(
  category?: AudioTag["category"]
): AudioTag[] {
  if (!category) return GEMINI_AUDIO_TAGS;
  return GEMINI_AUDIO_TAGS.filter((t) => t.category === category);
}

/** Case-insensitive search across tag text and description. */
export function searchTags(query: string): AudioTag[] {
  const q = query.toLowerCase();
  return GEMINI_AUDIO_TAGS.filter(
    (t) =>
      t.tag.toLowerCase().includes(q) || t.description.toLowerCase().includes(q)
  );
}

/** Insert a tag at the caret position (|) within text.
 *  Caret is represented by the cursor index (not a literal `|` char).
 *  Returns the new text with tag inserted and a single space boundary on each side. */
export function insertTagAtCaret(text: string, tag: string, caretPos: number): string {
  const before = text.slice(0, caretPos);
  const after = text.slice(caretPos);
  const needSpaceBefore = before.length > 0 && !before.endsWith(" ");
  const needSpaceAfter = after.length > 0 && !after.startsWith(" ");
  const prefix = needSpaceBefore ? " " : "";
  const suffix = needSpaceAfter ? " " : "";
  return before + prefix + tag + suffix + after;
}
