import { describe, it, expect } from "vitest";
import {
  filterTagsByCategory,
  searchTags,
  insertTagAtCaret,
  GEMINI_AUDIO_TAGS,
} from "../../data/gemini-audio-tags";

describe("filterTagsByCategory", () => {
  it("returns only emotion entries when category='emotion'", () => {
    const results = filterTagsByCategory("emotion");
    expect(results.length).toBeGreaterThan(0);
    results.forEach((t) => expect(t.category).toBe("emotion"));
  });

  it("returns all tags when no category provided", () => {
    expect(filterTagsByCategory()).toHaveLength(GEMINI_AUDIO_TAGS.length);
  });
});

describe("searchTags", () => {
  it("case-insensitive search for LAUGH returns laugh-related tags", () => {
    const results = searchTags("LAUGH");
    expect(results.length).toBeGreaterThan(0);
    results.forEach((t) =>
      expect(
        t.tag.toLowerCase().includes("laugh") ||
          t.description.toLowerCase().includes("laugh")
      ).toBe(true)
    );
  });

  it("search 'whisper' returns whisper tags", () => {
    const results = searchTags("whisper");
    expect(results.length).toBeGreaterThan(0);
  });
});

describe("insertTagAtCaret", () => {
  it("inserts tag at caret position in middle of text", () => {
    const text = "Hello world";
    const caretPos = 5; // after "Hello"
    const result = insertTagAtCaret(text, "[pause]", caretPos);
    expect(result).toContain("[pause]");
    expect(result.startsWith("Hello")).toBe(true);
  });

  it("inserts at position 0 without leading space", () => {
    const result = insertTagAtCaret("world", "[pause]", 0);
    expect(result.startsWith("[pause]")).toBe(true);
  });

  it("inserts at end without trailing space", () => {
    const result = insertTagAtCaret("Hello", "[pause]", 5);
    expect(result.endsWith("[pause]")).toBe(true);
  });
});

describe("readonly guard (pure logic)", () => {
  it("insertTagAtCaret when readonly should be caller responsibility — returns new text regardless", () => {
    // The readonly guard is enforced at the component level (onInsert not called).
    // Pure function always returns modified text; component skips calling it.
    const text = "Hello world";
    const result = insertTagAtCaret(text, "[pause]", 5);
    expect(result).not.toBe(text);
  });
});
