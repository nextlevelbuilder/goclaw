import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Badge, Button, ConfirmDialog, Skeleton, StatusChip, type StatusTone } from '../ui'
import { SectionCard } from './section-card'
import type { EvolutionSuggestion } from '../../types/evolution'

type NextStatus = 'approved' | 'rejected' | 'rolled_back'

interface EvolutionSuggestionsListProps {
  suggestions: EvolutionSuggestion[]
  loading: boolean
  readOnly?: boolean
  onUpdateStatus: (id: string, status: NextStatus) => Promise<void>
}

const STATUS_VIEW: Record<string, { tone: StatusTone; key: string }> = {
  pending: { tone: 'warning', key: 'statusPending' },
  approved: { tone: 'info', key: 'statusApproved' },
  applied: { tone: 'success', key: 'statusApplied' },
  rejected: { tone: 'danger', key: 'statusRejected' },
  rolled_back: { tone: 'idle', key: 'statusRolledBack' },
}

const CONFIRM_KEY: Record<NextStatus, string> = {
  approved: 'approveTitle',
  rejected: 'rejectTitle',
  rolled_back: 'rollbackTitle',
}

function formatParam(value: unknown): string {
  return typeof value === 'object' && value !== null ? JSON.stringify(value) : String(value)
}

export function EvolutionSuggestionsList({ suggestions, loading, readOnly = false, onUpdateStatus }: EvolutionSuggestionsListProps) {
  const { t, i18n } = useTranslation('agents')
  const [confirm, setConfirm] = useState<{ item: EvolutionSuggestion; action: NextStatus } | null>(null)

  const relative = (iso: string): string => {
    const diffSec = Math.round((new Date(iso).getTime() - Date.now()) / 1000)
    const rtf = new Intl.RelativeTimeFormat(i18n.language, { numeric: 'auto' })
    const abs = Math.abs(diffSec)
    if (abs < 3600) return rtf.format(Math.round(diffSec / 60), 'minute')
    if (abs < 86400) return rtf.format(Math.round(diffSec / 3600), 'hour')
    return rtf.format(Math.round(diffSec / 86400), 'day')
  }

  const pending = suggestions.filter((s) => s.status === 'pending').length
  const applied = suggestions.filter((s) => s.status === 'applied').length

  return (
    <SectionCard
      title={t('screen.evolution.suggestions')}
      subtitle={suggestions.length > 0 ? t('screen.evolution.suggestionsSummary', { pending, applied }) : undefined}
    >
      {loading ? (
        <div className="flex flex-col gap-2" aria-busy="true">
          <Skeleton className="h-20 w-full" />
          <Skeleton className="h-20 w-full" />
        </div>
      ) : suggestions.length === 0 ? (
        <p className="py-4 text-center text-ui font-normal text-ink-muted">{t('screen.evolution.noSuggestions')}</p>
      ) : (
        <ul className="flex flex-col gap-3">
          {suggestions.map((s) => {
            const view = STATUS_VIEW[s.status]
            const params = s.parameters ? Object.entries(s.parameters) : []
            return (
              <li key={s.id} className="flex flex-col gap-2 rounded-lg border border-border-subtle bg-surface-base p-3">
                <div className="flex flex-wrap items-center gap-2">
                  <Badge tone="accent">{s.suggestion_type}</Badge>
                  <StatusChip tone={view?.tone ?? 'idle'} label={view ? t(`screen.evolution.${view.key}`) : s.status} />
                  <time dateTime={s.created_at} className="ml-auto text-caption font-normal text-ink-muted">{relative(s.created_at)}</time>
                </div>
                <p className="text-ui text-ink">{s.suggestion}</p>
                {s.rationale && <p className="text-caption font-normal text-ink-muted">{s.rationale}</p>}
                {params.length > 0 && (
                  <div className="flex flex-wrap gap-1.5">
                    {params.map(([k, v]) => (
                      <code key={k} className="rounded-sm bg-surface-hover px-1.5 py-0.5 font-mono text-code text-ink-secondary">{k}: {formatParam(v)}</code>
                    ))}
                  </div>
                )}
                {(s.status === 'pending' || s.status === 'applied') && (
                  <div className="flex justify-end gap-2">
                    {s.status === 'pending' && (
                      <>
                        <Button size="sm" onClick={() => setConfirm({ item: s, action: 'rejected' })} disabled={readOnly}>{t('screen.evolution.reject')}</Button>
                        <Button size="sm" onClick={() => setConfirm({ item: s, action: 'approved' })} disabled={readOnly}>{t('screen.evolution.approve')}</Button>
                      </>
                    )}
                    {s.status === 'applied' && (
                      <Button size="sm" onClick={() => setConfirm({ item: s, action: 'rolled_back' })} disabled={readOnly}>{t('screen.evolution.rollback')}</Button>
                    )}
                  </div>
                )}
              </li>
            )
          })}
        </ul>
      )}

      <ConfirmDialog
        open={confirm !== null}
        onOpenChange={(open) => { if (!open) setConfirm(null) }}
        title={confirm ? t(`screen.evolution.${CONFIRM_KEY[confirm.action]}`) : ''}
        description={confirm ? t('screen.evolution.confirmDescription', { text: confirm.item.suggestion }) : undefined}
        confirmLabel={confirm ? t(`screen.evolution.${confirm.action === 'approved' ? 'approve' : confirm.action === 'rejected' ? 'reject' : 'rollback'}`) : undefined}
        variant={confirm?.action === 'rejected' ? 'destructive' : 'default'}
        onConfirm={async () => {
          if (confirm) await onUpdateStatus(confirm.item.id, confirm.action)
          setConfirm(null)
        }}
      />
    </SectionCard>
  )
}
