import { useState, useEffect, useCallback } from 'react'
import { getWsClient } from '../lib/ws'
import type { PendingPairing, PairedDevice } from '../types/channel'

export function usePairedDevices() {
  const [pendingPairings, setPendingPairings] = useState<PendingPairing[]>([])
  const [pairedDevices, setPairedDevices] = useState<PairedDevice[]>([])
  const [loading, setLoading] = useState(true)

  const fetchDevices = useCallback(async () => {
    try {
      const ws = getWsClient()
      const res = (await ws.call('device.pair.list')) as { pending: PendingPairing[]; paired: PairedDevice[] }
      setPendingPairings(res.pending ?? [])
      setPairedDevices(res.paired ?? [])
    } catch {
      // ignore — gateway may not be connected
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => {
    fetchDevices()
    let unsub1: (() => void) | undefined
    let unsub2: (() => void) | undefined
    try {
      const ws = getWsClient()
      unsub1 = ws.on('device.pair.requested', () => { fetchDevices() })
      unsub2 = ws.on('device.pair.resolved', () => { fetchDevices() })
    } catch { /* ws not ready */ }
    return () => { unsub1?.(); unsub2?.() }
  }, [fetchDevices])

  const approvePairing = useCallback(async (code: string) => {
    await getWsClient().call('device.pair.approve', { code, approvedBy: 'desktop' })
    fetchDevices()
  }, [fetchDevices])

  const denyPairing = useCallback(async (code: string) => {
    await getWsClient().call('device.pair.deny', { code })
    fetchDevices()
  }, [fetchDevices])

  const revokePairing = useCallback(async (senderId: string, channel: string) => {
    await getWsClient().call('device.pair.revoke', { senderId, channel })
    fetchDevices()
  }, [fetchDevices])

  return { pendingPairings, pairedDevices, loading, refresh: fetchDevices, approvePairing, denyPairing, revokePairing }
}
