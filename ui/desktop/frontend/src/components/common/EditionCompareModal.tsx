import { useTranslation } from 'react-i18next'

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
      <span className="inline-flex items-center justify-center w-5 h-5 rounded-full bg-emerald-500/15">
        <svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth={3} strokeLinecap="round" strokeLinejoin="round" className="text-emerald-500">
          <polyline points="20 6 9 17 4 12" />
        </svg>
      </span>
    )
  }
  if (value === false) {
    return <span className="text-text-muted">—</span>
  }
  return <span className="text-text-secondary">{value}</span>
}

export function EditionCompareModal({ open, onClose }: EditionCompareModalProps) {
  const { t } = useTranslation('teams')

  if (!open) return null

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/50" onClick={onClose}>
      <div
        onClick={(e) => e.stopPropagation()}
        className="bg-surface-primary border border-border rounded-xl shadow-xl w-full max-w-lg mx-4 max-h-[80vh] flex flex-col"
      >
        {/* Header */}
        <div className="flex items-center justify-between p-4 border-b border-border shrink-0">
          <div>
            <h3 className="text-sm font-semibold text-text-primary">
              {t('editionCompare', 'GoClaw Lite vs Standard')}
            </h3>
            <p className="text-[10px] text-text-muted mt-0.5">
              {t('editionUpgrade', 'Upgrade to Standard with PostgreSQL for full features')}
            </p>
          </div>
          <button onClick={onClose} className="text-text-muted hover:text-text-primary cursor-pointer p-1">
            <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth={2} strokeLinecap="round" strokeLinejoin="round">
              <line x1="18" y1="6" x2="6" y2="18" /><line x1="6" y1="6" x2="18" y2="18" />
            </svg>
          </button>
        </div>

        {/* Scrollable content */}
        <div className="overflow-y-auto overscroll-contain flex-1 px-4 py-3">
          {/* Column headers */}
          <div className="grid grid-cols-[1fr_90px_90px] gap-2 mb-2 px-1">
            <span className="text-[10px] text-text-muted font-medium">{t('feature', 'Feature')}</span>
            <span className="text-[10px] text-accent font-semibold text-center">Lite</span>
            <span className="text-[10px] text-text-muted font-medium text-center">Standard</span>
          </div>

          {FEATURES.map((group) => (
            <div key={group.group} className="mb-3">
              <div className="text-[9px] uppercase tracking-wider text-text-muted font-semibold px-1 py-1.5 bg-surface-tertiary/50 rounded mb-1">
                {t(`edition.group.${group.group}`, group.group)}
              </div>
              {group.rows.map((row) => (
                <div key={row.key} className="grid grid-cols-[1fr_90px_90px] gap-2 px-1 py-1.5 border-b border-border/30">
                  <span className="text-xs text-text-primary">{t(`edition.${row.key}`, row.key)}</span>
                  <div className="text-[11px] text-center"><FeatureCell value={row.lite} /></div>
                  <div className="text-[11px] text-center"><FeatureCell value={row.standard} /></div>
                </div>
              ))}
            </div>
          ))}
        </div>
      </div>
    </div>
  )
}
