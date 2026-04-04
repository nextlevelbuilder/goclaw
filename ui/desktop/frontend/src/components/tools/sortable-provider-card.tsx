import { useTranslation } from 'react-i18next'
import { useSortable } from '@dnd-kit/sortable'
import { CSS } from '@dnd-kit/utilities'
import { Switch } from '../common/Switch'
import { Combobox } from '../common/Combobox'

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

  return (
    <div ref={setNodeRef} style={style} className="rounded-lg border border-border p-3 space-y-3">
      {/* Header */}
      <div className="flex items-center justify-between">
        <div className="flex items-center gap-2">
          <button type="button" className="cursor-grab text-text-muted hover:text-text-primary shrink-0 touch-none" {...attributes} {...listeners}>
            <svg className="h-4 w-4" viewBox="0 0 24 24" fill="currentColor">
              <circle cx="9" cy="6" r="1.5" /><circle cx="15" cy="6" r="1.5" />
              <circle cx="9" cy="12" r="1.5" /><circle cx="15" cy="12" r="1.5" />
              <circle cx="9" cy="18" r="1.5" /><circle cx="15" cy="18" r="1.5" />
            </svg>
          </button>
          <span className="text-[11px] font-mono text-text-muted bg-surface-tertiary rounded px-1.5 py-0.5">#{index + 1}</span>
          <span className="text-sm font-medium text-text-primary">{entry.provider || t('builtin.mediaChain.newProvider')}</span>
          {entry.model && <span className="text-xs text-text-muted">/ {entry.model}</span>}
        </div>
        <div className="flex items-center gap-2">
          <Switch checked={entry.enabled} onCheckedChange={(v) => onUpdate(entry.id, { enabled: v })} />
          <button onClick={() => onRemove(entry.id)} className="p-1 text-text-muted hover:text-error transition-colors">
            <svg className="h-3.5 w-3.5" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth={2} strokeLinecap="round" strokeLinejoin="round">
              <polyline points="3 6 5 6 21 6" /><path d="M19 6v14a2 2 0 0 1-2 2H7a2 2 0 0 1-2-2V6m3 0V4a2 2 0 0 1 2-2h4a2 2 0 0 1 2 2v2" />
            </svg>
          </button>
        </div>
      </div>

      {/* Provider + Model */}
      <div className="grid grid-cols-2 gap-2">
        <div className="space-y-1">
          <label className="text-xs font-medium text-text-secondary">{t('builtin.mediaChain.provider')}</label>
          <Combobox value={entry.provider} onChange={(v) => onProviderChange(entry.id, v)} options={providerOptions} placeholder={t('builtin.mediaChain.selectProvider')} allowCustom />
        </div>
        <div className="space-y-1">
          <label className="text-xs font-medium text-text-secondary">{t('builtin.mediaChain.model')}</label>
          <Combobox value={entry.model} onChange={(v) => onUpdate(entry.id, { model: v })} options={modelOptions} placeholder={t('builtin.mediaChain.selectModel')} loading={modelLoading} allowCustom />
        </div>
      </div>

      {/* Timeout + Retries */}
      <div className="grid grid-cols-2 gap-2">
        <div className="space-y-1">
          <label className="text-xs font-medium text-text-secondary">{t('builtin.mediaChain.timeout')}</label>
          <input type="number" min={1} max={600} value={entry.timeout}
            onChange={(e) => onUpdate(entry.id, { timeout: Math.max(1, Number(e.target.value)) })}
            className="w-full bg-surface-tertiary border border-border rounded-lg px-3 py-1.5 text-base md:text-sm text-text-primary focus:outline-none focus:ring-1 focus:ring-accent"
          />
        </div>
        <div className="space-y-1">
          <label className="text-xs font-medium text-text-secondary">{t('builtin.mediaChain.retries')}</label>
          <input type="number" min={0} max={10} value={entry.max_retries}
            onChange={(e) => onUpdate(entry.id, { max_retries: Math.max(0, Number(e.target.value)) })}
            className="w-full bg-surface-tertiary border border-border rounded-lg px-3 py-1.5 text-base md:text-sm text-text-primary focus:outline-none focus:ring-1 focus:ring-accent"
          />
        </div>
      </div>
    </div>
  )
}
