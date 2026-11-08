import { useTranslation } from 'react-i18next'
import { useSortable } from '@dnd-kit/sortable'
import { CSS } from '@dnd-kit/utilities'
import { Combobox } from '../common/Combobox'
import { IconTrash } from '../common/Icons'
import { Badge, Field, IconButton, Input, Switch } from '../ui'

export interface ProviderEntry {
  id: string
  provider: string
  model: string
  enabled: boolean
  timeout: number
  max_retries: number
  params?: Record<string, unknown>
}

interface SortableProviderCardProps {
  entry: ProviderEntry
  index: number
  providerOptions: { value: string; label: string }[]
  modelOptions: { value: string; label: string }[]
  modelLoading: boolean
  onUpdate: (id: string, updates: Partial<ProviderEntry>) => void
  onRemove: (id: string) => void
  onProviderChange: (id: string, providerName: string) => void
}

/** One provider in a media tool's fallback chain: drag to reorder, enable, pick model, timeout and retries. */
export function SortableProviderCard({
  entry, index, providerOptions, modelOptions, modelLoading,
  onUpdate, onRemove, onProviderChange,
}: SortableProviderCardProps) {
  const { t } = useTranslation(['tools', 'common'])
  const { attributes, listeners, setNodeRef, transform, transition, isDragging } = useSortable({ id: entry.id })

  const style = {
    transform: CSS.Transform.toString(transform),
    transition,
    opacity: isDragging ? 0.5 : 1,
  }
  const title = entry.provider || t('tools:builtin.mediaChain.newProvider')

  return (
    <div ref={setNodeRef} style={style} className="flex flex-col gap-3 rounded-md border border-border-subtle p-3">
      <div className="flex items-center justify-between">
        <div className="flex min-w-0 items-center gap-2">
          <button
            type="button"
            aria-label={t('common:dragToReorder')}
            className="shrink-0 cursor-grab touch-none rounded-sm p-2 text-ink-muted hover:text-ink focus-visible:outline-2 focus-visible:outline-focus-ring"
            {...attributes}
            {...listeners}
          >
            <svg className="size-4" viewBox="0 0 24 24" fill="currentColor" aria-hidden="true">
              <circle cx="9" cy="6" r="1.5" /><circle cx="15" cy="6" r="1.5" />
              <circle cx="9" cy="12" r="1.5" /><circle cx="15" cy="12" r="1.5" />
              <circle cx="9" cy="18" r="1.5" /><circle cx="15" cy="18" r="1.5" />
            </svg>
          </button>
          <Badge tone="neutral">#{index + 1}</Badge>
          <span id={`provider-${entry.id}`} className="truncate text-ui font-medium text-ink">{title}</span>
          {entry.model && <span className="truncate text-caption text-ink-muted">/ {entry.model}</span>}
        </div>
        <div className="flex items-center gap-2">
          <Switch checked={entry.enabled} onCheckedChange={(v) => onUpdate(entry.id, { enabled: v })} aria-labelledby={`provider-${entry.id}`} />
          <IconButton aria-label={t('common:remove')} icon={<IconTrash size={14} />} onClick={() => onRemove(entry.id)} />
        </div>
      </div>

      <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
        <Field label={t('tools:builtin.mediaChain.provider')}>
          <Combobox value={entry.provider} onChange={(v) => onProviderChange(entry.id, v)} options={providerOptions} placeholder={t('tools:builtin.mediaChain.selectProvider')} allowCustom />
        </Field>
        <Field label={t('tools:builtin.mediaChain.model')}>
          <Combobox value={entry.model} onChange={(v) => onUpdate(entry.id, { model: v })} options={modelOptions} placeholder={t('tools:builtin.mediaChain.selectModel')} loading={modelLoading} allowCustom />
        </Field>
        <Field label={t('tools:builtin.mediaChain.timeout')}>
          <Input
            type="number" min={1} max={600} value={entry.timeout}
            onChange={(e) => onUpdate(entry.id, { timeout: Math.max(1, Number(e.target.value)) })}
          />
        </Field>
        <Field label={t('tools:builtin.mediaChain.retries')}>
          <Input
            type="number" min={0} max={10} value={entry.max_retries}
            onChange={(e) => onUpdate(entry.id, { max_retries: Math.max(0, Number(e.target.value)) })}
          />
        </Field>
      </div>
    </div>
  )
}
