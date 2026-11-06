import { useCallback, useEffect, useRef, useState } from 'react'
import { usageService } from '../services/usage-service'
import type { ProviderHealth } from '../types/provider'

/** Grace after `cooldown_until` before re-checking, so the gateway has cleared the cooldown. */
const RECHECK_GRACE_MS = 1_500
/** Re-check delay when the cooldown should already be over (clock skew or a renewed cooldown). */
const RECHECK_MIN_MS = 5_000
/** Re-check at least this often while cooling down. */
const RECHECK_MAX_MS = 30_000

/**
 * Milliseconds until the next health re-check while a provider is cooling down:
 * `cooldown_until` + grace, at most 30 s away, and 5 s when that moment has already passed.
 */
export function cooldownRecheckDelay(cooldownUntil: string | null, now: number = Date.now()): number {
  const until = cooldownUntil ? Date.parse(cooldownUntil) : Number.NaN
  if (!Number.isFinite(until)) return RECHECK_MAX_MS
  const delay = until + RECHECK_GRACE_MS - now
  return delay > 0 ? Math.min(delay, RECHECK_MAX_MS) : RECHECK_MIN_MS
}

/**
 * Cooldown / failure state of one provider (GET /v1/providers/{id}/health).
 * A failed refetch keeps the last good value and exposes the error separately,
 * so callers can show stale data instead of dropping the banner.
 * While the provider is cooling down the health is re-fetched when the cooldown
 * should end (and at least every 30 s), so status chips and banners clear on their own.
 */
export function useProviderHealth(providerId: string) {
  const [health, setHealth] = useState<ProviderHealth | null>(null)
  const [error, setError] = useState<unknown>(null)
  const [loading, setLoading] = useState(true)
  // Ignores responses that arrive after the provider changed, a newer request started, or unmount.
  const seq = useRef(0)
  const inFlight = useRef(false)

  const refetch = useCallback(async () => {
    const id = ++seq.current
    inFlight.current = true
    setLoading(true)
    try {
      const res = await usageService.health(providerId)
      if (id !== seq.current) return
      setHealth(res)
      setError(null)
    } catch (err) {
      if (id !== seq.current) return
      console.error('Failed to fetch provider health:', err)
      setError(err)
    } finally {
      if (id === seq.current) {
        inFlight.current = false
        setLoading(false)
      }
    }
  }, [providerId])

  useEffect(() => {
    setHealth(null)
    setError(null)
    void refetch()
    return () => {
      // Drop the pending response of this provider (provider change or unmount).
      seq.current++
      inFlight.current = false
    }
  }, [refetch])

  // Re-check while cooling down. Runs again after every response (health or error changes),
  // so a failed re-check keeps polling and a cleared cooldown stops it.
  const coolingDown = health?.cooling_down === true
  const cooldownUntil = health?.cooldown_until ?? null
  useEffect(() => {
    if (!coolingDown) return
    const timer = setTimeout(() => {
      // A request already running will re-arm this effect when it settles.
      if (!inFlight.current) void refetch()
    // After a failed re-check (gateway unreachable) back off to the 30s cap.
    }, error ? RECHECK_MAX_MS : cooldownRecheckDelay(cooldownUntil))
    return () => clearTimeout(timer)
  }, [coolingDown, cooldownUntil, health, error, refetch])

  return { health, error, loading, refetch }
}
