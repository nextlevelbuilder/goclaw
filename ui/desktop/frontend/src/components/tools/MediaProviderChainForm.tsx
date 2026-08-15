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
  arrayMove,
  sortableKeyboardCoordinates,
} from '@dnd-kit/sortable'
import { getApiClient } from '../../lib/api'
import { SortableProviderCard, type ProviderEntry } from './sortable-provider-card'
import type { BuiltinToolData } from '../../types/builtin-tool'

interface ModelInfo {
  id: string
  name?: string
}

interface MediaProviderChainFormProps {
  tool: BuiltinToolData
  onSave: (name: string, settings: Record<string, unknown>) => Promise<void>
  onClose: () => void
}

export function MediaProviderChainForm({ tool, onSave, onClose }: MediaProviderChainFormProps) {
  const { t } = useTranslation(['tools', 'common'])
  const [chain, setChain] = useState<ProviderEntry[]>([])
  const [saving, setSaving] = useState(false)
  const [error, setError] = useState('')

  const [dbProviders, setDbProviders] = useState<{ id: string; name: string }[]>([])
  const [modelsByProvider, setModelsByProvider] = useState<Record<string, ModelInfo[]>>({})
  const loadingRef = useRef<Set<string>>(new Set())

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
    setChain((prev) => [...prev, { id: crypto.randomUUID(), provider: '', model: '', enabled: true, timeout: 120, max_retries: 2 }])
  }

  async function handleSave() {
    setSaving(true)
    setError('')
    try {
      const serialized = chain.map(({ id: _id, ...rest }) => rest)
      await onSave(tool.name, { providers: serialized })
      onClose()
    } catch (err) {
      setError((err as Error).message || 'Failed to save')
    } finally {
      setSaving(false)
    }
  }

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
