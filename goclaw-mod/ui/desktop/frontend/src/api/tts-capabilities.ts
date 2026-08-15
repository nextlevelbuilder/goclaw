// Mirror of ui/web/src/api/tts-capabilities.ts for the desktop (Lite) frontend.
// No React Query — desktop fetches via ApiClient directly.

// ---- Types ----

export interface EnumOption {
  value: string;
  label: string;
}

export interface Dependency {
  field: string;
  op: string;
  value: unknown;
}

export type ParamType =
  | "range"
  | "number"
  | "integer"
  | "enum"
  | "boolean"
  | "string"
  | "text";

export interface ParamSchema {
  key: string;
  type: ParamType;
  label: string;
  description?: string;
  default?: unknown;
  min?: number;
  max?: number;
  step?: number;
  enum?: EnumOption[];
  depends_on?: Dependency[];
  /** "advanced" → Advanced section; absent/undefined → Basic section (forward-compat: unknown values → Basic). */
  group?: string;
  /**
   * When non-empty, this param is agent-overridable and the value is the
   * generic key alias stored in agents.other_config.tts_params (e.g. "speed",
   * "emotion", "style"). Empty / absent = not overridable.
   * Single source of truth for generic↔native key mapping (Finding #9).
   */
  agent_overridable_as?: string;
}

export interface VoiceOption {
  voice_id: string;
  name: string;
  language?: string;
  gender?: string;
  /** Provider-specific descriptors (e.g. {style: "Bright"} for Gemini). */
  labels?: Record<string, string>;
}

export interface ProviderCapabilities {
  provider: string;
  display_name: string;
  requires_api_key: boolean;
  models?: string[];
  voices?: VoiceOption[];
  params?: ParamSchema[] | null;
  custom_features?: Record<string, unknown> | null;
}

export interface CapabilitiesResponse {
  providers: ProviderCapabilities[];
}

// ---- parseCapabilitiesResponse ----

/**
 * Parses and validates a raw GET /v1/tts/capabilities JSON response.
 * Throws if the shape is invalid.
 */
export function parseCapabilitiesResponse(raw: unknown): ProviderCapabilities[] {
  if (raw === null || raw === undefined || typeof raw !== "object") {
    throw new Error("capabilities response must be an object");
  }
  const obj = raw as Record<string, unknown>;
  if (!("providers" in obj)) {
    throw new Error("capabilities response missing 'providers' field");
  }
  if (!Array.isArray(obj.providers)) {
    throw new Error("capabilities response 'providers' must be an array");
  }
  return obj.providers as ProviderCapabilities[];
}
