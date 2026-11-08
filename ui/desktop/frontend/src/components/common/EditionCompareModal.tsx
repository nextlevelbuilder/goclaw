import { useTranslation } from 'react-i18next'
import { Dialog } from '../ui'

interface EditionCompareModalProps {
  open: boolean
  onClose: () => void
}

type FeatureVal = true | false | string

interface FeatureRow {
  key: string
  lite: FeatureVal
  standard: FeatureVal
}

interface FeatureGroup {
  group: string
  rows: FeatureRow[]
}

const FEATURES: FeatureGroup[] = [
  {
    group: 'limits',
    rows: [
      { key: 'agents', lite: 'Max 5', standard: true },
      { key: 'teams', lite: 'Max 1', standard: true },
      { key: 'teamMembers', lite: 'Max 5', standard: true },
      { key: 'sessions', lite: 'Max 50', standard: true },
      { key: 'channels', lite: '1 Telegram + 1 Discord', standard: true },
    ],
  },
  {
    group: 'core',
    rows: [
      { key: 'chat', lite: true, standard: true },
      { key: 'tools', lite: true, standard: true },
      { key: 'mcpServers', lite: true, standard: true },
      { key: 'skills', lite: true, standard: true },
      { key: 'memory', lite: 'FTS5', standard: 'pgvector' },
      { key: 'cron', lite: true, standard: true },
      { key: 'traces', lite: 'Compact', standard: 'Full' },
    ],
  },
  {
    group: 'standardOnly',
    rows: [
      { key: 'taskActions', lite: 'Core lifecycle', standard: 'Full + review/approve' },
      { key: 'heartbeat', lite: false, standard: true },
      { key: 'storage', lite: false, standard: true },
      { key: 'skillManage', lite: false, standard: true },
      { key: 'knowledgeGraph', lite: false, standard: true },
      { key: 'vectorSearch', lite: false, standard: true },
      { key: 'rbac', lite: false, standard: true },
      { key: 'multiTenant', lite: false, standard: true },
      { key: 'tenantUsers', lite: false, standard: true },
      { key: 'agentLinks', lite: false, standard: true },
      { key: 'activityLogs', lite: false, standard: true },
      { key: 'apiKeys', lite: false, standard: true },
      { key: 'poolProvider', lite: false, standard: true },
      { key: 'secureCli', lite: false, standard: true },
    ],
  },
]

function FeatureCell({ value }: { value: FeatureVal }) {
  if (value === true) {
    return (
      <span className="inline-flex items-center justify-center w-5 h-5 rounded-full bg-success-subtle">
        <svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth={3} strokeLinecap="round" strokeLinejoin="round" className="text-success">
          <polyline points="20 6 9 17 4 12" />
        </svg>
      </span>
    )
  }
  if (value === false) {
    return <span className="text-ink-muted">—</span>
  }
  return <span className="text-ink-secondary">{value}</span>
}

export function EditionCompareModal({ open, onClose }: EditionCompareModalProps) {
  const { t } = useTranslation('teams')

  return (
    <Dialog
      open={open}
      onOpenChange={(next) => { if (!next) onClose() }}
      title={t('editionCompare', 'GoClaw Lite vs Standard')}
      description={t('editionUpgrade', 'Upgrade to Standard with PostgreSQL for full features')}
      size="md"
    >
      <div className="max-h-[60vh] overflow-y-auto overscroll-contain">
        <div className="mb-2 grid grid-cols-[1fr_90px_90px] gap-2 px-1">
          <span className="text-caption font-medium text-ink-muted">{t('feature', 'Feature')}</span>
          <span className="text-center text-caption font-semibold text-accent">Lite</span>
          <span className="text-center text-caption font-medium text-ink-muted">Standard</span>
        </div>

        {FEATURES.map((group) => (
          <div key={group.group} className="mb-3">
            <div className="mb-1 rounded-sm bg-surface-hover px-1 py-1.5 text-overline uppercase text-ink-muted">
              {t(`edition.group.${group.group}`, group.group)}
            </div>
            {group.rows.map((row) => (
              <div key={row.key} className="grid grid-cols-[1fr_90px_90px] gap-2 border-b border-border-subtle px-1 py-1.5">
                <span className="text-ui text-ink">{t(`edition.${row.key}`, row.key)}</span>
                <div className="text-center text-caption"><FeatureCell value={row.lite} /></div>
                <div className="text-center text-caption"><FeatureCell value={row.standard} /></div>
              </div>
            ))}
          </div>
        ))}
      </div>
    </Dialog>
  )
}
