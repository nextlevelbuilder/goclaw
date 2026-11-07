import { useTranslation } from 'react-i18next'
import { useUiStore } from '../../stores/ui-store'
import { IconBarChart } from '../common/Icons'
import { Button, EmptyState, PageHeader } from '../ui'

/** Interim Usage view; per-provider usage lives on each provider's Usage tab. */
export function UsageView() {
  const { t } = useTranslation('desktop')
  const setActiveView = useUiStore((s) => s.setActiveView)

  return (
    <div className="flex min-h-0 flex-1 flex-col">
      <PageHeader title={t('nav.views.usage')} description={t('stub.usageDesc')} />
      <div className="min-h-0 flex-1 overflow-y-auto overscroll-contain px-6 py-4">
        <EmptyState
          icon={<IconBarChart size={20} />}
          title={t('nav.views.usage')}
          description={t('stub.usageEmpty')}
          action={<Button variant="primary" onClick={() => setActiveView('providers')}>{t('stub.openProviders')}</Button>}
        />
      </div>
    </div>
  )
}
