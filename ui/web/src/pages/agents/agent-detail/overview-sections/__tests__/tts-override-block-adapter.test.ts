/**
 * Pure-logic tests for the bidirectional adapter in tts-override-block.tsx.
 *
 * The adapter converts between:
 *   - Storage (generic keys: speed, emotion, style) — what agents.other_config.tts_params stores
 *   - Form state (capability-native keys: voice_settings.speed, etc.) — what DynamicParamForm uses
 *
 * Tests cover all 5 providers and the round-trip invariant.
 *
 * NOTE: Since Finding #9 fix, the adapter functions accept an overridableParams array
 * (not a provider string). Provider-specific mapping is now encoded in each param's
 * agent_overridable_as field — no separate hard-coded lookup table.
 */
import { describe, it, expect } from "vitest";
import {
  genericToNativeFormState,
  nativeFormStateToGeneric,
  buildAdapterMaps,
} from "../tts-override-block";

// ---- Per-provider overridable param stubs ----
// These mirror the capabilities declared in the Go provider files.

const openaiOverridableParams = [
  { key: "speed", agent_overridable_as: "speed" },
];

const elevenLabsOverridableParams = [
  { key: "voice_settings.style", agent_overridable_as: "style" },
  { key: "voice_settings.speed", agent_overridable_as: "speed" },
];

const minimaxOverridableParams = [
  { key: "speed", agent_overridable_as: "speed" },
  { key: "emotion", agent_overridable_as: "emotion" },
];

const edgeOverridableParams: Array<{ key: string; agent_overridable_as?: string }> = [];
const geminiOverridableParams: Array<{ key: string; agent_overridable_as?: string }> = [];

// ---- buildAdapterMaps ----

describe("buildAdapterMaps", () => {
  it("openai: builds correct forward and inverse maps", () => {
    const { genericToNative, nativeToGeneric } = buildAdapterMaps(openaiOverridableParams);
    expect(genericToNative).toEqual({ speed: "speed" });
    expect(nativeToGeneric).toEqual({ speed: "speed" });
  });

  it("elevenlabs: builds correct forward and inverse maps", () => {
    const { genericToNative, nativeToGeneric } = buildAdapterMaps(elevenLabsOverridableParams);
    expect(genericToNative).toEqual({
      style: "voice_settings.style",
      speed: "voice_settings.speed",
    });
    expect(nativeToGeneric).toEqual({
      "voice_settings.style": "style",
      "voice_settings.speed": "speed",
    });
  });

  it("empty params → empty maps", () => {
    const { genericToNative, nativeToGeneric } = buildAdapterMaps([]);
    expect(genericToNative).toEqual({});
    expect(nativeToGeneric).toEqual({});
  });
});

// ---- genericToNativeFormState (load direction: storage → form) ----

describe("genericToNativeFormState", () => {
  it("openai: speed stays flat", () => {
    expect(genericToNativeFormState({ speed: 1.5 }, openaiOverridableParams)).toEqual({ speed: 1.5 });
  });

  it("openai: emotion dropped (not supported)", () => {
    const out = genericToNativeFormState({ speed: 1.5, emotion: "happy" }, openaiOverridableParams);
    expect(out).toEqual({ speed: 1.5 });
  });

  it("elevenlabs: speed → voice_settings.speed", () => {
    expect(genericToNativeFormState({ speed: 1.1 }, elevenLabsOverridableParams)).toEqual({
      "voice_settings.speed": 1.1,
    });
  });

  it("elevenlabs: style → voice_settings.style", () => {
    expect(genericToNativeFormState({ style: 0.5 }, elevenLabsOverridableParams)).toEqual({
      "voice_settings.style": 0.5,
    });
  });

  it("elevenlabs: emotion dropped", () => {
    const out = genericToNativeFormState({ speed: 1.0, style: 0.3, emotion: "happy" }, elevenLabsOverridableParams);
    expect(out).toEqual({ "voice_settings.speed": 1.0, "voice_settings.style": 0.3 });
  });

  it("minimax: speed stays flat", () => {
    expect(genericToNativeFormState({ speed: 0.9 }, minimaxOverridableParams)).toEqual({ speed: 0.9 });
  });

  it("minimax: emotion stays flat", () => {
    expect(genericToNativeFormState({ emotion: "neutral" }, minimaxOverridableParams)).toEqual({ emotion: "neutral" });
  });

  it("minimax: style dropped", () => {
    const out = genericToNativeFormState({ speed: 1.0, emotion: "happy", style: 0.5 }, minimaxOverridableParams);
    expect(out).toEqual({ speed: 1.0, emotion: "happy" });
  });

  it("edge: all keys dropped", () => {
    expect(genericToNativeFormState({ speed: 1.0, emotion: "happy", style: 0.5 }, edgeOverridableParams)).toEqual({});
  });

  it("gemini: all keys dropped", () => {
    expect(genericToNativeFormState({ speed: 1.0, emotion: "happy", style: 0.5 }, geminiOverridableParams)).toEqual({});
  });

  it("empty input → empty output", () => {
    expect(genericToNativeFormState({}, openaiOverridableParams)).toEqual({});
  });

  it("unknown provider (empty params) → all keys dropped", () => {
    expect(genericToNativeFormState({ speed: 1.0 }, [])).toEqual({});
  });
});

