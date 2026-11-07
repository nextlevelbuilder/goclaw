// Expandable group card showing managers list and inline add form for a single channel group.

import { useTranslation } from 'react-i18next'
import { cn } from '../../lib/cn'
import { Badge, Button, DataTable, IconButton, SkeletonList, type DataTableColumn } from '../ui'
import { IconChevronDown, IconTrash } from '../common/Icons'
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
  /** Disables add and remove while the gateway is unreachable. */
  readOnly?: boolean
  onToggle: () => void
  onInlineUserIdChange: (v: string) => void
  onContactSearch: (v: string) => void
  onInlineAdd: () => void
  onRemove: (userId: string) => void
}

export function ManagerGroupCard({
  groupId, writerCount, expanded, loading, managers, adding, inlineUserId,
  contactOptions, readOnly = false, onToggle, onInlineUserIdChange, onContactSearch, onInlineAdd, onRemove,
}: ManagerGroupCardProps) {
  const { t } = useTranslation(['channels', 'common'])
  const bodyId = `manager-group-${groupId}`

  const columns: DataTableColumn<GroupManagerData>[] = [
    { id: 'userId', header: t('channels:detail.managers.columns.userId'), cell: (m) => <span className="font-mono text-code text-ink">{m.user_id}</span> },
    { id: 'name', header: t('channels:detail.managers.columns.name'), cell: (m) => <span className="text-ui text-ink-muted">{m.display_name || '—'}</span> },
    { id: 'username', header: t('channels:detail.managers.columns.username'), cell: (m) => <span className="text-ui text-ink-muted">{m.username ? `@${m.username}` : '—'}</span> },
    {
      id: 'actions',
      header: <span className="sr-only">{t('channels:screen.columns.actions')}</span>,
      className: 'w-14 text-right',
      cell: (m) => (
        <div className="flex justify-end">
          <IconButton
            aria-label={t('common:remove')}
            icon={<IconTrash size={14} />}
            size="sm"
            disabled={readOnly}
            onClick={() => onRemove(m.user_id)}
          />
        </div>
      ),
    },
  ]

  return (
    <div className="overflow-hidden rounded-lg border border-border-subtle bg-surface-base">
      <button
        type="button"
        onClick={onToggle}
        aria-expanded={expanded}
        aria-controls={bodyId}
        className="flex w-full items-center justify-between gap-3 px-4 py-3 text-left transition-colors hover:bg-surface-hover"
      >
        <span className="truncate font-mono text-code text-ink">{shortGroupId(groupId)}</span>
        <span className="flex shrink-0 items-center gap-3">
          <Badge>
            {writerCount === 1
              ? t('channels:detail.managers.managersCount', { count: writerCount })
              : t('channels:detail.managers.managersCountPlural', { count: writerCount })}
          </Badge>
          <IconChevronDown size={14} className={cn('text-ink-muted transition-transform', expanded && 'rotate-180')} />
        </span>
      </button>

      {expanded && (
        <div id={bodyId} className="flex flex-col gap-3 border-t border-border-subtle px-4 py-3">
          {loading ? (
            <SkeletonList rows={2} avatar={false} label={t('channels:detail.managers.loadingManagers')} />
          ) : managers.length === 0 ? (
            <p className="py-2 text-ui text-ink-muted">{t('channels:detail.managers.noManagers')}</p>
          ) : (
            <DataTable
              aria-label={t('channels:detail.managers.tableLabel', { group: shortGroupId(groupId) })}
              columns={columns}
              rows={managers}
              getRowId={(m) => m.user_id}
            />
          )}

          <div className="flex items-center gap-2">
            <div className="min-w-0 flex-1">
              <Combobox
                value={inlineUserId}
                onChange={(v) => { onInlineUserIdChange(v); onContactSearch(v) }}
                options={contactOptions}
                placeholder={t('channels:detail.managers.addForm.userIdPlaceholder')}
                disabled={readOnly}
              />
            </div>
            <Button onClick={onInlineAdd} loading={adding} disabled={readOnly || !inlineUserId.trim()}>
              {t('channels:detail.managers.addForm.add')}
            </Button>
          </div>
        </div>
      )}
    </div>
  )
}
