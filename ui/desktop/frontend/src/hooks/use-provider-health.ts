import { useCallback, useEffect, useRef, useState } from 'react'
import { usageService } from '../services/usage-service'
import type { ProviderHealth } from '../types/provider'

/**
 * Cooldown / failure state of one provider (GET /v1/providers/{id}/health).
 * A failed refetch keeps the last good value and exposes the error separately,
 * so callers can show stale data instead of dropping the banner.
 */
export function useProviderHealth(providerId: string) {
  const [health, setHealth] = useState<ProviderHealth | null>(null)
  const [error, setError] = useState<unknown>(null)
  const [loading, setLoading] = useState(true)
  // Ignores responses that arrive after the provider changed or a newer request started.
  const seq = useRef(0)

  const refetch = useCallback(async () => {
    const id = ++seq.current
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
      if (id === seq.current) setLoading(false)
    }
  }, [providerId])

  useEffect(() => {
    setHealth(null)
    setError(null)
    void refetch()
  }, [refetch])

  return { health, error, loading, refetch }
}
