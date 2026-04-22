import { useState, useEffect, useCallback } from 'react'
import { getWsClient } from '../lib/ws'

interface ConfigResponse {
  config?: { tts?: { provider?: string } }
}

/**
 * Read-only desktop TTS config hook.
 * Calls WS `config.get` (no REST GET /v1/config exists on gateway).
 * Pattern mirrors use-channel-status.ts and use-paired-devices.ts.
 */
export function useDesktopTtsConfig() {
  const [globalProvider, setGlobalProvider] = useState('')
  const [loading, setLoading] = useState(true)

  const fetchConfig = useCallback(async () => {
    try {
      const ws = getWsClient()
      const res = (await ws.call('config.get')) as ConfigResponse
      setGlobalProvider(res.config?.tts?.provider ?? '')
    } catch {
      // Gateway may not be connected yet — treat as unconfigured.
      setGlobalProvider('')
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => {
    fetchConfig()
    // Subscribe to config.updated event if gateway emits it — invalidate cache.
    let unsub: (() => void) | undefined
    try {
      const ws = getWsClient()
      unsub = ws.on('config.updated', () => { void fetchConfig() })
    } catch { /* ws not ready */ }
    return () => { unsub?.() }
  }, [fetchConfig])

  return { globalProvider, loading }
}
