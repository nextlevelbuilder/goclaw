import { useState, type KeyboardEvent, type ReactNode } from 'react'
import { useTranslation } from 'react-i18next'
import { cn } from '../../lib/cn'
import { IconClose } from '../common/Icons'
import { Checkbox } from './Checkbox'
import { IconButton } from './IconButton'

export interface DataTableColumn<T> {
  id: string
  header: ReactNode
  cell: (row: T) => ReactNode
  /** Classes for both the header and body cells of this column (width, alignment). */
  className?: string
}

export interface DataTableProps<T> {
  columns: DataTableColumn<T>[]
  rows: T[]
  getRowId: (row: T) => string
  /** Accessible table name. */
  'aria-label': string
  /** Adds a checkbox column. */
  selectable?: boolean
  /** Controlled selection. Omit both to let the table hold its own selection. */
  selectedIds?: string[]
  onSelectionChange?: (ids: string[]) => void
  /** Actions for the floating bulk bar shown while rows are selected. */
  bulkActions?: (selectedIds: string[]) => ReactNode
  /** Row activation (click or Enter). */
  onRowClick?: (row: T) => void
  /** Highlights the row that is open in a detail panel. */
  activeRowId?: string | null
  /** Rendered instead of the body when rows is empty. */
  empty?: ReactNode
  className?: string
}

/** Table shell: header row, row-h rows, optional selection and a floating bulk-action bar. */
export function DataTable<T>({
  columns,
  rows,
  getRowId,
  selectable = false,
  selectedIds,
  onSelectionChange,
  bulkActions,
  onRowClick,
  activeRowId,
  empty,
  className,
  ...aria
}: DataTableProps<T>) {
  const { t } = useTranslation('common')
  const [innerSelection, setInnerSelection] = useState<string[]>([])
  const selection = selectedIds ?? innerSelection
  const setSelection = (ids: string[]) => {
    if (selectedIds === undefined) setInnerSelection(ids)
    onSelectionChange?.(ids)
  }

  const ids = rows.map(getRowId)
  const selectedSet = new Set(selection)
  const selectedVisible = ids.filter((id) => selectedSet.has(id))
  const allSelected = ids.length > 0 && selectedVisible.length === ids.length
  const someSelected = selectedVisible.length > 0 && !allSelected

  const toggleAll = (checked: boolean) => setSelection(checked ? ids : [])
  const toggleRow = (id: string, checked: boolean) =>
    setSelection(checked ? [...selection.filter((s) => s !== id), id] : selection.filter((s) => s !== id))

  const onRowKey = (e: KeyboardEvent<HTMLTableRowElement>, row: T) => {
    if (!onRowClick || e.target !== e.currentTarget) return
    if (e.key === 'Enter' || e.key === ' ') {
      e.preventDefault()
      onRowClick(row)
    }
  }

  const colCount = columns.length + (selectable ? 1 : 0)

  return (
    <div className={cn('relative', className)}>
      <div className="overflow-x-auto rounded-lg border border-border-subtle">
        <table className="w-full min-w-[600px] border-collapse text-ui" aria-label={aria['aria-label']}>
          <thead>
            <tr>
              {selectable && (
                <th scope="col" className="w-11 border-b border-border-subtle bg-surface-raised px-4 py-2 text-left">
                  <Checkbox
                    aria-label={t('selectAllRows')}
                    checked={allSelected}
                    indeterminate={someSelected}
                    onCheckedChange={toggleAll}
                    disabled={rows.length === 0}
                  />
                </th>
              )}
              {columns.map((c) => (
                <th
                  key={c.id}
                  scope="col"
                  className={cn('border-b border-border-subtle bg-surface-raised px-4 py-2 text-left text-overline text-ink-muted', c.className)}
                >
                  {c.header}
                </th>
              ))}
            </tr>
          </thead>
          <tbody>
            {rows.length === 0 && empty ? (
              <tr>
                <td colSpan={colCount}>{empty}</td>
              </tr>
            ) : (
              rows.map((row) => {
                const id = getRowId(row)
                const selected = selectedSet.has(id)
                return (
                  <tr
                    key={id}
                    aria-selected={selectable ? selected : undefined}
                    data-active={activeRowId === id || undefined}
                    tabIndex={onRowClick ? 0 : undefined}
                    onClick={onRowClick ? () => onRowClick(row) : undefined}
                    onKeyDown={onRowClick ? (e) => onRowKey(e, row) : undefined}
                    className={cn(
                      'border-b border-border-subtle last:border-b-0 transition-colors',
                      onRowClick && 'cursor-pointer hover:bg-surface-hover',
                      (selected || activeRowId === id) && 'bg-accent-subtle hover:bg-accent-subtle',
                    )}
                  >
                    {selectable && (
                      <td className="h-row w-11 px-4 py-2" onClick={(e) => e.stopPropagation()}>
                        <Checkbox
                          aria-label={t('selectRow')}
                          checked={selected}
                          onCheckedChange={(checked) => toggleRow(id, checked)}
                        />
                      </td>
                    )}
                    {columns.map((c) => (
                      <td key={c.id} className={cn('h-row px-4 py-2 text-ink', c.className)}>
                        {c.cell(row)}
                      </td>
                    ))}
                  </tr>
                )
              })
            )}
          </tbody>
        </table>
      </div>

      {selectable && bulkActions && selection.length > 0 && (
        <div className="pointer-events-none sticky bottom-4 z-10 mt-4 flex justify-center">
          <div
            role="toolbar"
            aria-label={t('selectedCount', { count: selection.length })}
            className="pointer-events-auto flex items-center gap-2 rounded-lg border border-border-subtle bg-surface-raised py-2 pl-4 pr-2 text-ui shadow-popover"
          >
            <span className="text-ink">{t('selectedCount', { count: selection.length })}</span>
            {bulkActions(selection)}
            <IconButton aria-label={t('clearSelection')} icon={<IconClose size={14} />} size="sm" onClick={() => setSelection([])} />
          </div>
        </div>
      )}
    </div>
  )
}
