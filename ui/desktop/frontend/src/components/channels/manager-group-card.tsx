// Expandable group card showing managers list and inline add form for a single channel group.

import { useTranslation } from 'react-i18next'
import { Combobox } from '../common/Combobox'
import type { GroupManagerData } from '../../types/channel'

function shortGroupId(id: string): string {
  return id.match(/^group:[^:]+:(.+)$/)?.[1] ?? id
}

interface ManagerGroupCardProps {
  groupId: string
  writerCount: number
  expanded: boolean
  loading: boolean
  managers: GroupManagerData[]
  adding: boolean
  inlineUserId: string
  contactOptions: { value: string; label: string }[]
  onToggle: () => void
  onInlineUserIdChange: (v: string) => void
  onContactSearch: (v: string) => void
  onInlineAdd: () => void
  onRemove: (userId: string) => void
}

export function ManagerGroupCard({
  groupId, writerCount, expanded, loading, managers, adding, inlineUserId,
  contactOptions, onToggle, onInlineUserIdChange, onContactSearch, onInlineAdd, onRemove,
}: ManagerGroupCardProps) {
  const { t } = useTranslation('channels')

  return (
    <div className="border border-border rounded-lg overflow-hidden">
      <button
        onClick={onToggle}
        className="w-full flex items-center justify-between px-4 py-3 hover:bg-surface-tertiary/30 transition-colors text-left cursor-pointer"
      >
        <span className="text-xs font-medium text-text-primary font-mono">{shortGroupId(groupId)}</span>
        <div className="flex items-center gap-3">
          <span className="rounded-full bg-surface-tertiary px-2 py-0.5 text-[11px] text-text-muted tabular-nums">
            {writerCount === 1
              ? t('detail.managers.managersCount', { count: writerCount })
              : t('detail.managers.managersCountPlural', { count: writerCount })}
          </span>
          <svg
            className={`w-3.5 h-3.5 text-text-muted transition-transform ${expanded ? 'rotate-90' : ''}`}
            viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth={2.5}
          >
            <path d="M9 18l6-6-6-6" />
          </svg>
        </div>
      </button>

      {expanded && (
        <div className="border-t border-border bg-surface-tertiary/10 px-4 pb-3 pt-2 space-y-3">
          {loading ? (
            <p className="text-xs text-text-muted py-2">{t('detail.managers.loadingManagers')}</p>
          ) : managers.length === 0 ? (
            <p className="text-xs text-text-muted py-2">{t('detail.managers.noManagers')}</p>
          ) : (
            <div className="rounded-md border border-border bg-surface-secondary overflow-hidden">
              <table className="w-full text-xs">
                <thead>
                  <tr className="border-b border-border bg-surface-tertiary/40">
                    <th className="px-3 py-1.5 text-left text-[11px] font-medium text-text-muted">{t('detail.managers.columns.userId')}</th>
                    <th className="px-3 py-1.5 text-left text-[11px] font-medium text-text-muted">{t('detail.managers.columns.name')}</th>
                    <th className="px-3 py-1.5 text-left text-[11px] font-medium text-text-muted">{t('detail.managers.columns.username')}</th>
                    <th className="px-3 py-1.5 w-10" />
                  </tr>
                </thead>
                <tbody>
                  {managers.map((m) => (
                    <tr key={m.user_id} className="border-b border-border last:border-0 hover:bg-surface-tertiary/20">
                      <td className="px-3 py-1.5 font-mono">{m.user_id}</td>
                      <td className="px-3 py-1.5 text-text-muted">{m.display_name || '—'}</td>
                      <td className="px-3 py-1.5 text-text-muted">{m.username ? `@${m.username}` : '—'}</td>
                      <td className="px-3 py-1.5 text-right">
                        <button
                          onClick={() => onRemove(m.user_id)}
                          className="text-[11px] text-text-muted hover:text-error transition-colors cursor-pointer"
                        >
                          <svg className="h-3.5 w-3.5" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth={2} strokeLinecap="round" strokeLinejoin="round">
                            <path d="M3 6h18" /><path d="M19 6v14c0 1-1 2-2 2H7c-1 0-2-1-2-2V6" /><path d="M8 6V4c0-1 1-2 2-2h4c1 0 2 1 2 2v2" />
                          </svg>
                        </button>
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          )}

          {/* Inline add */}
          <div className="flex gap-2 items-end">
            <div className="flex-1">
              <Combobox
                value={inlineUserId}
                onChange={(v) => { onInlineUserIdChange(v); onContactSearch(v) }}
                options={contactOptions}
                placeholder={t('detail.managers.addForm.userIdPlaceholder')}
              />
            </div>
            <button
              onClick={onInlineAdd}
              disabled={adding || !inlineUserId.trim()}
              className="px-3 py-1.5 bg-accent text-white text-xs rounded-lg disabled:opacity-50 cursor-pointer hover:bg-accent-hover transition-colors shrink-0"
            >
              {t('detail.managers.addForm.add')}
            </button>
          </div>
        </div>
      )}
    </div>
  )
}