// ---- nativeFormStateToGeneric (save direction: form → storage) ----

describe("nativeFormStateToGeneric", () => {
  it("openai: flat speed → generic speed", () => {
    expect(nativeFormStateToGeneric({ speed: 1.5 }, openaiOverridableParams)).toEqual({ speed: 1.5 });
  });

  it("elevenlabs: voice_settings.speed → generic speed", () => {
    expect(nativeFormStateToGeneric({ "voice_settings.speed": 1.1 }, elevenLabsOverridableParams)).toEqual({
      speed: 1.1,
    });
  });

  it("elevenlabs: voice_settings.style → generic style", () => {
    expect(nativeFormStateToGeneric({ "voice_settings.style": 0.4 }, elevenLabsOverridableParams)).toEqual({
      style: 0.4,
    });
  });

  it("elevenlabs: both native keys → both generic keys", () => {
    const out = nativeFormStateToGeneric(
      { "voice_settings.speed": 1.0, "voice_settings.style": 0.3 },
      elevenLabsOverridableParams,
    );
    expect(out).toEqual({ speed: 1.0, style: 0.3 });
  });

  it("minimax: flat speed+emotion → generic", () => {
    expect(nativeFormStateToGeneric({ speed: 0.9, emotion: "neutral" }, minimaxOverridableParams)).toEqual({
      speed: 0.9,
      emotion: "neutral",
    });
  });

  it("edge: no mappings → empty", () => {
    expect(nativeFormStateToGeneric({ speed: 1.0 }, edgeOverridableParams)).toEqual({});
  });

  it("gemini: no mappings → empty", () => {
    expect(nativeFormStateToGeneric({ speed: 1.0 }, geminiOverridableParams)).toEqual({});
  });
});

// ---- Round-trip invariant ----
// For any generic params + overridableParams, load then save must return the same generic map.

describe("round-trip: genericToNative → nativeToGeneric", () => {
  const cases: Array<{
    label: string;
    params: Array<{ key: string; agent_overridable_as?: string }>;
    generic: Record<string, unknown>;
  }> = [
    { label: "openai/speed", params: openaiOverridableParams, generic: { speed: 1.5 } },
    { label: "elevenlabs/speed+style", params: elevenLabsOverridableParams, generic: { speed: 1.1, style: 0.4 } },
    { label: "minimax/speed+emotion", params: minimaxOverridableParams, generic: { speed: 0.9, emotion: "happy" } },
    { label: "edge/empty", params: edgeOverridableParams, generic: {} },
    { label: "gemini/empty", params: geminiOverridableParams, generic: {} },
    { label: "openai/empty-generic", params: openaiOverridableParams, generic: {} },
  ];

  for (const { label, params, generic } of cases) {
    it(`${label}: round-trip preserves supported keys`, () => {
      const nativeState = genericToNativeFormState(generic as Record<string, import("@/components/dynamic-param-form").ParamValue>, params);
      const backToGeneric = nativeFormStateToGeneric(nativeState, params);
      expect(backToGeneric).toEqual(generic);
    });
  }
});
