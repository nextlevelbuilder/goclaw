import { useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { useProviders } from '../../hooks/use-providers'
import { useProviderCapabilities } from '../../hooks/use-provider-capabilities'
import { useGatewayStatus } from '../layout/use-gateway-status'
import { cn } from '../../lib/cn'
import { ApiError } from '../../lib/api'
import { IconActivity, IconGear, IconPlus, IconTrash } from '../common/Icons'
import {
  Button, ConfirmDialog, DataTable, EmptyState, ErrorState, Menu, PageHeader, SkeletonList, Switch,
  type DataTableColumn,
} from '../ui'
import { ProviderNameCell } from './ProviderNameCell'
import { ProviderFormDialog } from './ProviderFormDialog'
import { ProviderDetail } from './ProviderDetail'
import { ProviderStatusChip } from './provider-usage-widgets'
import { providerStatus } from './provider-usage-format'
import type { ProviderCapability, ProviderData, ProviderInput } from '../../types/provider'

function modelNames(p: ProviderData): string[] {
  return (p.models ?? []).map((m) => m.label || m.id.split('/').slice(1).join('/') || m.id)
}

export function ProviderList() {
  const { t } = useTranslation(['providers', 'common'])
  const { providers, loading, error, fetchProviders, createProvider, updateProvider, deleteProvider } = useProviders()
  const { capabilities } = useProviderCapabilities()
  const { disconnected } = useGatewayStatus()
  const [formOpen, setFormOpen] = useState(false)
  const [editingProvider, setEditingProvider] = useState<ProviderData | null>(null)
  const [deletingProvider, setDeletingProvider] = useState<ProviderData | null>(null)
  const [deleting, setDeleting] = useState(false)
  // Id only, so the detail view always renders the latest row after an edit.
  const [selectedId, setSelectedId] = useState<string | null>(null)

  // `/v1/providers` carries the CRUD fields; capabilities add wire_api / auth_kind /
  // model_source / catalogue. Merge by provider_id (uuid) or provider name.
  const rows = useMemo(() => {
    const byKey = new Map<string, ProviderCapability>()
    for (const c of capabilities) {
      byKey.set(c.id, c)
      if (c.provider_id) byKey.set(c.provider_id, c)
    }
    return providers.map((p) => {
      const cap = byKey.get(p.id) ?? byKey.get(p.name)
      if (!cap) return p
      return {
        ...p,
        wire_api: p.wire_api ?? cap.wire_api,
        auth_kind: p.auth_kind ?? cap.auth_kind,
        model_source: p.model_source ?? cap.model_source,
        default_model_id: p.default_model_id ?? cap.default_model_id,
        models: p.models ?? cap.models,
      }
    })
  }, [providers, capabilities])

  const openForm = (provider: ProviderData | null) => {
    setEditingProvider(provider)
    setFormOpen(true)
  }

  const handleSubmit = async (input: ProviderInput) => {
    if (editingProvider) await updateProvider(editingProvider.id, input)
    else await createProvider(input)
  }

  const handleConfirmDelete = async () => {
    if (!deletingProvider) return
    setDeleting(true)
    try {
      await deleteProvider(deletingProvider.id)
      if (selectedId === deletingProvider.id) setSelectedId(null)
      setDeletingProvider(null)
    } catch {
      // The hook already toasted the failure; keep the dialog open so the user can retry.
    } finally {
      setDeleting(false)
    }
  }

  const selected = selectedId ? rows.find((p) => p.id === selectedId) ?? null : null
  const displayName = (p: ProviderData) => p.display_name || p.name

  const columns: DataTableColumn<ProviderData>[] = [
    {
      id: 'name',
      header: t('providers:columns.name'),
      cell: (p) => <ProviderNameCell provider={p} onOpen={(provider) => setSelectedId(provider.id)} />,
    },
    {
      id: 'status',
      header: t('providers:columns.status'),
      className: 'w-40',
      cell: (p) => <ProviderStatusChip status={providerStatus(p.enabled, null)} />,
    },
    {
      id: 'models',
      header: t('providers:columns.models'),
      cell: (p) => {
        const names = modelNames(p)
        if (names.length === 0) return <span className="text-caption text-ink-muted">{t('providers:providerDetail.notSet')}</span>
        return (
          <span className="block max-w-[260px] truncate text-code text-ink-secondary" title={names.join(', ')}>
            {names.slice(0, 2).join(', ')}{names.length > 2 ? ` +${names.length - 2}` : ''}
          </span>
        )
      },
    },
    {
      id: 'enabled',
      header: t('providers:columns.enabled'),
      className: 'w-24',
      cell: (p) => (
        <span onClick={(e) => e.stopPropagation()} className="inline-flex">
          <Switch
            checked={p.enabled}
            disabled={disconnected}
            aria-label={t('providers:enableToggle', { name: displayName(p) })}
            onCheckedChange={(enabled) => { void updateProvider(p.id, { enabled }, true).catch(() => {}) }}
          />
        </span>
      ),
    },
    {
      id: 'actions',
      header: <span className="sr-only">{t('providers:columns.actions')}</span>,
      className: 'w-12 text-right',
      cell: (p) => (
        <span onClick={(e) => e.stopPropagation()} className="inline-flex">
          <Menu
            label={t('providers:rowMenu', { name: displayName(p) })}
            items={[
              { label: t('providers:providerDetail.edit'), icon: <IconGear size={14} />, onSelect: () => openForm(p), disabled: disconnected },
              'separator',
              { label: t('providers:delete.confirmLabel'), icon: <IconTrash size={14} />, destructive: true, onSelect: () => setDeletingProvider(p), disabled: disconnected },
            ]}
          />
        </span>
      ),
    },
  ]

  let body: React.ReactNode
  if (loading) {
    body = <SkeletonList rows={4} label={t('common:state.loading')} className="rounded-lg border border-border-subtle" />
  } else if (error && providers.length === 0) {
    body = (
      <ErrorState
        title={t('providers:states.errorTitle')}
        description={t('providers:states.errorBody')}
        code={error instanceof ApiError ? String(error.status) : undefined}
        onRetry={() => { void fetchProviders() }}
      />
    )
  } else if (rows.length === 0) {
    body = (
      <EmptyState
        icon={<IconActivity size={20} />}
        title={t('providers:emptyTitle')}
        description={t('providers:emptyDescription')}
        action={<Button variant="primary" icon={<IconPlus size={16} />} onClick={() => openForm(null)} disabled={disconnected}>{t('providers:addProvider')}</Button>}
      />
    )
  } else {
    body = (
      <DataTable
        aria-label={t('providers:title')}
        columns={columns}
        rows={rows}
        getRowId={(p) => p.id}
        onRowClick={(p) => setSelectedId(p.id)}
      />
    )
  }

  const dialogs = (
    <>
      <ProviderFormDialog open={formOpen} onOpenChange={setFormOpen} provider={editingProvider} onSubmit={handleSubmit} />
      <ConfirmDialog
        open={!!deletingProvider}
        onOpenChange={(open) => { if (!open) setDeletingProvider(null) }}
        title={t('providers:delete.title', { name: deletingProvider ? displayName(deletingProvider) : '' })}
        description={t('providers:delete.description', { name: deletingProvider ? displayName(deletingProvider) : '' })}
        confirmLabel={t('providers:delete.confirmLabel')}
        variant="destructive"
        loading={deleting}
        onConfirm={handleConfirmDelete}
      />
    </>
  )

  if (selected) {
    return (
      <>
        <ProviderDetail key={selected.id} provider={selected} onBack={() => setSelectedId(null)} onEdit={openForm} />
        {dialogs}
      </>
    )
  }

  const enabledCount = rows.filter((p) => p.enabled).length
  return (
    <div className="flex min-h-0 flex-1 flex-col">
      <PageHeader
        title={t('providers:title')}
        description={rows.length > 0 ? t('providers:summary', { count: rows.length, active: enabledCount }) : t('providers:description')}
        actions={rows.length > 0 && (
          <Button variant="primary" icon={<IconPlus size={16} />} onClick={() => openForm(null)} disabled={disconnected}>
            {t('providers:addProvider')}
          </Button>
        )}
      />
      <div className={cn('min-h-0 flex-1 overflow-y-auto overscroll-contain px-6 py-4', disconnected && 'opacity-60')}>
        {body}
      </div>
      {dialogs}
    </div>
  )
}
