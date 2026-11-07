import { useState, useEffect, useCallback, useRef } from 'react'
import i18n from '../i18n'
import { getApiClient, isApiClientReady } from '../lib/api'
import { toast } from '../stores/toast-store'
import type { TraceData, SpanData } from '../types/trace'

const PAGE_SIZE = 20

interface TracePage {
  traces: TraceData[] | null
  total: number
}

function pageUrl(offset: number, agentId: string): string {
  let url = `/v1/traces?limit=${PAGE_SIZE}&offset=${offset}`
  if (agentId) url += `&agent_id=${encodeURIComponent(agentId)}`
  return url
}

export function useTraces() {
  const [traces, setTraces] = useState<TraceData[]>([])
  const [total, setTotal] = useState(0)
  const [loading, setLoading] = useState(true)
  const [loadingMore, setLoadingMore] = useState(false)
  const [error, setError] = useState<Error | null>(null)
  const [agentFilter, setAgentFilter] = useState('')
  const offsetRef = useRef(0)

  const fetchTraces = useCallback(async () => {
    if (!isApiClientReady()) { setLoading(false); return }
    offsetRef.current = 0
    try {
      const res = await getApiClient().get<TracePage>(pageUrl(0, agentFilter))
      setTraces(res.traces ?? [])
      setTotal(res.total ?? 0)
      setError(null)
    } catch (err) {
      console.error('Failed to fetch traces:', err)
      setError(err instanceof Error ? err : new Error(String(err)))
    } finally {
      setLoading(false)
    }
  }, [agentFilter])

  useEffect(() => {
    setLoading(true)
    void fetchTraces()
  }, [fetchTraces])

  const loadMore = useCallback(async () => {
    if (!isApiClientReady()) return
    const nextOffset = offsetRef.current + PAGE_SIZE
    setLoadingMore(true)
    try {
      const res = await getApiClient().get<TracePage>(pageUrl(nextOffset, agentFilter))
      offsetRef.current = nextOffset
      setTraces((prev) => [...prev, ...(res.traces ?? [])])
      setTotal(res.total ?? 0)
    } catch (err) {
      console.error('Failed to load more traces:', err)
      toast.error(i18n.t('traces:toast.failedLoad'), (err as Error).message)
    } finally {
      setLoadingMore(false)
    }
  }, [agentFilter])

  return {
    traces,
    total,
    loading,
    loadingMore,
    error,
    fetchTraces,
    agentFilter,
    setAgentFilter,
    loadMore,
  }
}

export async function fetchTraceDetail(id: string): Promise<{ trace: TraceData; spans: SpanData[] }> {
  return getApiClient().get<{ trace: TraceData; spans: SpanData[] }>(`/v1/traces/${encodeURIComponent(id)}`)
}
