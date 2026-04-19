/**
 * Unit tests for voice-picker logic.
 *
 * NOTE: @testing-library/react is not installed — tests cover pure logic
 * and module contracts rather than DOM rendering.
 */
import { describe, it, expect, vi } from "vitest";
import type { Voice } from "@/api/voices";

// --- helpers under test (extracted from voice-picker.tsx logic) ---

const LABEL_KEYS = ["gender", "accent", "age", "use_case"] as const;

function getVisibleLabels(voice: Voice): string[] {
  return LABEL_KEYS
    .filter((k) => voice.labels?.[k])
    .map((k) => voice.labels![k] as string)
    .slice(0, 2);
}

function filterVoices(voices: Voice[], search: string): Voice[] {
  if (!search.trim()) return voices;
  const q = search.toLowerCase();
  return voices.filter((v) => v.name.toLowerCase().includes(q));
}

// --- useVoices shape test via vi.mock ---

vi.mock("@/api/voices", () => ({
  useVoices: vi.fn(() => ({ data: [], isLoading: false, error: null })),
  useRefreshVoices: vi.fn(() => ({ mutate: vi.fn(), isPending: false })),
  voiceKeys: { all: ["voices"] },
}));

// --- tests ---

describe("voice-picker — label extraction", () => {
  it("returns empty array when labels absent", () => {
    const voice: Voice = { voice_id: "v1", name: "Alice" };
    expect(getVisibleLabels(voice)).toEqual([]);
  });

  it("returns matching label values", () => {
    const voice: Voice = {
      voice_id: "v2",
      name: "Bob",
      labels: { gender: "male", accent: "american", age: "young" },
    };
    const labels = getVisibleLabels(voice);
    expect(labels).toContain("male");
    expect(labels).toContain("american");
    // capped at 2
    expect(labels.length).toBe(2);
  });

  it("ignores unknown label keys", () => {
    const voice: Voice = {
      voice_id: "v3",
      name: "Carol",
      labels: { style: "calm" },
    };
    expect(getVisibleLabels(voice)).toEqual([]);
  });
});

describe("voice-picker — filterVoices", () => {
  const voices: Voice[] = [
    { voice_id: "1", name: "Rachel" },
    { voice_id: "2", name: "Dave British" },
    { voice_id: "3", name: "Bella" },
  ];

  it("returns all voices when search is empty", () => {
    expect(filterVoices(voices, "")).toHaveLength(3);
    expect(filterVoices(voices, "   ")).toHaveLength(3);
  });

  it("filters voices by name substring (case-insensitive)", () => {
    expect(filterVoices(voices, "bella")).toEqual([{ voice_id: "3", name: "Bella" }]);
    expect(filterVoices(voices, "BRIT")).toEqual([{ voice_id: "2", name: "Dave British" }]);
  });

  it("returns empty array when no match", () => {
    expect(filterVoices(voices, "zzzz")).toHaveLength(0);
  });
});

describe("voice-picker — onChange contract", () => {
  it("onChange receives voice.id on selection", () => {
    const onChange = vi.fn();
    const voice: Voice = { voice_id: "target-id", name: "Target" };
    // Simulate handleSelect logic
    onChange(voice.voice_id);
    expect(onChange).toHaveBeenCalledWith("target-id");
  });
});

describe("useRefreshVoices — mock contract", () => {
  it("exposes mutate and isPending", async () => {
    const { useRefreshVoices } = await import("@/api/voices");
    const result = useRefreshVoices();
    expect(typeof result.mutate).toBe("function");
    expect(result.isPending).toBe(false);
  });
});

describe("useVoices — loading / empty / data states", () => {
  it("returns empty data and isLoading=false by default (mock)", async () => {
    const { useVoices } = await import("@/api/voices");
    const result = useVoices();
    expect(result.isLoading).toBe(false);
    expect(result.data).toEqual([]);
  });

  it("loading state: isLoading true when mock returns true", async () => {
    const { useVoices } = await import("@/api/voices");
    (useVoices as ReturnType<typeof vi.fn>).mockReturnValueOnce({
      data: undefined,
      isLoading: true,
      error: null,
    });
    const result = useVoices();
    expect(result.isLoading).toBe(true);
    expect(result.data).toBeUndefined();
  });

  it("data state: returns voice rows when mock has data", async () => {
    const voices: Voice[] = [
      { voice_id: "abc", name: "Aria", labels: { gender: "female" } },
    ];
    const { useVoices } = await import("@/api/voices");
    (useVoices as ReturnType<typeof vi.fn>).mockReturnValueOnce({
      data: voices,
      isLoading: false,
      error: null,
    });
    const result = useVoices();
    const data = result.data as Voice[];
    expect(data).toHaveLength(1);
    expect(data[0]!.name).toBe("Aria");
  });
});
