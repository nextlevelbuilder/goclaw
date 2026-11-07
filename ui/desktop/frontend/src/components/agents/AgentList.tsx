import { useState, useCallback, useEffect, useMemo, useRef } from 'react'
import { useTranslation } from 'react-i18next'
import { useAgentCrud } from '../../hooks/use-agent-crud'
import { useAgentStore } from '../../stores/agent-store'
import { useGatewayStatus } from '../layout/use-gateway-status'
import { chatWithAgent } from '../layout/shell-actions'
import {
  Button, ConfirmDialog, EmptyState, ErrorState, Input, PageHeader, SegmentedControl, Select, Skeleton, SkeletonList, toast,
} from '../ui'
import { IconBot, IconPlus, IconSearch } from '../common/Icons'
import { SummoningModal } from '../onboarding/SummoningModal'
import { cn } from '../../lib/cn'
import { AgentCard } from './AgentCard'
import { AgentDetailPanel } from './AgentDetailPanel'
import { AgentFormDialog } from './AgentFormDialog'
import { AgentTable } from './AgentTable'
import { AGENT_PRESETS, resolvePreset } from './agent-preset-selector'
import { agentDisplayName, agentStatusView, isAgentEnabled } from './agent-display'
import type { AgentData, AgentInput } from '../../types/agent'

type ViewMode = 'list' | 'grid'
type StatusFilter = 'all' | 'active' | 'inactive' | 'summoning' | 'failed'

function errorMessage(err: unknown): string | undefined {
  return err instanceof Error ? err.message : undefined
}

