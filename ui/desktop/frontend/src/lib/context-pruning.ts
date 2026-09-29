import type { ContextPruningConfig } from '../types/agent'

// The backend treats an unset context_pruning as enabled (cache-ttl); only mode "off" disables it.
export function isContextPruningEnabled(config: ContextPruningConfig | null | undefined): boolean {
  return config?.mode !== 'off'
}

// An unset config inherits the global default, so it stays unset unless the user changed it.
export function buildContextPruningPayload(
  current: ContextPruningConfig | null | undefined,
  enabled: boolean,
  config: ContextPruningConfig,
): ContextPruningConfig | undefined {
  if (current == null && enabled && Object.keys(config).length === 0) return undefined
  return enabled ? { ...config, mode: 'cache-ttl' } : { mode: 'off' }
}
