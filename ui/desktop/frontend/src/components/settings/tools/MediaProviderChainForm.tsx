import { useState, useEffect, useCallback, useRef } from 'react'
import { useTranslation } from 'react-i18next'
import {
  DndContext,
  closestCenter,
  KeyboardSensor,
  PointerSensor,
  useSensor,
  useSensors,
  type DragEndEvent,
} from '@dnd-kit/core'
import {
  SortableContext,
  verticalListSortingStrategy,
  useSortable,
  arrayMove,
  sortableKeyboardCoordinates,
} from '@dnd-kit/sortable'
import { CSS } from '@dnd-kit/utilities'
import { Switch } from '../../common/Switch'
import { Combobox } from '../../common/Combobox'
import { getApiClient } from '../../../lib/api'
import type { BuiltinToolData } from '../../../types/builtin-tool'

interface ProviderEntry {
  id: string
  provider: string
  model: string
  enabled: boolean
  timeout: number
  max_retries: number
  params?: Record<string, unknown>
}

interface ModelInfo {
  id: string
  name?: string
}

interface MediaProviderChainFormProps {
  tool: BuiltinToolData
  onSave: (name: string, settings: Record<string, unknown>) => Promise<void>
  onClose: () => void
}

/* ─── Sortable Card ─── */

function SortableProviderCard({
  entry,
  index,
  providerOptions,
  modelOptions,
  modelLoading,
  onUpdate,
  onRemove,
  onProviderChange,
}: {
  entry: ProviderEntry
  index: number
  providerOptions: { value: string; label: string }[]
  modelOptions: { value: string; label: string }[]
  modelLoading: boolean
  onUpdate: (id: string, updates: Partial<ProviderEntry>) => void
  onRemove: (id: string) => void
  onProviderChange: (id: string, providerName: string) => void
}) {
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
          {/* Drag handle */}
          <button
            type="button"
            className="cursor-grab text-text-muted hover:text-text-primary shrink-0 touch-none"
            {...attributes}
            {...listeners}
          >
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
          <Combobox
            value={entry.provider}
            onChange={(v) => onProviderChange(entry.id, v)}
            options={providerOptions}
            placeholder={t('builtin.mediaChain.selectProvider')}
            allowCustom
          />
        </div>
        <div className="space-y-1">
          <label className="text-xs font-medium text-text-secondary">{t('builtin.mediaChain.model')}</label>
          <Combobox
            value={entry.model}
            onChange={(v) => onUpdate(entry.id, { model: v })}
            options={modelOptions}
            placeholder={t('builtin.mediaChain.selectModel')}
            loading={modelLoading}
            allowCustom
          />
        </div>
      </div>

      {/* Timeout + Retries */}
      <div className="grid grid-cols-2 gap-2">
        <div className="space-y-1">
          <label className="text-xs font-medium text-text-secondary">{t('builtin.mediaChain.timeout')}</label>
          <input
            type="number"
            min={1}
            max={600}
            value={entry.timeout}
            onChange={(e) => onUpdate(entry.id, { timeout: Math.max(1, Number(e.target.value)) })}
            className="w-full bg-surface-tertiary border border-border rounded-lg px-3 py-1.5 text-base md:text-sm text-text-primary focus:outline-none focus:ring-1 focus:ring-accent"
          />
        </div>
        <div className="space-y-1">
          <label className="text-xs font-medium text-text-secondary">{t('builtin.mediaChain.retries')}</label>
          <input
            type="number"
            min={0}
            max={10}
            value={entry.max_retries}
            onChange={(e) => onUpdate(entry.id, { max_retries: Math.max(0, Number(e.target.value)) })}
            className="w-full bg-surface-tertiary border border-border rounded-lg px-3 py-1.5 text-base md:text-sm text-text-primary focus:outline-none focus:ring-1 focus:ring-accent"
          />
        </div>
      </div>
    </div>
  )
}

/* ─── Main Form ─── */

