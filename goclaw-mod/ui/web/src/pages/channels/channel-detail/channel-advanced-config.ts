import { flattenConfig, unflattenConfig } from "@/lib/config-flatten";
import { normalizeReasoningDeliveryConfig } from "../reasoning-delivery-config";

export const ESSENTIAL_CONFIG_KEYS = new Set(["dm_policy", "group_policy", "require_mention", "mention_mode"]);

function isAdvancedConfigKey(key: string): boolean {
  return !ESSENTIAL_CONFIG_KEYS.has(key) && key !== "groups" && !key.startsWith("groups.");
}

export function deriveAdvancedInitialValues(config: Record<string, unknown> | null | undefined): Record<string, unknown> {
  const normalized = normalizeReasoningDeliveryConfig({ ...((config ?? {}) as Record<string, unknown>) });
  const flat = flattenConfig(normalized);
  // Only keep advanced keys (exclude essential + group overrides).
  return Object.fromEntries(
    Object.entries(flat).filter(([k]) => isAdvancedConfigKey(k)),
  );
}

export function buildAdvancedConfigUpdate(
  existingConfig: Record<string, unknown> | null | undefined,
  values: Record<string, unknown>,
): Record<string, unknown> {
  const normalized = normalizeReasoningDeliveryConfig({ ...((existingConfig ?? {}) as Record<string, unknown>) });
  const flat = flattenConfig(normalized);

  for (const [key, value] of Object.entries(values)) {
    if (!isAdvancedConfigKey(key)) continue;
    if (value === undefined || value === "" || value === null) {
      delete flat[key];
      continue;
    }
    flat[key] = value;
  }

  return normalizeReasoningDeliveryConfig(unflattenConfig(flat));
}
