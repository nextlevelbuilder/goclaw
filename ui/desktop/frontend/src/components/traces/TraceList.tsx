import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { useTraces } from '../../hooks/use-traces'
import { useAgentCrud } from '../../hooks/use-agent-crud'
import { RefreshButton } from '../common/RefreshButton'
import { Combobox } from '../common/Combobox'
import { TraceDetailDialog } from './TraceDetailDialog'
import { TraceListRow } from './trace-list-row'

export function TraceList() {
  const { t } = useTranslation('traces')
  const { traces, total, loading, fetchTraces, agentFilter, setAgentFilter, loadMore } = useTraces()
  const { agents } = useAgentCrud()
  const [selectedTraceId, setSelectedTraceId] = useState<string | null>(null)

  const agentOptions = [
    { value: '', label: t('allAgents') },
    ...agents.map((a) => ({ value: a.id, label: a.display_name || a.agent_key })),
  ]

  const allLoaded = traces.length >= total && total > 0

  return (
    <div className="space-y-4">
      {/* Header */}
      <div className="flex items-center justify-between gap-3">
        <div>
          <h2 className="text-sm font-semibold text-text-primary">{t('title')}</h2>
          <p className="text-xs text-text-muted mt-0.5">{t('description')}</p>
        </div>
        <div className="flex items-center gap-2">
          <div className="w-44">
            <Combobox
              value={agentFilter}
              onChange={setAgentFilter}
              options={agentOptions}
              placeholder={t('allAgents')}
              allowCustom={false}
            />
          </div>
          <RefreshButton onRefresh={fetchTraces} />
        </div>
      </div>

      {/* Loading */}
      {loading && traces.length === 0 ? (
        <div className="space-y-2">
          {[1, 2, 3].map((i) => (
            <div key={i} className="h-12 rounded-lg bg-surface-tertiary/50 animate-pulse" />
          ))}
        </div>
      ) : traces.length === 0 ? (
        <div className="flex flex-col items-center gap-2 py-12">
          <svg className="h-10 w-10 text-text-muted/40" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth={1.5} strokeLinecap="round" strokeLinejoin="round">
            <path d="M22 12h-2.48a2 2 0 0 0-1.93 1.46l-2.35 8.36a.25.25 0 0 1-.48 0L9.24 2.18a.25.25 0 0 0-.48 0l-2.35 8.36A2 2 0 0 1 4.49 12H2" />
          </svg>
          <p className="text-sm text-text-muted">{t('emptyTitle')}</p>
          <p className="text-xs text-text-muted/70">{t('emptyDescription')}</p>
        </div>
      ) : (
        <>
          <div className="overflow-x-auto rounded-lg border border-border">
            <table className="w-full text-sm min-w-[600px]">
              <thead>
                <tr className="border-b border-border bg-surface-tertiary/40">
                  <th className="px-4 py-2.5 text-left text-xs font-medium text-text-muted">{t('columns.name')}</th>
                  <th className="px-4 py-2.5 text-left text-xs font-medium text-text-muted">{t('columns.status')}</th>
                  <th className="px-4 py-2.5 text-left text-xs font-medium text-text-muted">{t('columns.duration')}</th>
                  <th className="px-4 py-2.5 text-left text-xs font-medium text-text-muted">{t('columns.tokens')}</th>
                  <th className="px-4 py-2.5 text-center text-xs font-medium text-text-muted">{t('columns.spans')}</th>
                  <th className="px-4 py-2.5 text-left text-xs font-medium text-text-muted">{t('columns.time')}</th>
                </tr>
              </thead>
              <tbody>
                {traces.map((trace) => (
                  <TraceListRow
                    key={trace.id}
                    trace={trace}
                    onClick={() => setSelectedTraceId(trace.id)}
                  />
                ))}
              </tbody>
            </table>
          </div>

          {!allLoaded && (
            <div className="flex justify-center pt-1">
              <button
                onClick={loadMore}
                disabled={loading}
                className="text-xs text-text-muted hover:text-text-primary transition-colors disabled:opacity-50 flex items-center gap-1.5"
              >
                {loading ? (
                  <svg className="h-3.5 w-3.5 animate-spin" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth={2}>
                    <path d="M21 12a9 9 0 1 1-6.219-8.56" />
                  </svg>
                ) : (
                  <svg className="h-3.5 w-3.5" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth={2} strokeLinecap="round" strokeLinejoin="round">
                    <path d="M5 12h14" /><path d="m12 5 7 7-7 7" />
                  </svg>
                )}
                Load more ({traces.length}/{total})
              </button>
            </div>
          )}
        </>
      )}

      {selectedTraceId && (
        <TraceDetailDialog
          traceId={selectedTraceId}
          onClose={() => setSelectedTraceId(null)}
        />
      )}
    </div>
  )
}
