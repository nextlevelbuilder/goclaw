import { useState, useEffect, useCallback } from 'react'
import { getWsClient } from '../lib/ws'
import { toast } from '../stores/toast-store'
import i18n from '../i18n'
import type { PendingPairing, PairedDevice } from '../types/channel'

export function usePairedDevices() {
  const [pendingPairings, setPendingPairings] = useState<PendingPairing[]>([])
  const [pairedDevices, setPairedDevices] = useState<PairedDevice[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState(false)

  const fetchDevices = useCallback(async () => {
    setError(false)
    try {
      const ws = getWsClient()
      const res = (await ws.call('device.pair.list')) as { pending: PendingPairing[]; paired: PairedDevice[] }
      setPendingPairings(res.pending ?? [])
      setPairedDevices(res.paired ?? [])
    } catch {
      setError(true)
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

  /** Runs a pairing mutation, toasts the outcome and refreshes. Resolves true on success. */
  const mutate = useCallback(async (method: string, params: Record<string, unknown>, okKey: string): Promise<boolean> => {
    try {
      await getWsClient().call(method, params)
      toast.success(i18n.t(`channels:toast.${okKey}`))
      void fetchDevices()
      return true
    } catch (err) {
      toast.error(i18n.t('channels:toast.failedPairing'), (err as Error).message)
      return false
    }
  }, [fetchDevices])

  const approvePairing = useCallback((code: string) => mutate('device.pair.approve', { code, approvedBy: 'desktop' }, 'pairingApproved'), [mutate])
  const denyPairing = useCallback((code: string) => mutate('device.pair.deny', { code }, 'pairingDenied'), [mutate])
  const revokePairing = useCallback((senderId: string, channel: string) => mutate('device.pair.revoke', { senderId, channel }, 'pairingRevoked'), [mutate])

  return { pendingPairings, pairedDevices, loading, error, refresh: fetchDevices, approvePairing, denyPairing, revokePairing }
}
