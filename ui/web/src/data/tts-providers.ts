/**
 * TTS provider display metadata — reduced shape (Phase C).
 *
 * REMOVED: models, voices, dynamic, defaultVoice, defaultModel, requiresApiKey.
 *   - Models/voices/params come from GET /v1/tts/capabilities (useTtsCapabilities hook).
 *   - requiresApiKey derived from capabilities.requires_api_key.
 *
 * KEPT: TtsProviderId union type for type narrowing; id, title, color, icon, desc.
 *
 * For consumers that need a synchronous model list fallback (e.g. prompt-settings-section),
 * use PROVIDER_MODEL_CATALOG below until capabilities are loaded.
 *
 * KEEP IN SYNC with ui/desktop/frontend/src/data/tts-providers.ts
 */

export type TtsProviderId = "openai" | "elevenlabs" | "edge" | "minimax" | "gemini";

/** Minimal display info only — no runtime data. */
export interface TtsProviderMeta {
  id: TtsProviderId;
  title: string;
  color: string;
  desc: string;
}

export const TTS_PROVIDERS: Record<TtsProviderId, TtsProviderMeta> = {
  elevenlabs: {
    id: "elevenlabs",
    title: "ElevenLabs",
    color: "#000",
    desc: "High-quality neural voices with dynamic library",
  },
  openai: {
    id: "openai",
    title: "OpenAI",
    color: "#10a37f",
    desc: "Reliable TTS via OpenAI API",
  },
  edge: {
    id: "edge",
    title: "Edge (Free)",
    color: "#0078d4",
    desc: "Microsoft Edge TTS — free, no API key required",
  },
  minimax: {
    id: "minimax",
    title: "MiniMax",
    color: "#6200ea",
    desc: "MiniMax TTS with dynamic voice library",
  },
  gemini: {
    id: "gemini",
    title: "Google Gemini",
    color: "#1a73e8",
    desc: "Gemini 3.1 Flash TTS — 70+ languages, multi-speaker, audio tags (preview)",
  },
};

/**
 * Returns provider display meta by id, or null if id is unknown/empty.
 */
export function getProviderDefinition(id: string): TtsProviderMeta | null {
  return TTS_PROVIDERS[id as TtsProviderId] ?? null;
}

/** Minimal model option shape for synchronous catalog fallback. */
export interface TtsModelOption {
  value: string;
  label: string;
  description?: string;
}

/**
 * Synchronous model catalog fallback — used by consumers that cannot await
 * capabilities (e.g. getModelOptions in prompt-settings-section).
 * Source of truth for model lists moves to capabilities endpoint; this is a
 * static snapshot kept in sync with backend allowlists.
 */
export const PROVIDER_MODEL_CATALOG: Record<TtsProviderId, TtsModelOption[]> = {
  elevenlabs: [
    { value: "eleven_v3", label: "Eleven v3" },
    { value: "eleven_flash_v2_5", label: "Eleven Flash v2.5" },
    { value: "eleven_multilingual_v2", label: "Eleven Multilingual v2" },
    { value: "eleven_turbo_v2_5", label: "Eleven Turbo v2.5" },
  ],
  openai: [
    { value: "gpt-4o-mini-tts", label: "GPT-4o Mini TTS" },
    { value: "tts-1", label: "TTS-1" },
    { value: "tts-1-hd", label: "TTS-1 HD" },
  ],
  edge: [],
  minimax: [
    { value: "speech-02-hd", label: "Speech-02 HD" },
    { value: "speech-02-turbo", label: "Speech-02 Turbo" },
    { value: "speech-01-hd", label: "Speech-01 HD" },
    { value: "speech-01-turbo", label: "Speech-01 Turbo" },
  ],
  gemini: [
    { value: "gemini-3.1-flash-tts-preview", label: "Gemini 3.1 Flash TTS (preview)" },
    { value: "gemini-2.5-flash-preview-tts", label: "Gemini 2.5 Flash TTS (preview)" },
    { value: "gemini-2.5-pro-preview-tts", label: "Gemini 2.5 Pro TTS (preview)" },
  ],
};
