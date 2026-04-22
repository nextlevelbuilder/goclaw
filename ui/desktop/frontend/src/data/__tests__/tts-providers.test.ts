/**
 * Desktop TTS provider catalog sanity tests.
 * Mirrors ui/web/src/data/__tests__/tts-providers.test.ts — keeps desktop
 * catalog in sync with the web catalog via identical assertions.
 */
import { describe, it, expect } from "vitest";
import { TTS_PROVIDERS, getProviderDefinition, type TtsProviderId } from "../tts-providers";

describe("Desktop TTS_PROVIDERS catalog", () => {
  const ids: TtsProviderId[] = ["openai", "elevenlabs", "edge", "minimax"];

  it("has exactly 4 provider entries matching known IDs", () => {
    expect(Object.keys(TTS_PROVIDERS).sort()).toEqual([...ids].sort());
  });

  it.each(ids)("%s.id matches its record key", (id) => {
    expect(TTS_PROVIDERS[id].id).toBe(id);
  });

  it("ElevenLabs is dynamic, others are not", () => {
    expect(TTS_PROVIDERS.elevenlabs.dynamic).toBe(true);
    expect(TTS_PROVIDERS.openai.dynamic).toBe(false);
    expect(TTS_PROVIDERS.edge.dynamic).toBe(false);
    expect(TTS_PROVIDERS.minimax.dynamic).toBe(false);
  });

  it.each(ids.filter((id) => TTS_PROVIDERS[id].models.length > 0))(
    "%s.defaultModel is in its models list",
    (id) => {
      const def = TTS_PROVIDERS[id];
      if (def.defaultModel) {
        const modelValues = def.models.map((m) => m.value);
        expect(modelValues).toContain(def.defaultModel);
      }
    },
  );

  it.each(ids.filter((id) => !TTS_PROVIDERS[id].dynamic))(
    "%s.defaultVoice is in its voices list",
    (id) => {
      const def = TTS_PROVIDERS[id];
      if (def.defaultVoice) {
        const voiceValues = def.voices.map((v) => v.value);
        expect(voiceValues).toContain(def.defaultVoice);
      }
    },
  );

  it("Edge requires no API key", () => {
    expect(TTS_PROVIDERS.edge.requiresApiKey).toBe(false);
  });

  it("OpenAI and MiniMax require an API key", () => {
    expect(TTS_PROVIDERS.openai.requiresApiKey).toBe(true);
    expect(TTS_PROVIDERS.minimax.requiresApiKey).toBe(true);
  });

  it("ElevenLabs exposes all 4 backend-allowlisted models", () => {
    const modelIds = TTS_PROVIDERS.elevenlabs.models.map((m) => m.value);
    expect(modelIds).toContain("eleven_v3");
    expect(modelIds).toContain("eleven_flash_v2_5");
    expect(modelIds).toContain("eleven_multilingual_v2");
    expect(modelIds).toContain("eleven_turbo_v2_5");
    expect(modelIds).toHaveLength(4);
  });

  it("OpenAI models include the 3 standard model IDs", () => {
    const ids = TTS_PROVIDERS.openai.models.map((m) => m.value);
    expect(ids).toContain("gpt-4o-mini-tts");
    expect(ids).toContain("tts-1");
    expect(ids).toContain("tts-1-hd");
  });

  it("Edge has 20 hardcoded voices", () => {
    expect(TTS_PROVIDERS.edge.voices).toHaveLength(20);
  });

  it("getProviderDefinition returns null for unknown id", () => {
    expect(getProviderDefinition("")).toBeNull();
    expect(getProviderDefinition("nonexistent")).toBeNull();
  });

  it("getProviderDefinition returns correct definition for known id", () => {
    expect(getProviderDefinition("openai")?.id).toBe("openai");
  });
});
