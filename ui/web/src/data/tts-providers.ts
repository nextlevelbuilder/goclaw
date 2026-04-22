/**
 * TTS provider catalog — source of truth for non-ElevenLabs providers.
 *
 * ElevenLabs voices are fetched dynamically via /v1/voices (dynamic: true).
 * OpenAI, Edge, and MiniMax voices are hardcoded here because their backends
 * accept any string and fall back to provider defaults.
 *
 * Backend provider name refs:
 *   openai    → internal/audio/openai/tts.go:59
 *   elevenlabs → internal/audio/elevenlabs/tts.go Name()
 *   edge      → internal/audio/edge/tts.go:47
 *   minimax   → internal/audio/minimax/tts.go:64
 *
 * KEEP IN SYNC with ui/desktop/frontend/src/data/tts-providers.ts
 */

export type TtsProviderId = "openai" | "elevenlabs" | "edge" | "minimax";

export interface TtsVoiceOption {
  value: string;
  label: string;
  locale?: string;
}

export interface TtsModelOption {
  value: string;
  label: string;
  description?: string;
}

export interface TtsProviderDefinition {
  id: TtsProviderId;
  /** true = voices fetched from /v1/voices; false = use voices[] below */
  dynamic: boolean;
  voices: TtsVoiceOption[];
  models: TtsModelOption[];
  defaultVoice?: string;
  defaultModel?: string;
  requiresApiKey: boolean;
}

