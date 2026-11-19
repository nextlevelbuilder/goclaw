import { normalizeReasoningDeliveryConfig } from './reasoning-delivery-config'

export const ESSENTIAL_KEYS = new Set(['dm_policy', 'group_policy', 'require_mention', 'mention_mode'])

function isAdvancedConfigKey(key: string): boolean {
  return !ESSENTIAL_KEYS.has(key) && key !== 'groups'
}

export function deriveAdvancedInitialValues(config: Record<string, unknown> | null | undefined): Record<string, unknown> {
  const normalized = normalizeReasoningDeliveryConfig({ ...(config ?? {}) })
  return Object.fromEntries(
    Object.entries(normalized).filter(([k]) => isAdvancedConfigKey(k)),
  )
}

// Fields emit undefined/'' when cleared. Drop those keys from the saved config
// instead of skipping them, otherwise the spread keeps the stale value.
export function buildAdvancedConfigUpdate(
  existingConfig: Record<string, unknown> | null | undefined,
  values: Record<string, unknown>,
): Record<string, unknown> {
  const next: Record<string, unknown> = { ...(existingConfig ?? {}) }

  for (const [key, value] of Object.entries(values)) {
    if (!isAdvancedConfigKey(key)) continue
    if (value === undefined || value === '' || value === null) {
      delete next[key]
      continue
    }
    next[key] = value
  }

  return normalizeReasoningDeliveryConfig(next)
}
