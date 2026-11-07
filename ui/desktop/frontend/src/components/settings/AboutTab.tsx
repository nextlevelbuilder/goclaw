import { useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { wails, wailsRuntime, type UpdateInfo } from '../../lib/wails'
import { IconDownload } from '../common/Icons'
import { Button, Spinner, StatusChip } from '../ui'
import { SettingsCard, SettingsRow } from './settings-row'

type UpdateState =
  | { kind: 'idle' }
  | { kind: 'checking' }
  | { kind: 'current' }
  | { kind: 'available'; info: UpdateInfo }
  | { kind: 'downloading' }
  | { kind: 'ready' }
  | { kind: 'checkFailed' }
  | { kind: 'updateFailed'; info: UpdateInfo }

const LIMIT_KEYS = ['agents', 'teams', 'teamMembers', 'database', 'users'] as const

export function AboutTab() {
  const { t } = useTranslation('settings')
  const [version, setVersion] = useState('…')
  const [dataDir, setDataDir] = useState('')
  const [update, setUpdate] = useState<UpdateState>({ kind: 'idle' })
  const [copied, setCopied] = useState(false)

  useEffect(() => {
    wails.getVersion().then(setVersion).catch(() => setVersion('unknown'))
    wails.getDataDir().then(setDataDir).catch(() => {})
  }, [])

  const check = async () => {
    setUpdate({ kind: 'checking' })
    try {
      const info = await wails.checkForUpdate()
      setUpdate(info?.available ? { kind: 'available', info } : { kind: 'current' })
    } catch {
      setUpdate({ kind: 'checkFailed' })
    }
  }

  const apply = async (info: UpdateInfo) => {
    setUpdate({ kind: 'downloading' })
    try {
      await wails.applyUpdate()
      setUpdate({ kind: 'ready' })
    } catch {
      setUpdate({ kind: 'updateFailed', info })
    }
  }

  const copyDetails = async () => {
    const text = [`GoClaw Desktop ${version}`, t('about.editionValue'), navigator.userAgent, dataDir].filter(Boolean).join('\n')
    try {
      await navigator.clipboard.writeText(text)
      setCopied(true)
      window.setTimeout(() => setCopied(false), 2000)
    } catch {
      setCopied(false)
    }
  }

  return (
    <>
      <div className="flex items-center gap-4">
        <span aria-hidden="true" className="flex size-12 items-center justify-center rounded-xl bg-accent text-title font-bold text-on-accent">G</span>
        <div className="min-w-0 flex-1">
          <h2 className="text-title text-ink">{t('about.title')}</h2>
          <p className="text-caption text-ink-muted">{t('about.subtitle', { version })}</p>
        </div>
        {update.kind === 'available' && <StatusChip tone="info" label={t('about.available', { version: update.info.version })} />}
      </div>

      <SettingsCard title={t('about.updatesTitle')}>
        <SettingsRow label={t('about.updatesTitle')} hint={updateHint(update, t)}>
          {update.kind === 'available' || update.kind === 'updateFailed' ? (
            <Button variant="primary" icon={<IconDownload size={14} />} onClick={() => { void apply(update.info) }}>{t('about.updateNow')}</Button>
          ) : update.kind === 'ready' ? (
            <Button variant="primary" onClick={() => { void wails.restartApp().catch(() => setUpdate({ kind: 'checkFailed' })) }}>{t('about.restart')}</Button>
          ) : update.kind === 'downloading' ? (
            <Spinner size={16} />
          ) : (
            <Button loading={update.kind === 'checking'} onClick={() => { void check() }}>
              {update.kind === 'checking' ? t('about.checking') : t('about.check')}
            </Button>
          )}
        </SettingsRow>
      </SettingsCard>

      <SettingsCard title={t('about.detailsTitle')}>
        <SettingsRow label={t('about.version')}><span className="text-code text-ink-secondary">{version}</span></SettingsRow>
        <SettingsRow label={t('about.edition')}><span className="text-ui text-ink-secondary">{t('about.editionValue')}</span></SettingsRow>
        <SettingsRow label={t('about.runtime')}><span className="text-code text-ink-secondary">{t('about.runtimeValue')}</span></SettingsRow>
        {dataDir && (
          <SettingsRow label={t('about.dataDir')}>
            <button
              type="button"
              onClick={() => { void wails.openFile(dataDir).catch(() => {}) }}
              title={dataDir}
              className="max-w-[260px] truncate text-code text-accent hover:underline"
            >
              {dataDir}
            </button>
          </SettingsRow>
        )}
      </SettingsCard>

      <SettingsCard title={t('about.diagnosticsTitle')}>
        <SettingsRow label={t('about.diagnosticsTitle')} hint={t('about.diagnosticsHint')}>
          <Button onClick={() => { void copyDetails() }}>{copied ? t('about.copied') : t('about.copy')}</Button>
        </SettingsRow>
      </SettingsCard>

      <SettingsCard title={t('about.limitsTitle')}>
        {LIMIT_KEYS.map((key) => (
          <SettingsRow key={key} label={t(`about.limits.${key}`)}>
            <span className="text-ui text-ink-secondary">{t(`about.limits.${key}Value`)}</span>
          </SettingsRow>
        ))}
      </SettingsCard>

      <p className="text-caption text-ink-muted">
        <button type="button" onClick={() => wailsRuntime.browserOpenURL('https://github.com/nextlevelbuilder/goclaw')} className="text-accent hover:underline">
          {t('about.github')}
        </button>
        <span className="mx-2" aria-hidden="true">·</span>
        {t('about.builtWith')}
      </p>
    </>
  )
}

function updateHint(update: UpdateState, t: (key: string, opts?: Record<string, unknown>) => string): string {
  switch (update.kind) {
    case 'current': return t('about.upToDate')
    case 'available': return t('about.available', { version: update.info.version })
    case 'downloading': return t('about.downloading')
    case 'ready': return t('about.ready')
    case 'checkFailed': return t('about.checkFailed')
    case 'updateFailed': return t('about.updateFailed')
    default: return t('about.updatesHint')
  }
}
