/**
 * Desktop hook for fetching TTS provider capabilities.
 * Mirrors useTtsCapabilities from ui/web/src/api/tts-capabilities.ts
 * but uses the desktop ApiClient pattern (no React Query).
 */
import { useState, useEffect, useCallback } from 'react'
import { getApiClient } from '../lib/api'
import { parseCapabilitiesResponse } from '../api/tts-capabilities'
import type { ProviderCapabilities } from '../api/tts-capabilities'

interface UseTtsCapabilitiesResult {
  data: ProviderCapabilities[]
  isLoading: boolean
  error: string | null
}

export function useTtsCapabilities(): UseTtsCapabilitiesResult {
  const [data, setData] = useState<ProviderCapabilities[]>([])
  const [isLoading, setIsLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)

  const fetch = useCallback(async () => {
    setIsLoading(true)
    setError(null)
    try {
      const res = await getApiClient().get<unknown>('/v1/tts/capabilities')
      setData(parseCapabilitiesResponse(res))
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to load capabilities')
      setData([])
    } finally {
      setIsLoading(false)
    }
  }, [])

  useEffect(() => { void fetch() }, [fetch])

  return { data, isLoading, error }
}
