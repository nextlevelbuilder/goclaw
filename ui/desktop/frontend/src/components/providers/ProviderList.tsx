import { useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { useProviders } from '../../hooks/use-providers'
import { useProviderCapabilities } from '../../hooks/use-provider-capabilities'
import { ProviderRow } from './ProviderRow'
import { ProviderFormDialog } from './ProviderFormDialog'
import { ProviderDetail } from './ProviderDetail'
import { ConfirmDialog } from '../common/ConfirmDialog'
import type { ProviderCapability, ProviderData, ProviderInput } from '../../types/provider'

export function ProviderList() {
  const { t } = useTranslation(['providers', 'common'])
  const { providers, loading, createProvider, updateProvider, deleteProvider } = useProviders()
  const { capabilities } = useProviderCapabilities()
  const [formOpen, setFormOpen] = useState(false)
  const [editingProvider, setEditingProvider] = useState<ProviderData | null>(null)
  const [deletingProvider, setDeletingProvider] = useState<ProviderData | null>(null)
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

  const handleEdit = (provider: ProviderData) => {
    setEditingProvider(provider)
    setFormOpen(true)
  }

  const handleCreate = () => {
    setEditingProvider(null)
    setFormOpen(true)
  }

  const handleSubmit = async (input: ProviderInput) => {
    if (editingProvider) {
      await updateProvider(editingProvider.id, input)
    } else {
      await createProvider(input)
    }
  }

  const handleConfirmDelete = async () => {
    if (deletingProvider) {
      await deleteProvider(deletingProvider.id)
      if (selectedId === deletingProvider.id) setSelectedId(null)
      setDeletingProvider(null)
    }
  }

  if (loading) {
    return <p className="text-xs text-text-muted py-4">{t('common:loading')}</p>
  }

  const selected = selectedId ? rows.find((p) => p.id === selectedId) ?? null : null

  return (
    <>
      {selected ? (
        <ProviderDetail key={selected.id} provider={selected} onBack={() => setSelectedId(null)} onEdit={handleEdit} />
      ) : (
        <div className="space-y-3">
          <div className="flex items-center justify-between">
            <h3 className="text-sm font-semibold text-text-primary">{t('providers:title')}</h3>
            <button
              onClick={handleCreate}
              className="px-3 py-1.5 text-xs bg-accent text-accent-foreground rounded-lg font-medium hover:bg-accent-hover transition-colors"
            >
              + {t('providers:addProvider')}
            </button>
          </div>

          {rows.length === 0 ? (
            <p className="text-xs text-text-muted py-4 text-center">{t('providers:emptyTitle')}</p>
          ) : (
            <div className="space-y-1.5">
              {rows.map((p) => (
                <ProviderRow
                  key={p.id}
                  provider={p}
                  onOpen={(provider) => setSelectedId(provider.id)}
                  onEdit={handleEdit}
                  onDelete={setDeletingProvider}
                />
              ))}
            </div>
          )}
        </div>
      )}

      <ProviderFormDialog
        open={formOpen}
        onOpenChange={setFormOpen}
        provider={editingProvider}
        onSubmit={handleSubmit}
      />

      <ConfirmDialog
        open={!!deletingProvider}
        onOpenChange={(open) => { if (!open) setDeletingProvider(null) }}
        title="Delete provider?"
        description={`This will remove "${deletingProvider?.display_name || deletingProvider?.name}". Agents using this provider will stop working.`}
        confirmLabel="Delete"
        variant="destructive"
        onConfirm={handleConfirmDelete}
      />
    </>
  )
}
