import { uniqueId } from "@/lib/utils";
import { getChatGPTOAuthPoolOwnership } from "@/pages/providers/provider-utils";
import type { ProviderData } from "@/types/provider";
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

/**
 * Parse stored settings into ProviderEntry list, filtering out unavailable providers.
 *
 * Also migrates legacy entries that point directly at a Codex pool *member*
 * (which the UI no longer lets users pick — the dropdown only shows owners).
 * Such an entry is silently rewritten to its pool owner so the dropdown
 * displays a matching option and runtime routing actually goes through the
 * pool's strategy instead of a bare solo call to the member.
 */
export function parseInitialEntries(
  settings: Record<string, unknown>,
  providers: ProviderData[],
): ProviderEntry[] {
  const ownership = getChatGPTOAuthPoolOwnership(providers);
  const providersByName = new Map(providers.map((p) => [p.name, p]));

  const resolveToOwner = (name: string, providerId: string): { name: string; id: string } => {
    const ownerName = ownership.ownerByMember.get(name);
    if (!ownerName) return { name, id: providerId };
    const owner = providersByName.get(ownerName);
    if (!owner) return { name, id: providerId };
    return { name: ownerName, id: owner.id };
  };

  // New format: { providers: [...] }
  if (Array.isArray(settings.providers)) {
    return (settings.providers as Record<string, unknown>[])
      .map((p) => {
        const rawName = String(p.provider ?? "");
        const rawPid = String(p.provider_id ?? "");
        const initialPid = (rawPid && providers.some((pr) => pr.id === rawPid))
          ? rawPid
          : providers.find((pr) => pr.name === rawName)?.id ?? "";
        if (!initialPid) return null;
        const migrated = resolveToOwner(rawName, initialPid);
        return {
          id: uniqueId(),
          provider_id: migrated.id,
          provider: migrated.name,
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
      const migrated = resolveToOwner(providerName, providerData.id);
      return [
        {
          id: uniqueId(),
          provider_id: migrated.id,
          provider: migrated.name,
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
