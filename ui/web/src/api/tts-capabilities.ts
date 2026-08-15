import { useQuery } from "@tanstack/react-query";
import { useHttp } from "@/hooks/use-ws";

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
   * Used as the single source of truth for the generic↔native key mapping in
   * the UI — no separate hard-coded lookup table needed (Finding #9).
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

interface CapabilitiesResponse {
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

// ---- buildCapabilitiesQueryOptions ----

type CapabilitiesFetcher = () => Promise<CapabilitiesResponse>;

/**
 * Builds React Query options for the TTS capabilities query.
 * The queryFn catches errors and returns an empty providers array as fallback.
 */
export function buildCapabilitiesQueryOptions(fetcher: CapabilitiesFetcher) {
  return {
    queryKey: ["tts", "capabilities"] as const,
    staleTime: 5 * 60_000, // 5 minutes — catalog data changes rarely
    queryFn: async (_ctx: { signal: AbortSignal }): Promise<ProviderCapabilities[]> => {
      try {
        const resp = await fetcher();
        return parseCapabilitiesResponse(resp);
      } catch {
        return [];
      }
    },
  };
}

// ---- useTtsCapabilities hook ----

export const ttsCapabilitiesKeys = {
  all: ["tts", "capabilities"] as const,
};

/** React Query hook for fetching TTS provider capabilities. */
export function useTtsCapabilities() {
  const http = useHttp();
  return useQuery({
    queryKey: ttsCapabilitiesKeys.all,
    queryFn: async (): Promise<ProviderCapabilities[]> => {
      try {
        const resp = await http.get<CapabilitiesResponse>("/v1/tts/capabilities");
        return parseCapabilitiesResponse(resp);
      } catch {
        return [];
      }
    },
    staleTime: 5 * 60_000,
    retry: 1,
  });
}
