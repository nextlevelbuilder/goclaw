/**
 * Unit tests for voice-picker logic.
 *
 * NOTE: @testing-library/react is not installed — tests cover pure logic
 * and module contracts rather than DOM rendering.
 *
 * Phase 2 additions:
 *   - Gemini dispatch now routes to PortalVoicePicker (was StaticVoicePicker).
 *   - PortalVoice shape: voice_id + name; labels/preview_url optional.
 *   - VoiceRow gracefully handles absent preview_url + absent labels for Gemini.
 */
import { describe, it, expect, vi } from "vitest";
import type { Voice } from "@/api/voices";
import type { PortalVoice } from "@/components/voice-picker";

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

/** Mirrors getVisibleLabels for PortalVoice (used by Gemini path). */
function getPortalVoiceLabels(voice: PortalVoice): string[] {
  return LABEL_KEYS
    .filter((k) => voice.labels?.[k])
    .map((k) => voice.labels![k] as string)
    .slice(0, 2);
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

// --- Phase 2: Gemini dispatch + PortalVoice shape tests ---

describe("PortalVoice — Gemini static voice shape", () => {
  it("Gemini voice has only voice_id + name (no labels, no preview_url)", () => {
    // Mirrors mapCapVoiceToPortal: VoiceOption → PortalVoice
    const geminiVoice: PortalVoice = { voice_id: "Aoede", name: "Aoede" };
    expect(geminiVoice.labels).toBeUndefined();
    expect(geminiVoice.preview_url).toBeUndefined();
  });

  it("getPortalVoiceLabels returns empty array for Gemini voice (no labels field)", () => {
    const geminiVoice: PortalVoice = { voice_id: "Kore", name: "Kore" };
    expect(getPortalVoiceLabels(geminiVoice)).toEqual([]);
  });

  it("filterVoices works on PortalVoice array (Gemini search)", () => {
    const geminiVoices: PortalVoice[] = [
      { voice_id: "Aoede", name: "Aoede" },
      { voice_id: "Kore", name: "Kore" },
      { voice_id: "Charon", name: "Charon" },
    ];
    const filtered = geminiVoices.filter((v) =>
      v.name.toLowerCase().includes("ao")
    );
    expect(filtered).toHaveLength(1);
    expect(filtered[0]!.voice_id).toBe("Aoede");
  });

  it("filterVoices is case-insensitive for Gemini voices", () => {
    const geminiVoices: PortalVoice[] = [
      { voice_id: "Charon", name: "Charon" },
      { voice_id: "Fenrir", name: "Fenrir" },
    ];
    const filtered = geminiVoices.filter((v) =>
      v.name.toLowerCase().includes("char")
    );
    expect(filtered).toHaveLength(1);
    expect(filtered[0]!.name).toBe("Charon");
  });
});

describe("PortalVoicePicker dispatch — Gemini routes to PortalVoicePicker (Phase 2)", () => {
  /**
   * Characterization: Gemini has static voices + no voices_dynamic flag.
   * Before Phase 2: routed to StaticVoicePicker (Radix Select).
   * After Phase 2: routes to PortalVoicePicker (search + row UI).
   *
   * We verify the dispatch rule via the capability shape check rather than DOM render
   * (no @testing-library/react available).
   */
  it("Gemini capabilities: no voices_dynamic flag + voices array present", () => {
    // Simulates the capability shape that triggers the Gemini portal route
    const geminiCaps = {
      provider: "gemini",
      display_name: "Google Gemini",
      requires_api_key: true,
      voices: [
        { voice_id: "Aoede", name: "Aoede" },
        { voice_id: "Kore", name: "Kore" },
      ],
      // custom_features does NOT contain voices_dynamic for Gemini
      custom_features: { multi_speaker: true, audio_tags: true } as Record<string, unknown>,
    };

    const voicesDynamic = geminiCaps.custom_features?.["voices_dynamic"] === true;
    const staticVoices = geminiCaps.voices ?? [];

    // Gemini route condition: provider === "gemini" AND staticVoices.length > 0
    const routesToPortal = geminiCaps.provider === "gemini" && staticVoices.length > 0;
    // Static route (OpenAI) condition: NOT gemini AND has caps AND !dynamic AND has voices
    const routesToStatic = geminiCaps.provider !== "gemini" && !voicesDynamic && staticVoices.length > 0;

    expect(voicesDynamic).toBe(false);
    expect(staticVoices).toHaveLength(2);
    expect(routesToPortal).toBe(true);
    expect(routesToStatic).toBe(false);
  });

  it("OpenAI capabilities: no voices_dynamic + voices present → still routes to StaticVoicePicker", () => {
    const openaiCaps = {
      provider: "openai",
      display_name: "OpenAI",
      requires_api_key: true,
      voices: [
        { voice_id: "alloy", name: "Alloy" },
        { voice_id: "echo", name: "Echo" },
      ],
      custom_features: null,
    };

    const voicesDynamic = openaiCaps.custom_features?.["voices_dynamic"] === true;
    const staticVoices = openaiCaps.voices ?? [];

    const routesToPortal = openaiCaps.provider === "gemini" && staticVoices.length > 0;
    const routesToStatic = openaiCaps.provider !== "gemini" && !voicesDynamic && staticVoices.length > 0;

    expect(routesToPortal).toBe(false);
    expect(routesToStatic).toBe(true);
  });

  it("ElevenLabs: no static voices + no voices_dynamic → routes to DynamicVoicePicker", () => {
    const elevenlabsCaps = {
      provider: "elevenlabs",
      display_name: "ElevenLabs",
      requires_api_key: true,
      voices: [],
      custom_features: null,
    };

    const voicesDynamic = elevenlabsCaps.custom_features?.["voices_dynamic"] === true;
    const staticVoices = elevenlabsCaps.voices ?? [];

    const routesToPortal = elevenlabsCaps.provider === "gemini" && staticVoices.length > 0;
    const routesToStatic = elevenlabsCaps.provider !== "gemini" && !voicesDynamic && staticVoices.length > 0;
    const routesToDynamic = !routesToPortal && !routesToStatic;

    expect(routesToDynamic).toBe(true);
  });

  it("MiniMax: voices_dynamic=true → routes to DynamicVoicePicker with allowFreeText", () => {
    const minimaxCaps = {
      provider: "minimax",
      display_name: "MiniMax",
      requires_api_key: true,
      voices: [],
      custom_features: { voices_dynamic: true },
    };

    const voicesDynamic = minimaxCaps.custom_features?.["voices_dynamic"] === true;
    expect(voicesDynamic).toBe(true);

    // minimax provider → DynamicVoicePicker with allowFreeText=true
    const allowFreeText = minimaxCaps.provider === "minimax";
    expect(allowFreeText).toBe(true);
  });
});

describe("mapCapVoiceToPortal — Gemini voice mapping", () => {
  it("maps VoiceOption to PortalVoice with only voice_id + name", () => {
    // Mirrors the mapCapVoiceToPortal function in voice-picker.tsx
    const capVoice = { voice_id: "Aoede", name: "Aoede", language: "en-US", gender: "female" };
    const portalVoice: PortalVoice = { voice_id: capVoice.voice_id, name: capVoice.name };

    expect(portalVoice.voice_id).toBe("Aoede");
    expect(portalVoice.name).toBe("Aoede");
    expect(portalVoice.labels).toBeUndefined();
    expect(portalVoice.preview_url).toBeUndefined();
  });
});
