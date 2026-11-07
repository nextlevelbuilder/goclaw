import { useCallback, useEffect, useRef, useState } from 'react'
import { usageService, type UsageOverview } from '../services/usage-service'
import type { UsageRange } from '../types/provider'

/** Loads gateway-wide usage for a range; refetches when the range changes. */
export function useUsageOverview(range: UsageRange) {
  const [data, setData] = useState<UsageOverview | null>(null)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<Error | null>(null)

  // Only the newest request may write state, so a slow reply for an old range cannot overwrite a newer one.
  const requestId = useRef(0)

  const load = useCallback(async () => {
    const id = ++requestId.current
    const current = () => requestId.current === id
    setLoading(true)
    setError(null)
    try {
      const overview = await usageService.overview(range)
      if (current()) setData(overview)
    } catch (err) {
      if (!current()) return
      setData(null)
      setError(err instanceof Error ? err : new Error(String(err)))
    } finally {
      if (current()) setLoading(false)
    }
  }, [range])

  useEffect(() => {
    void load()
    return () => { requestId.current++ }
  }, [load])

  return { data, loading, error, reload: load }
}
