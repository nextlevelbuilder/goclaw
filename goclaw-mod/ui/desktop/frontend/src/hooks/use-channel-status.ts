import { useState, useEffect, useCallback } from 'react'
import { getWsClient } from '../lib/ws'
import type { ChannelStatus } from '../types/channel'

export function useChannelStatus() {
  const [statusMap, setStatusMap] = useState<Record<string, ChannelStatus>>({})
  const [loading, setLoading] = useState(true)

  const fetchStatus = useCallback(async () => {
    try {
      const ws = getWsClient()
      const res = (await ws.call('channels.status')) as { channels: Record<string, ChannelStatus> }
      setStatusMap(res.channels ?? {})
    } catch {
      // gateway may not be ready yet
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => { fetchStatus() }, [fetchStatus])

  return { statusMap, loading, refreshStatus: fetchStatus }
}
