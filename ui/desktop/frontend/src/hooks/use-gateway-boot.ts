import { useCallback, useEffect, useRef, useState } from 'react'
import { wails } from '../lib/wails'
import { initWsClient } from '../lib/ws'
import { initApiClient } from '../lib/api'
import { useUiStore } from '../stores/ui-store'

export type BootPhase = 'gateway' | 'connect' | 'setup'

export type BootState =
  | { status: 'starting'; phase: BootPhase }
  | { status: 'ready' }
  | { status: 'failed'; reason: 'timeout' | 'connect'; detail?: string }

export const GATEWAY_POLL_MS = 500
export const GATEWAY_MAX_ATTEMPTS = 30
/** Seconds the gateway is given before the start counts as failed. */
export const GATEWAY_TIMEOUT_SECS = (GATEWAY_POLL_MS * GATEWAY_MAX_ATTEMPTS) / 1000

const sleep = (ms: number) => new Promise((r) => setTimeout(r, ms))

/**
 * Waits for the embedded gateway, then connects the WebSocket and API clients and
 * checks whether the database is empty (which resets the onboarding flag).
 * `retry` runs the whole sequence again after a failure.
 */
export function useGatewayBoot(): { state: BootState; retry: () => void } {
  const [state, setState] = useState<BootState>({ status: 'starting', phase: 'gateway' })
  const [run, setRun] = useState(0)
  const active = useRef(0)

  useEffect(() => {
    const id = ++active.current
    const current = () => active.current === id
    const go = (next: BootState) => { if (current()) setState(next) }

    const boot = async () => {
      go({ status: 'starting', phase: 'gateway' })
      let ready = false
      for (let attempts = 0; attempts < GATEWAY_MAX_ATTEMPTS && current(); attempts++) {
        try {
          if (await wails.isGatewayReady()) { ready = true; break }
        } catch { /* not ready yet */ }
        await sleep(GATEWAY_POLL_MS)
      }
      if (!current()) return
      if (!ready) {
        go({ status: 'failed', reason: 'timeout' })
        return
      }

      go({ status: 'starting', phase: 'connect' })
      let api
      try {
        let token = ''
        try { token = await wails.getGatewayToken() } catch (e) {
          console.warn('[app] failed to get token:', e)
        }
        const gatewayUrl = await wails.getGatewayURL()
        initWsClient(gatewayUrl.replace(/^http/, 'ws') + '/ws', token)
        api = initApiClient(gatewayUrl, token)
      } catch (e) {
        go({ status: 'failed', reason: 'connect', detail: e instanceof Error ? e.message : undefined })
        return
      }

      go({ status: 'starting', phase: 'setup' })
      // Auto-detect empty DB → reset onboarded flag (handles DB deletion)
      try {
        const [pRes, aRes] = await Promise.allSettled([
          api.get<{ providers?: unknown[] | null }>('/v1/providers'),
          api.get<{ agents?: unknown[] | null }>('/v1/agents'),
        ])
        const hasProviders = pRes.status === 'fulfilled' && (pRes.value.providers?.length ?? 0) > 0
        const hasAgents = aRes.status === 'fulfilled' && (aRes.value.agents?.length ?? 0) > 0
        if (!hasProviders && !hasAgents && current()) {
          useUiStore.getState().resetOnboarding()
        }
      } catch { /* ignore — onboarding wizard will handle */ }
      go({ status: 'ready' })
    }

    void boot()
    return () => { active.current++ }
  }, [run])

  const retry = useCallback(() => setRun((n) => n + 1), [])
  return { state, retry }
}