export function AgentList() {
  const { t } = useTranslation(['agents', 'common', 'desktop'])
  const {
    agents, loading, error, atLimit, maxAgents, fetchAgents,
    createAgent, updateAgent, deleteAgent, setAgentsEnabled, deleteAgents, resummonAgent, cancelSummonAgent,
  } = useAgentCrud()
  const { disconnected } = useGatewayStatus()
  const setStoreAgents = useAgentStore((s) => s.setAgents)

  const [view, setView] = useState<ViewMode>('list')
  const [query, setQuery] = useState('')
  const [statusFilter, setStatusFilter] = useState<StatusFilter>('all')
  const [modelFilter, setModelFilter] = useState('all')
  const [selectedIds, setSelectedIds] = useState<string[]>([])
  const [retrying, setRetrying] = useState(false)

  const [formOpen, setFormOpen] = useState(false)
  const [presetKey, setPresetKey] = useState<string | null>(null)
  const [detailId, setDetailId] = useState<string | null>(null)
  const [deletingAgent, setDeletingAgent] = useState<AgentData | null>(null)
  const [bulkDeleteIds, setBulkDeleteIds] = useState<string[] | null>(null)
  const [deleting, setDeleting] = useState(false)
  const [summoningAgent, setSummoningAgent] = useState<{ id: string; name: string } | null>(null)

  const detailAgent = useMemo(() => agents.find((a) => a.id === detailId) ?? null, [agents, detailId])
  const nameOf = useCallback((a: AgentData) => agentDisplayName(a) ?? t('agents:screen.list.unnamed'), [t])

  const refreshSidebar = useCallback((list: AgentData[]) => {
    setStoreAgents(list.map((a) => {
      const otherCfg = typeof a.other_config === 'object' && a.other_config !== null ? a.other_config as Record<string, unknown> : {}
      return {
        id: a.id,
        key: a.agent_key,
        name: a.display_name || a.agent_key,
        model: a.model ?? 'unknown',
        status: 'online' as const,
        emoji: typeof otherCfg.emoji === 'string' ? otherCfg.emoji : undefined,
      }
    }))
  }, [setStoreAgents])

  // The sidebar roster is refreshed only after writes, so statuses the chat sets are not reset on mount.
  const sidebarDirty = useRef(false)
  const reload = useCallback(async () => {
    sidebarDirty.current = true
    await fetchAgents()
  }, [fetchAgents])

  useEffect(() => {
    if (!sidebarDirty.current || loading) return
    sidebarDirty.current = false
    refreshSidebar(agents)
  }, [agents, loading, refreshSidebar])

  const retry = async () => {
    setRetrying(true)
    try { await reload() } finally { setRetrying(false) }
  }

  const modelOptions = useMemo(() => [...new Set(agents.map((a) => a.model).filter(Boolean))].sort(), [agents])

  const visible = useMemo(() => {
    const q = query.trim().toLocaleLowerCase()
    return agents.filter((a) => {
      if (statusFilter !== 'all' && agentStatusView(a.status).key !== statusFilter) return false
      if (modelFilter !== 'all' && a.model !== modelFilter) return false
      if (!q) return true
      return [a.display_name, a.agent_key, a.model, a.agent_description, a.frontmatter]
        .some((v) => typeof v === 'string' && v.toLocaleLowerCase().includes(q))
    })
  }, [agents, query, statusFilter, modelFilter])

  const filtering = query.trim() !== '' || statusFilter !== 'all' || modelFilter !== 'all'
  const activeCount = agents.filter(isAgentEnabled).length

  // ── writes ──────────────────────────────────────────────────────────────

  const handleCreate = async (input: AgentInput) => {
    try {
      const created = await createAgent(input)
      toast.success(t('agents:screen.toast.created'))
      if (created.status === 'summoning') {
        setSummoningAgent({ id: created.id, name: created.display_name || created.agent_key })
      }
      await reload()
      return created
    } catch (err) {
      toast.error(t('agents:screen.toast.createFailed'), errorMessage(err))
      throw err
    }
  }

  const handleSave = async (id: string, updates: Partial<AgentData>) => {
    try {
      await updateAgent(id, updates)
      toast.success(t('agents:screen.toast.updated'))
      await reload()
    } catch (err) {
      toast.error(t('agents:screen.toast.updateFailed'), errorMessage(err))
      throw err
    }
  }

  const handleToggle = async (agent: AgentData, enabled: boolean) => {
    try {
      await updateAgent(agent.id, { status: enabled ? 'active' : 'idle' })
    } catch (err) {
      toast.error(t('agents:screen.toast.updateFailed'), errorMessage(err))
    }
  }

  const handleBulkEnable = async (enabled: boolean) => {
    const ids = selectedIds
    const res = await setAgentsEnabled(ids, enabled)
    if (res.failed.length > 0) {
      toast.error(t('agents:screen.bulk.partialFailed', { failed: res.failed.length, total: ids.length }))
    } else {
      toast.success(t('agents:screen.bulk.updated', { count: res.done.length }))
    }
    setSelectedIds(res.failed)
  }

  const handleDeleteOne = async () => {
    if (!deletingAgent) return
    setDeleting(true)
    try {
      await deleteAgent(deletingAgent.id)
      toast.success(t('agents:screen.toast.deleted', { name: nameOf(deletingAgent) }))
      setSelectedIds((ids) => ids.filter((id) => id !== deletingAgent.id))
      if (detailId === deletingAgent.id) setDetailId(null)
      setDeletingAgent(null)
      await reload()
    } catch (err) {
      toast.error(t('agents:screen.toast.deleteFailed'), errorMessage(err))
    } finally {
      setDeleting(false)
    }
  }

  const handleDeleteMany = async () => {
    if (!bulkDeleteIds) return
    setDeleting(true)
    try {
      const res = await deleteAgents(bulkDeleteIds)
      if (res.failed.length > 0) {
        toast.error(t('agents:screen.bulk.partialFailed', { failed: res.failed.length, total: bulkDeleteIds.length }))
      } else {
        toast.success(t('agents:screen.bulk.deleted', { count: res.done.length }))
      }
      setSelectedIds(res.failed)
      setBulkDeleteIds(null)
      await reload()
    } finally {
      setDeleting(false)
    }
  }

  const handleResummon = async (agent: AgentData) => {
    try {
      await resummonAgent(agent.id)
      setSummoningAgent({ id: agent.id, name: nameOf(agent) })
    } catch (err) {
      toast.error(t('agents:screen.toast.resummonFailed'), errorMessage(err))
    }
  }

  const handleSummoningComplete = () => {
    setSummoningAgent(null)
    void reload()
  }

  const openCreate = (key: string | null = null) => {
    setPresetKey(key)
    setFormOpen(true)
  }

  const bulkNames = (bulkDeleteIds ?? [])
    .map((id) => agents.find((a) => a.id === id))
    .filter((a): a is AgentData => !!a)
    .map(nameOf)
    .join(', ')

  // ── rendering ───────────────────────────────────────────────────────────

  const dialogs = (
    <>
      <AgentFormDialog open={formOpen} onOpenChange={setFormOpen} initialPresetKey={presetKey} onSubmit={handleCreate} />

      <ConfirmDialog
        open={!!deletingAgent}
        onOpenChange={(open) => { if (!open) setDeletingAgent(null) }}
        title={t('agents:screen.deleteOne.title', { name: deletingAgent ? nameOf(deletingAgent) : '' })}
        description={t('agents:screen.deleteOne.description', { name: deletingAgent ? nameOf(deletingAgent) : '' })}
        confirmLabel={t('agents:screen.deleteOne.confirm')}
        variant="destructive"
        loading={deleting}
        onConfirm={handleDeleteOne}
      />

      <ConfirmDialog
        open={bulkDeleteIds !== null}
        onOpenChange={(open) => { if (!open) setBulkDeleteIds(null) }}
        title={t('agents:screen.deleteMany.title', { count: bulkDeleteIds?.length ?? 0 })}
        description={(
          <>
            {t('agents:screen.deleteMany.description', { names: bulkNames })}{' '}
            {t('agents:screen.deleteMany.typeToConfirm', { word: t('agents:screen.deleteMany.word') })}
          </>
        )}
        confirmLabel={t('agents:screen.deleteMany.confirm', { count: bulkDeleteIds?.length ?? 0 })}
        variant="destructive"
        confirmValue={t('agents:screen.deleteMany.word')}
        loading={deleting}
        onConfirm={handleDeleteMany}
      />

      {summoningAgent && (
        <SummoningModal
          agentId={summoningAgent.id}
          agentName={summoningAgent.name}
          onContinue={handleSummoningComplete}
          onCancel={cancelSummonAgent}
        />
      )}
    </>
  )

  if (detailAgent) {
    return (
      <>
        <AgentDetailPanel
          key={detailAgent.id}
          agent={detailAgent}
          readOnly={disconnected}
          onSave={handleSave}
          onResummon={() => handleResummon(detailAgent)}
          onDelete={setDeletingAgent}
          onClose={() => setDetailId(null)}
        />
        {dialogs}
      </>
    )
  }

  const header = (
    <PageHeader
      title={t('agents:title')}
      description={
        loading ? t('agents:screen.list.loading')
          : agents.length === 0 ? t('agents:screen.empty.subtitle')
            : t('agents:screen.list.summary', { count: agents.length, active: activeCount })
      }
      actions={(
        <Button variant="primary" icon={<IconPlus size={14} />} onClick={() => openCreate()} disabled={atLimit || disconnected}>
          {t('agents:screen.list.newAgent')}
        </Button>
      )}
    />
  )

  if (loading) {
    return (
      <div className="flex h-full min-h-0 flex-col">
        {header}
        <div className="flex-1 overflow-y-auto px-6 py-4" aria-busy="true">
          <Skeleton className="mb-4 h-8 w-72" />
          <div className="rounded-lg border border-border-subtle">
            <SkeletonList rows={5} label={t('agents:screen.list.loading')} />
          </div>
        </div>
      </div>
    )
  }

  if (error && agents.length === 0) {
    return (
      <div className="flex h-full min-h-0 flex-col">
        {header}
        <div className="flex-1 overflow-y-auto px-6 py-4">
          <ErrorState
            title={t('agents:screen.list.errorTitle')}
            description={t('agents:screen.list.errorDescription')}
            onRetry={retry}
            retrying={retrying}
          />
        </div>
        {dialogs}
      </div>
    )
  }

  if (agents.length === 0) {
    return (
      <div className="flex h-full min-h-0 flex-col">
        {header}
        <div className="flex-1 overflow-y-auto px-6 py-8">
          <EmptyState
            icon={<IconBot size={20} />}
            title={t('agents:screen.empty.title')}
            description={t('agents:screen.empty.description')}
            action={(
              <Button variant="primary" icon={<IconPlus size={14} />} onClick={() => openCreate()} disabled={disconnected}>
                {t('agents:screen.empty.create')}
              </Button>
            )}
          />
          <section aria-label={t('agents:screen.empty.templates')} className="mx-auto mt-4 max-w-2xl">
            <h2 className="mb-3 text-center text-overline text-ink-muted">{t('agents:screen.empty.templates')}</h2>
            <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
              {AGENT_PRESETS.map((p) => {
                const r = resolvePreset(t, p)
                return (
                  <button
                    key={p.key}
                    type="button"
                    disabled={disconnected}
                    aria-label={t('agents:screen.empty.useTemplate', { name: r.label })}
                    onClick={() => openCreate(p.key)}
                    className="flex items-center gap-3 rounded-lg border border-border-subtle bg-surface-raised p-3 text-left transition-colors hover:bg-surface-hover disabled:pointer-events-none disabled:opacity-50"
                  >
                    <span aria-hidden="true" className="flex size-9 shrink-0 items-center justify-center rounded-md bg-accent-subtle text-body">{r.emoji}</span>
                    <span className="min-w-0 flex-1">
                      <span className="block truncate text-ui font-semibold text-ink">{r.displayName}</span>
                      <span className="line-clamp-2 text-caption font-normal text-ink-muted">{r.description.replace(/^Name:\s*[^.]+\.?\s*/, '')}</span>
                    </span>
                  </button>
                )
              })}
            </div>
          </section>
        </div>
        {dialogs}
      </div>
    )
  }

  const noMatch = (
    <EmptyState
      icon={<IconSearch size={20} />}
      title={t('agents:screen.list.noMatchTitle')}
      description={t('agents:screen.list.noMatchDescription')}
      action={(
        <Button onClick={() => { setQuery(''); setStatusFilter('all'); setModelFilter('all') }}>
          {t('agents:screen.list.clearFilters')}
        </Button>
      )}
    />
  )

  const bulkActions = (ids: string[]) => (
    <>
      <Button size="sm" disabled={disconnected} onClick={() => void handleBulkEnable(true)}>{t('agents:screen.bulk.enable')}</Button>
      <Button size="sm" disabled={disconnected} onClick={() => void handleBulkEnable(false)}>{t('agents:screen.bulk.disable')}</Button>
      <Button size="sm" variant="danger" disabled={disconnected} onClick={() => setBulkDeleteIds(ids)}>{t('agents:screen.bulk.delete')}</Button>
    </>
  )

  return (
    <div className="flex h-full min-h-0 flex-col">
      {header}
      <div className="flex shrink-0 flex-wrap items-center gap-2 px-6 pt-4">
        <div className="relative w-full max-w-72 flex-1">
          <IconSearch size={14} className="pointer-events-none absolute left-2.5 top-1/2 -translate-y-1/2 text-ink-muted" />
          <Input
            type="search"
            value={query}
            onChange={(e) => setQuery(e.target.value)}
            placeholder={t('agents:screen.list.search')}
            aria-label={t('agents:screen.list.search')}
            className="pl-8"
          />
        </div>
        <Select
          value={statusFilter}
          onChange={(e) => setStatusFilter(e.target.value as StatusFilter)}
          aria-label={t('agents:screen.list.statusFilter')}
          className="w-40"
          options={[
            { value: 'all', label: t('agents:screen.list.allStatuses') },
            { value: 'active', label: t('agents:screen.status.active') },
            { value: 'inactive', label: t('agents:screen.status.inactive') },
            { value: 'summoning', label: t('agents:screen.status.summoning') },
            { value: 'failed', label: t('agents:screen.status.failed') },
          ]}
        />
        <Select
          value={modelFilter}
          onChange={(e) => setModelFilter(e.target.value)}
          aria-label={t('agents:screen.list.modelFilter')}
          className="w-48"
          options={[{ value: 'all', label: t('agents:screen.list.allModels') }, ...modelOptions.map((m) => ({ value: m, label: m }))]}
        />
        <SegmentedControl<ViewMode>
          value={view}
          onValueChange={setView}
          aria-label={t('agents:screen.list.viewLabel')}
          className="ml-auto"
          options={[
            { value: 'list', label: t('agents:screen.list.viewList') },
            { value: 'grid', label: t('agents:screen.list.viewGrid') },
          ]}
        />
      </div>

      <div className={cn('min-h-0 flex-1 overflow-y-auto px-6 py-4', disconnected && 'opacity-60')}>
        {atLimit && <p className="mb-3 text-caption text-warning">{t('agents:screen.list.limitReached', { max: maxAgents })}</p>}
        {error && <p role="alert" className="mb-3 text-caption text-danger">{t('agents:screen.list.errorTitle')}</p>}
        {view === 'list' ? (
          <AgentTable
            agents={visible}
            selectedIds={selectedIds}
            onSelectionChange={setSelectedIds}
            bulkActions={bulkActions}
            readOnly={disconnected}
            empty={filtering ? noMatch : undefined}
            onOpen={(a) => setDetailId(a.id)}
            onChat={(a) => chatWithAgent(a.id)}
            onToggle={handleToggle}
            onResummon={handleResummon}
            onDelete={setDeletingAgent}
          />
        ) : visible.length === 0 ? (
          noMatch
        ) : (
          <div className="grid grid-cols-1 gap-3 sm:grid-cols-2 xl:grid-cols-3">
            {visible.map((a) => (
              <AgentCard
                key={a.id}
                agent={a}
                readOnly={disconnected}
                onOpen={(x) => setDetailId(x.id)}
                onChat={(x) => chatWithAgent(x.id)}
                onResummon={handleResummon}
                onDelete={setDeletingAgent}
              />
            ))}
          </div>
        )}
      </div>
      {dialogs}
    </div>
  )
}
