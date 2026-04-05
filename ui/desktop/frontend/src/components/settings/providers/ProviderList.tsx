import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { useProviders } from '../../../hooks/use-providers'
import { ProviderRow } from './ProviderRow'
import { ProviderFormDialog } from './ProviderFormDialog'
import { ConfirmDialog } from '../../common/ConfirmDialog'
import type { ProviderData, ProviderInput } from '../../../types/provider'

export function ProviderList() {
  const { t } = useTranslation(['providers', 'common'])
  const { providers, loading, createProvider, updateProvider, deleteProvider } = useProviders()
  const [formOpen, setFormOpen] = useState(false)
  const [editingProvider, setEditingProvider] = useState<ProviderData | null>(null)
  const [deletingProvider, setDeletingProvider] = useState<ProviderData | null>(null)

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
      setDeletingProvider(null)
    }
  }

  if (loading) {
    return <p className="text-xs text-text-muted py-4">{t('common:loading')}</p>
  }

  return (
    <>
      <div className="space-y-3">
        <div className="flex items-center justify-between">
          <h3 className="text-sm font-semibold text-text-primary">{t('providers:title')}</h3>
          <button
            onClick={handleCreate}
            className="px-3 py-1.5 text-xs bg-accent text-white rounded-lg font-medium hover:bg-accent-hover transition-colors"
          >
            + {t('providers:addProvider')}
          </button>
        </div>

        {providers.length === 0 ? (
          <p className="text-xs text-text-muted py-4 text-center">{t('providers:emptyTitle')}</p>
        ) : (
          <div className="space-y-1.5">
            {providers.map((p) => (
              <ProviderRow key={p.id} provider={p} onEdit={handleEdit} onDelete={setDeletingProvider} />
            ))}
          </div>
        )}
      </div>

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
