import { useState, useEffect, useCallback } from 'react'
import { getApiClient, isApiClientReady } from '../lib/api'
import type { ToolAggregate, RetrievalAggregate, AggregatedMetrics } from '../types/evolution'

export function useEvolutionMetrics(agentId: string, timeRange: '7d' | '30d' | '90d') {
  const [toolAggs, setToolAggs] = useState<ToolAggregate[]>([])
  const [retrievalAggs, setRetrievalAggs] = useState<RetrievalAggregate[]>([])
  const [loading, setLoading] = useState(true)

  const fetchMetrics = useCallback(async () => {
    if (!isApiClientReady()) { setLoading(false); return }
    setLoading(true)
    try {
      const since = new Date(Date.now() - parseDays(timeRange) * 86400000).toISOString()
      const res = await getApiClient().getWithParams<AggregatedMetrics>(
        `/v1/agents/${agentId}/evolution/metrics`,
        { aggregate: 'true', since },
      )
      setToolAggs(res.tool_aggregates ?? [])
      setRetrievalAggs(res.retrieval_aggregates ?? [])
    } catch (err) {
      console.error('Failed to fetch evolution metrics:', err)
    } finally {
      setLoading(false)
    }
  }, [agentId, timeRange])

  useEffect(() => { fetchMetrics() }, [fetchMetrics])

  return { toolAggs, retrievalAggs, loading }
}

const RANGE_DAYS: Record<string, number> = { '7d': 7, '30d': 30, '90d': 90 }

function parseDays(range: string): number {
  return RANGE_DAYS[range] ?? 7
}
