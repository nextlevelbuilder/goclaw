import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { useTraces } from '../../hooks/use-traces'
import { useAgentCrud } from '../../hooks/use-agent-crud'
import { useGatewayStatus } from '../layout/use-gateway-status'
import { useUiStore } from '../../stores/ui-store'
import { startNewChat } from '../layout/shell-actions'
import { ApiError } from '../../lib/api'
import { cn } from '../../lib/cn'
import { IconRefresh, IconTrendingUp } from '../common/Icons'
import {
  Button, DataTable, EmptyState, ErrorState, IconButton, PageHeader, Select, SkeletonList, StatusChip,
  type DataTableColumn,
} from '../ui'
import { TraceDetailDialog } from './TraceDetailDialog'
import { formatDuration, formatRelative, formatTokens, statusLabelKey, statusTone } from './trace-detail-formatters'
import type { TraceData } from '../../types/trace'

export function TraceList() {
  const { t, i18n } = useTranslation(['traces', 'common'])
  const { traces, total, loading, loadingMore, error, fetchTraces, agentFilter, setAgentFilter, loadMore } = useTraces()
  const { agents } = useAgentCrud()
  const { disconnected } = useGatewayStatus()
  const setActiveView = useUiStore((s) => s.setActiveView)
  const [selectedTraceId, setSelectedTraceId] = useState<string | null>(null)
  const [refreshing, setRefreshing] = useState(false)

  const agentOptions = [
    { value: '', label: t('traces:allAgents') },
    ...agents.map((a) => ({ value: a.id, label: a.display_name || a.agent_key })),
  ]
  const agentLabel = (id: string) => {
    const a = agents.find((x) => x.id === id)
    return a ? a.display_name || a.agent_key : ''
  }

  const refresh = async () => {
    setRefreshing(true)
    try { await fetchTraces() } finally { setRefreshing(false) }
  }

  const columns: DataTableColumn<TraceData>[] = [
    {
      id: 'name',
      header: t('traces:columns.name'),
      cell: (trace) => {
        const cacheRead = trace.metadata?.total_cache_read_tokens as number | undefined
        return (
          <div className="min-w-0">
            <div className="flex min-w-0 items-center gap-2">
              <span className="max-w-[280px] truncate text-ui font-medium text-ink">{trace.name || t('traces:unnamed')}</span>
              {trace.parent_trace_id && <span className="shrink-0 text-caption text-ink-muted">{t('traces:delegated')}</span>}
              {trace.channel && <span className="shrink-0 rounded-pill border border-border-subtle px-1.5 text-caption text-ink-secondary">{trace.channel}</span>}
            </div>
            {agentLabel(trace.agent_id) && <p className="truncate text-caption text-ink-muted">{agentLabel(trace.agent_id)}</p>}
            {(cacheRead ?? 0) > 0 && <p className="text-caption text-success">{formatTokens(cacheRead)} {t('traces:cached')}</p>}
          </div>
        )
      },
    },
    {
      id: 'status',
      header: t('traces:columns.status'),
      className: 'w-36',
      cell: (trace) => {
        const key = statusLabelKey(trace.status)
        return <StatusChip tone={statusTone(trace.status)} label={key ? t(`traces:${key}`) : trace.status} />
      },
    },
    {
      id: 'duration',
      header: t('traces:columns.duration'),
      className: 'w-28 whitespace-nowrap',
      cell: (trace) => <span className="text-code text-ink-secondary">{formatDuration(trace.duration_ms, trace.start_time, trace.end_time)}</span>,
    },
    {
      id: 'tokens',
      header: t('traces:columns.tokens'),
      className: 'w-36 whitespace-nowrap',
      cell: (trace) => (
        <span className="text-code text-ink-secondary">
          {formatTokens(trace.total_input_tokens)} / {formatTokens(trace.total_output_tokens)}
        </span>
      ),
    },
    {
      id: 'spans',
      header: t('traces:columns.spans'),
      className: 'w-20',
      cell: (trace) => <span className="text-ink-secondary">{trace.span_count}</span>,
    },
    {
      id: 'time',
      header: t('traces:columns.time'),
      className: 'w-36 whitespace-nowrap',
      cell: (trace) => <span className="text-ink-secondary">{formatRelative(trace.created_at, i18n.language)}</span>,
    },
  ]

  const allLoaded = traces.length >= total
  let body: React.ReactNode
  if (loading) {
    body = <SkeletonList rows={6} avatar={false} label={t('common:state.loading')} className="rounded-lg border border-border-subtle" />
  } else if (error && traces.length === 0) {
    body = (
      <ErrorState
        title={t('traces:states.errorTitle')}
        description={t('traces:states.errorBody')}
        code={error instanceof ApiError ? String(error.status) : undefined}
        onRetry={() => { void refresh() }}
        retrying={refreshing}
      />
    )
  } else if (traces.length === 0) {
    body = (
      <EmptyState
        icon={<IconTrendingUp size={20} />}
        title={agentFilter ? t('traces:noMatchTitle') : t('traces:emptyTitle')}
        description={agentFilter ? t('traces:noMatchDescription') : t('traces:emptyDescription')}
        action={agentFilter ? (
          <Button variant="secondary" onClick={() => setAgentFilter('')}>{t('traces:clearFilter')}</Button>
        ) : (
          <div className="flex items-center gap-2">
            <Button variant="primary" onClick={() => startNewChat()}>{t('traces:startChat')}</Button>
            <Button variant="secondary" onClick={() => setActiveView('agents')}>{t('traces:openAgents')}</Button>
          </div>
        )}
      />
    )
  } else {
    body = (
      <div className="flex flex-col gap-3">
        <DataTable
          aria-label={t('traces:title')}
          columns={columns}
          rows={traces}
          getRowId={(trace) => trace.id}
          onRowClick={(trace) => setSelectedTraceId(trace.id)}
          activeRowId={selectedTraceId}
        />
        {!allLoaded && (
          <div className="flex justify-center">
            <Button variant="secondary" onClick={() => { void loadMore() }} loading={loadingMore} disabled={disconnected}>
              {t('traces:loadMore', { shown: traces.length, total })}
            </Button>
          </div>
        )}
      </div>
    )
  }

  return (
    <div className="flex min-h-0 flex-1 flex-col">
      <PageHeader
        title={t('traces:title')}
        description={total > 0 ? t('traces:summary', { count: total }) : t('traces:description')}
        actions={(
          <>
            <Select
              aria-label={t('traces:filterByAgent')}
              value={agentFilter}
              onChange={(e) => setAgentFilter(e.target.value)}
              options={agentOptions}
              className="w-48"
            />
            <IconButton
              aria-label={t('common:refresh')}
              icon={<IconRefresh size={16} />}
              variant="secondary"
              onClick={() => { void refresh() }}
              loading={refreshing}
            />
          </>
        )}
      />
      <div className={cn('min-h-0 flex-1 overflow-y-auto overscroll-contain px-6 py-4', disconnected && 'opacity-60')}>
        {body}
      </div>

      {selectedTraceId && (
        <TraceDetailDialog traceId={selectedTraceId} onClose={() => setSelectedTraceId(null)} />
      )}
    </div>
  )
}