export const TTS_PROVIDERS: Record<TtsProviderId, TtsProviderDefinition> = {
  // ElevenLabs — voices fetched from server; models hardcoded from allowlist
  // Ref: internal/audio/elevenlabs/models.go:14-19
  // Defaults: internal/config/config_channels.go:499-500
  elevenlabs: {
    id: "elevenlabs",
    dynamic: true,
    voices: [],
    models: [
      { value: "eleven_v3", label: "Eleven v3" },
      { value: "eleven_flash_v2_5", label: "Eleven Flash v2.5" },
      { value: "eleven_multilingual_v2", label: "Eleven Multilingual v2" },
      { value: "eleven_turbo_v2_5", label: "Eleven Turbo v2.5" },
    ],
    defaultVoice: "pMsXgVXv3BLzUgSXRplE",
    defaultModel: "eleven_multilingual_v2",
    requiresApiKey: true,
  },

  // OpenAI TTS — docs: https://platform.openai.com/docs/guides/text-to-speech
  // Ref: internal/audio/openai/tts.go:46-51
  openai: {
    id: "openai",
    dynamic: false,
    voices: [
      { value: "alloy", label: "Alloy" },
      { value: "echo", label: "Echo" },
      { value: "fable", label: "Fable" },
      { value: "onyx", label: "Onyx" },
      { value: "nova", label: "Nova" },
      { value: "shimmer", label: "Shimmer" },
      { value: "coral", label: "Coral" },
      { value: "sage", label: "Sage" },
    ],
    models: [
      { value: "gpt-4o-mini-tts", label: "GPT-4o Mini TTS" },
      { value: "tts-1", label: "TTS-1" },
      { value: "tts-1-hd", label: "TTS-1 HD" },
    ],
    defaultVoice: "alloy",
    defaultModel: "gpt-4o-mini-tts",
    requiresApiKey: true,
  },

  // Microsoft Edge TTS — free, no API key required
  // Ref: internal/audio/edge/tts.go:37-39
  // Top-20 neural voices from edge-tts registry
  edge: {
    id: "edge",
    dynamic: false,
    voices: [
      { value: "en-US-MichelleNeural", label: "Michelle (en-US)", locale: "en-US" },
      { value: "en-US-AriaNeural", label: "Aria (en-US)", locale: "en-US" },
      { value: "en-US-GuyNeural", label: "Guy (en-US)", locale: "en-US" },
      { value: "en-GB-SoniaNeural", label: "Sonia (en-GB)", locale: "en-GB" },
      { value: "en-GB-RyanNeural", label: "Ryan (en-GB)", locale: "en-GB" },
      { value: "en-AU-NatashaNeural", label: "Natasha (en-AU)", locale: "en-AU" },
      { value: "vi-VN-HoaiMyNeural", label: "Hoài My (vi-VN)", locale: "vi-VN" },
      { value: "vi-VN-NamMinhNeural", label: "Nam Minh (vi-VN)", locale: "vi-VN" },
      { value: "zh-CN-XiaoxiaoNeural", label: "Xiaoxiao (zh-CN)", locale: "zh-CN" },
      { value: "zh-CN-YunxiNeural", label: "Yunxi (zh-CN)", locale: "zh-CN" },
      { value: "ja-JP-NanamiNeural", label: "Nanami (ja-JP)", locale: "ja-JP" },
      { value: "ko-KR-SunHiNeural", label: "Sun Hi (ko-KR)", locale: "ko-KR" },
      { value: "es-ES-ElviraNeural", label: "Elvira (es-ES)", locale: "es-ES" },
      { value: "fr-FR-DeniseNeural", label: "Denise (fr-FR)", locale: "fr-FR" },
      { value: "de-DE-KatjaNeural", label: "Katja (de-DE)", locale: "de-DE" },
      { value: "it-IT-ElsaNeural", label: "Elsa (it-IT)", locale: "it-IT" },
      { value: "pt-BR-FranciscaNeural", label: "Francisca (pt-BR)", locale: "pt-BR" },
      { value: "ru-RU-SvetlanaNeural", label: "Svetlana (ru-RU)", locale: "ru-RU" },
      { value: "hi-IN-SwaraNeural", label: "Swara (hi-IN)", locale: "hi-IN" },
      { value: "id-ID-GadisNeural", label: "Gadis (id-ID)", locale: "id-ID" },
    ],
    models: [],
    defaultVoice: "en-US-MichelleNeural",
    requiresApiKey: false,
  },

  // MiniMax TTS — docs: https://platform.minimax.io/docs/api-reference/speech-t2a-intro
  // Ref: internal/audio/minimax/tts.go:50-56
  minimax: {
    id: "minimax",
    dynamic: false,
    voices: [
      { value: "Wise_Woman", label: "Wise Woman" },
      { value: "Friendly_Person", label: "Friendly Person" },
      { value: "Inspirational_girl", label: "Inspirational Girl" },
      { value: "Deep_Voice_Man", label: "Deep Voice Man" },
      { value: "Calm_Woman", label: "Calm Woman" },
      { value: "Casual_Guy", label: "Casual Guy" },
      { value: "Lively_Girl", label: "Lively Girl" },
      { value: "Patient_Man", label: "Patient Man" },
      { value: "Young_Knight", label: "Young Knight" },
      { value: "Determined_Man", label: "Determined Man" },
      { value: "Lovely_Girl", label: "Lovely Girl" },
      { value: "Decent_Boy", label: "Decent Boy" },
      { value: "Imposing_Manner", label: "Imposing Manner" },
      { value: "Elegant_Man", label: "Elegant Man" },
      { value: "Abbess", label: "Abbess" },
      { value: "Sweet_Girl_2", label: "Sweet Girl 2" },
      { value: "Exuberant_Girl", label: "Exuberant Girl" },
    ],
    models: [
      { value: "speech-02-hd", label: "Speech-02 HD" },
      { value: "speech-02-turbo", label: "Speech-02 Turbo" },
      { value: "speech-01-hd", label: "Speech-01 HD" },
      { value: "speech-01-turbo", label: "Speech-01 Turbo" },
    ],
    defaultVoice: "Wise_Woman",
    defaultModel: "speech-02-hd",
    requiresApiKey: true,
  },
};

/**
 * Returns provider definition by id, or null if id is unknown/empty.
 */
export function getProviderDefinition(id: string): TtsProviderDefinition | null {
  return TTS_PROVIDERS[id as TtsProviderId] ?? null;
}