export function MediaProviderChainForm({ tool, onSave, onClose }: MediaProviderChainFormProps) {
  const { t } = useTranslation(['tools', 'common'])
  const [chain, setChain] = useState<ProviderEntry[]>([])
  const [saving, setSaving] = useState(false)
  const [error, setError] = useState('')

  const [dbProviders, setDbProviders] = useState<{ id: string; name: string }[]>([])
  const [modelsByProvider, setModelsByProvider] = useState<Record<string, ModelInfo[]>>({})

  // Track which providers we've already started loading models for
  const loadingRef = useRef<Set<string>>(new Set())

  // Parse initial chain entries with stable IDs
  useEffect(() => {
    const settings = tool.settings as { providers?: Omit<ProviderEntry, 'id'>[] }
    const entries = (settings?.providers ?? []).map((p) => ({
      ...p,
      id: crypto.randomUUID(),
      enabled: p.enabled ?? true,
      timeout: p.timeout ?? 120,
      max_retries: p.max_retries ?? 2,
    }))
    setChain(entries)
    setError('')
    setSaving(false)
    loadingRef.current.clear()

    // Fetch providers list
    getApiClient().get<{ providers: { id: string; name: string; display_name?: string }[] | null }>('/v1/providers')
      .then((res) => {
        setDbProviders((res.providers ?? []).map((p) => ({ id: p.id, name: p.name })))
      })
      .catch(() => {})
  }, [tool])

  // Once dbProviders loads, fetch models for all existing chain entries
  useEffect(() => {
    if (dbProviders.length === 0) return
    for (const entry of chain) {
      if (entry.provider && !modelsByProvider[entry.provider] && !loadingRef.current.has(entry.provider)) {
        loadModelsForProvider(entry.provider)
      }
    }
  }, [dbProviders]) // eslint-disable-line react-hooks/exhaustive-deps

  async function loadModelsForProvider(providerName: string) {
    if (modelsByProvider[providerName] || loadingRef.current.has(providerName)) return
    loadingRef.current.add(providerName)
    const prov = dbProviders.find((p) => p.name === providerName)
    if (!prov) return
    try {
      const res = await getApiClient().get<{ models?: ModelInfo[] }>(`/v1/providers/${prov.id}/models`)
      setModelsByProvider((prev) => ({ ...prev, [providerName]: res.models ?? [] }))
    } catch {
      // Mark as loaded with empty list so we don't retry — user can still type manually
      setModelsByProvider((prev) => ({ ...prev, [providerName]: [] }))
    }
  }

  const handleUpdate = useCallback((id: string, updates: Partial<ProviderEntry>) => {
    setChain((prev) => prev.map((p) => p.id === id ? { ...p, ...updates } : p))
  }, [])

  const handleProviderChange = useCallback((id: string, providerName: string) => {
    setChain((prev) => prev.map((p) => p.id === id ? { ...p, provider: providerName, model: '' } : p))
    if (providerName) loadModelsForProvider(providerName)
  }, [dbProviders, modelsByProvider]) // eslint-disable-line react-hooks/exhaustive-deps

  const handleRemove = useCallback((id: string) => {
    setChain((prev) => prev.filter((p) => p.id !== id))
  }, [])

  function addEntry() {
    setChain((prev) => [...prev, {
      id: crypto.randomUUID(),
      provider: '',
      model: '',
      enabled: true,
      timeout: 120,
      max_retries: 2,
    }])
  }

  async function handleSave() {
    setSaving(true)
    setError('')
    try {
      // Strip internal `id` field before saving
      const serialized = chain.map(({ id: _id, ...rest }) => rest)
      await onSave(tool.name, { providers: serialized })
      onClose()
    } catch (err) {
      setError((err as Error).message || 'Failed to save')
    } finally {
      setSaving(false)
    }
  }

  // Drag-and-drop
  const sensors = useSensors(
    useSensor(PointerSensor),
    useSensor(KeyboardSensor, { coordinateGetter: sortableKeyboardCoordinates }),
  )

  const handleDragEnd = useCallback((event: DragEndEvent) => {
    const { active, over } = event
    if (over && active.id !== over.id) {
      setChain((prev) => {
        const oldIndex = prev.findIndex((e) => e.id === active.id)
        const newIndex = prev.findIndex((e) => e.id === over.id)
        return arrayMove(prev, oldIndex, newIndex)
      })
    }
  }, [])

  const providerOptions = dbProviders.map((p) => ({ value: p.name, label: p.name }))

  // Disable save if any entry is missing provider or model
  const hasIncomplete = chain.some((e) => !e.provider || !e.model)

  return (
    <>
      <div className="max-h-[60vh] overflow-y-auto p-5 space-y-3">
        <DndContext sensors={sensors} collisionDetection={closestCenter} onDragEnd={handleDragEnd}>
          <SortableContext items={chain.map((e) => e.id)} strategy={verticalListSortingStrategy}>
            {chain.map((entry, i) => {
              const models = modelsByProvider[entry.provider] ?? []
              const modelOpts = models.map((m) => ({ value: m.id, label: m.name || m.id }))
              const isModelLoading = entry.provider !== '' && !modelsByProvider[entry.provider]
              return (
                <SortableProviderCard
                  key={entry.id}
                  entry={entry}
                  index={i}
                  providerOptions={providerOptions}
                  modelOptions={modelOpts}
                  modelLoading={isModelLoading}
                  onUpdate={handleUpdate}
                  onRemove={handleRemove}
                  onProviderChange={handleProviderChange}
                />
              )
            })}
          </SortableContext>
        </DndContext>

        <button onClick={addEntry} className="text-xs text-accent hover:text-accent-hover flex items-center gap-1 transition-colors">
          <svg className="h-3.5 w-3.5" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth={2} strokeLinecap="round" strokeLinejoin="round">
            <path d="M5 12h14" /><path d="M12 5v14" />
          </svg>
          {t('builtin.mediaChain.addProvider')}
        </button>
      </div>

      {error && <div className="px-5"><p className="text-xs text-error">{error}</p></div>}
      <div className="flex items-center justify-end gap-2 border-t border-border px-5 py-4">
        <button type="button" onClick={onClose} className="border border-border rounded-lg px-4 py-1.5 text-sm text-text-secondary hover:bg-surface-tertiary transition-colors">
          {t('builtin.mediaChain.cancel')}
        </button>
        <button type="button" onClick={handleSave} disabled={saving || hasIncomplete} className="bg-accent rounded-lg px-4 py-1.5 text-sm text-white hover:bg-accent-hover disabled:opacity-50 transition-colors">
          {saving ? t('builtin.mediaChain.saving') : t('builtin.mediaChain.save')}
        </button>
      </div>
    </>
  )
}
