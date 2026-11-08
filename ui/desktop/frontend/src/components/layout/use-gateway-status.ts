import { useCallback, useEffect, useState } from 'react'
import { getWsClient, type WsConnectionState, type WsConnectionStatus } from '../../lib/ws'

export interface GatewayStatus {
  /** connecting: first connect or a retry in flight; online: session ready; offline: waiting to retry. */
  status: WsConnectionStatus
  online: boolean
  /**
   * The connection was lost and is not back yet: true while offline and during
   * the reconnect attempts that follow, false on first connect. Drives the
   * offline banner and dimmed saved data.
   */
  disconnected: boolean
  /** Whole seconds until the next automatic retry while offline, else null. */
  retryInSeconds: number | null
  /** Reconnect now instead of waiting for the backoff timer. */
  retryNow: () => void
}

const CONNECTING: WsConnectionState = { status: 'connecting', nextRetryAt: null, lost: false }

function readState(): WsConnectionState {
  try {
    return getWsClient().connectionState
  } catch {
    return CONNECTING // client not initialised yet (startup, tests)
  }
}

/**
 * Live gateway connection state from the WebSocket client, for the offline
 * banner, the sidebar footer and screens that dim saved data while offline.
 */
export function useGatewayStatus(): GatewayStatus {
  const [state, setState] = useState<WsConnectionState>(readState)
  const [now, setNow] = useState(() => Date.now())

  useEffect(() => {
    let unsubscribe = () => {}
    try {
      const client = getWsClient()
      setState(client.connectionState)
      unsubscribe = client.subscribeConnection(setState)
    } catch {
      /* client not initialised; stay "connecting" */
    }
    return unsubscribe
  }, [])

  // Tick once a second while a retry is scheduled so the countdown stays current.
  useEffect(() => {
    if (state.status !== 'offline' || state.nextRetryAt === null) return
    setNow(Date.now())
    const id = setInterval(() => setNow(Date.now()), 1000)
    return () => clearInterval(id)
  }, [state])

  const retryNow = useCallback(() => {
    try {
      getWsClient().retryNow()
    } catch {
      /* client not initialised */
    }
  }, [])

  const retryInSeconds = state.status === 'offline' && state.nextRetryAt !== null
    ? Math.max(0, Math.ceil((state.nextRetryAt - now) / 1000))
    : null

  const online = state.status === 'online'
  return { status: state.status, online, disconnected: state.lost && !online, retryInSeconds, retryNow }
}
