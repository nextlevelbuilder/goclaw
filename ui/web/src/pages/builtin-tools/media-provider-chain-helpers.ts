import { uniqueId } from "@/lib/utils";
import { MEDIA_PARAMS_SCHEMA } from "./media-provider-params-schema";
export interface ProviderEntry {
  id: string;
  provider_id: string;
  provider: string;
  model: string;
  enabled: boolean;
  timeout: number;
  max_retries: number;
  params: Record<string, unknown>;
}

/** Convert tool_name to display title (e.g. "text_to_speech" → "Text To Speech"). */
export function formatToolTitle(name: string): string {
  return name
    .replace(/_/g, " ")
    .replace(/\b\w/g, (c) => c.toUpperCase());
}

/** Build default param values for a given tool + provider type from schema. */
export function buildDefaultParams(toolName: string, providerType: string): Record<string, unknown> {
  const schema = MEDIA_PARAMS_SCHEMA[toolName]?.[providerType] ?? [];
  const defaults: Record<string, unknown> = {};
  for (const field of schema) {
    if (field.default !== undefined) {
      defaults[field.key] = field.default;
    }
  }
  return defaults;
}

/** Parse stored settings into ProviderEntry list, filtering out unavailable providers. */
export function parseInitialEntries(
  settings: Record<string, unknown>,
  providers: Array<{ id: string; name: string }>,
): ProviderEntry[] {
  // New format: { providers: [...] }
  if (Array.isArray(settings.providers)) {
    return (settings.providers as Record<string, unknown>[])
      .map((p) => {
        const name = String(p.provider ?? "");
        const pid = String(p.provider_id ?? "");
        const resolved = (pid && providers.some((pr) => pr.id === pid))
          ? pid
          : providers.find((pr) => pr.name === name)?.id ?? "";
        if (!resolved) return null;
        return {
          id: uniqueId(),
          provider_id: resolved,
          provider: name,
          model: String(p.model ?? ""),
          enabled: Boolean(p.enabled ?? true),
          timeout: Number(p.timeout ?? 120),
          max_retries: Number(p.max_retries ?? 2),
          params: (p.params as Record<string, unknown>) ?? {},
        };
      })
      .filter((e): e is ProviderEntry => e !== null);
  }

  // Legacy format: { provider, model }
  if (settings.provider || settings.model) {
    const providerName = String(settings.provider ?? "");
    const providerData = providers.find((p) => p.name === providerName);
    if (providerData) {
      return [
        {
          id: uniqueId(),
          provider_id: providerData.id,
          provider: providerName,
          model: String(settings.model ?? ""),
          enabled: true,
          timeout: 120,
          max_retries: 2,
          params: {},
        },
      ];
    }
  }

  return [];
}
