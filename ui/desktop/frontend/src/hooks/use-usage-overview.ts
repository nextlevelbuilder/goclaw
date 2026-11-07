import { useCallback, useEffect, useState } from 'react'
import { usageService, type UsageOverview } from '../services/usage-service'
import type { UsageRange } from '../types/provider'

/** Loads gateway-wide usage for a range; refetches when the range changes. */
export function useUsageOverview(range: UsageRange) {
  const [data, setData] = useState<UsageOverview | null>(null)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<Error | null>(null)

  const load = useCallback(async () => {
    setLoading(true)
    setError(null)
    try {
      setData(await usageService.overview(range))
    } catch (err) {
      setData(null)
      setError(err instanceof Error ? err : new Error(String(err)))
    } finally {
      setLoading(false)
    }
  }, [range])

  useEffect(() => { void load() }, [load])

  return { data, loading, error, reload: load }
}
