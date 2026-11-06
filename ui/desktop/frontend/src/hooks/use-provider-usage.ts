import { useCallback, useEffect, useReducer, useRef } from 'react'
import { usageService } from '../services/usage-service'
import type { ProviderUsageBundle, UsageRange } from '../types/provider'

/** Cached data younger than this is reused as-is when switching back to a range. */
const FRESH_MS = 60_000

interface UsageEntry {
  data?: ProviderUsageBundle
  fetchedAt?: number
  error?: unknown
  loading?: boolean
}

export interface ProviderUsageState {
  data: ProviderUsageBundle | null
  /** Epoch ms of the shown data. */
  fetchedAt: number | null
  /** True only while nothing is cached for this range yet. */
  loading: boolean
  /** Background refresh in progress while cached data is shown. */
  refreshing: boolean
  /** Load failed and there is no data to show. */
  error: unknown
  /** Refresh failed; `data` is the last good (stale) value. */
  staleError: unknown
  refetch: () => void
}

/**
 * Usage tab data for one provider, cached per range for the lifetime of the hook.
 * `providerName` filters the snapshot endpoints; `providerId` addresses usage-detail.
 * Nothing is fetched while `enabled` is false.
 */
export function useProviderUsage(
  providerId: string,
  providerName: string,
  range: UsageRange,
  enabled = true,
): ProviderUsageState {
  const cache = useRef(new Map<string, UsageEntry>())
  const [, rerender] = useReducer((n: number) => n + 1, 0)
  const keyOf = useCallback((r: UsageRange) => `${providerId}\u0000${providerName}\u0000${r}`, [providerId, providerName])

  const patch = useCallback((key: string, next: UsageEntry) => {
    cache.current.set(key, { ...cache.current.get(key), ...next })
    rerender()
  }, [])

  const load = useCallback(async (r: UsageRange) => {
    const key = keyOf(r)
    if (cache.current.get(key)?.loading) return
    patch(key, { loading: true })
    try {
      const data = await usageService.bundle(providerId, providerName, r)
      patch(key, { data, fetchedAt: Date.now(), error: undefined, loading: false })
    } catch (err) {
      console.error('Failed to fetch provider usage:', err)
      patch(key, { error: err, loading: false })
    }
  }, [keyOf, patch, providerId, providerName])

  useEffect(() => {
    if (!enabled) return
    const entry = cache.current.get(keyOf(range))
    if (entry?.loading) return
    if (entry?.data && entry.fetchedAt !== undefined && Date.now() - entry.fetchedAt < FRESH_MS) return
    void load(range)
  }, [enabled, keyOf, load, range])

  const refetch = useCallback(() => { void load(range) }, [load, range])

  const entry = cache.current.get(keyOf(range))
  const data = entry?.data ?? null
  return {
    data,
    fetchedAt: entry?.fetchedAt ?? null,
    loading: !data && (entry === undefined || !!entry.loading),
    refreshing: !!data && !!entry?.loading,
    error: data ? null : entry?.error ?? null,
    staleError: data ? entry?.error ?? null : null,
    refetch,
  }
}
