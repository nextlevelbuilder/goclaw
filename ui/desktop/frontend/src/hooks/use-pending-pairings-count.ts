import { useState, useEffect, useCallback } from 'react'
import { getWsClient } from '../lib/ws'

export function usePendingPairingsCount() {
  const [pendingCount, setPendingCount] = useState(0)

  const fetchCount = useCallback(async () => {
    try {
      const ws = getWsClient()
      const res = (await ws.call('device.pair.list')) as { pending: { code: string }[] }
      setPendingCount(res.pending?.length ?? 0)
    } catch {
      // ignore
    }
  }, [])

  useEffect(() => {
    fetchCount()
    let unsub1: (() => void) | undefined
    let unsub2: (() => void) | undefined
    try {
      const ws = getWsClient()
      unsub1 = ws.on('device.pair.requested', () => { fetchCount() })
      unsub2 = ws.on('device.pair.resolved', () => { fetchCount() })
    } catch { /* ws not ready */ }
    return () => { unsub1?.(); unsub2?.() }
  }, [fetchCount])

  return { pendingCount }
}
