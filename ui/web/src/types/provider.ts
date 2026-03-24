export interface ProviderData {
  id: string;
  name: string;
  display_name: string;
  provider_type: string;
  api_base: string;
  api_key: string; // masked "***" from server
  enabled: boolean;
  settings?: Record<string, unknown>;
  created_at: string;
  updated_at: string;
}

export interface ProviderInput {
  name: string;
  display_name?: string;
  provider_type: string;
  api_base?: string;
  api_key?: string;
  enabled?: boolean;
  settings?: Record<string, unknown>;
}

export interface ModelInfo {
  id: string;
  name?: string;
}

export interface EmbeddingSettings {
  enabled: boolean;
  model?: string;
  api_base?: string;
}

/** Extract embedding settings from provider.settings */
export function getEmbeddingSettings(settings?: Record<string, unknown>): EmbeddingSettings | null {
  if (!settings?.embedding) return null;
  return settings.embedding as EmbeddingSettings;
}
