import { useState, useEffect, useCallback } from 'react'
import { getApiClient, isApiClientReady } from '../lib/api'
import { toast } from '../stores/toast-store'
import type { TraceData, SpanData } from '../types/trace'

const PAGE_SIZE = 20

export function useTraces() {
  const [traces, setTraces] = useState<TraceData[]>([])
  const [total, setTotal] = useState(0)
  const [loading, setLoading] = useState(true)
  const [agentFilter, setAgentFilter] = useState('')
  const [offset, setOffset] = useState(0)

  const fetchTraces = useCallback(async (reset = true) => {
    if (!isApiClientReady()) { setLoading(false); return }
    const currentOffset = reset ? 0 : offset
    if (reset) setOffset(0)
    try {
      let url = `/v1/traces?limit=${PAGE_SIZE}&offset=${currentOffset}`
      if (agentFilter) url += `&agent_id=${encodeURIComponent(agentFilter)}`
      const res = await getApiClient().get<{ traces: TraceData[] | null; total: number }>(url)
      const fetched = res.traces ?? []
      if (reset) {
        setTraces(fetched)
      } else {
        setTraces((prev) => [...prev, ...fetched])
      }
      setTotal(res.total ?? 0)
    } catch (err) {
      console.error('Failed to fetch traces:', err)
      toast.error('Failed to load traces', (err as Error).message)
    } finally {
      setLoading(false)
    }
  }, [agentFilter, offset])

  useEffect(() => {
    fetchTraces(true)
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [agentFilter])

  const loadMore = useCallback(async () => {
    if (!isApiClientReady()) return
    const newOffset = offset + PAGE_SIZE
    setOffset(newOffset)
    try {
      let url = `/v1/traces?limit=${PAGE_SIZE}&offset=${newOffset}`
      if (agentFilter) url += `&agent_id=${encodeURIComponent(agentFilter)}`
      const res = await getApiClient().get<{ traces: TraceData[] | null; total: number }>(url)
      setTraces((prev) => [...prev, ...(res.traces ?? [])])
      setTotal(res.total ?? 0)
    } catch (err) {
      console.error('Failed to load more traces:', err)
    } finally {
      setLoading(false)
    }
  }, [agentFilter, offset])

  return {
    traces,
    total,
    loading,
    fetchTraces: () => fetchTraces(true),
    agentFilter,
    setAgentFilter,
    offset,
    loadMore,
  }
}

export async function fetchTraceDetail(id: string): Promise<{ trace: TraceData; spans: SpanData[] }> {
  return getApiClient().get<{ trace: TraceData; spans: SpanData[] }>(`/v1/traces/${encodeURIComponent(id)}`)
}
