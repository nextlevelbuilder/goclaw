import { useCallback, useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { wails } from '../../lib/wails'
import { useGatewayStatus } from '../layout/use-gateway-status'
import { Button, ErrorState, Skeleton, StatusChip, type StatusTone } from '../ui'
import { SettingsCard, SettingsRow } from './settings-row'

interface GatewayInfo {
  url: string
  port: number
  dataDir: string
}

export function GatewayTab() {
  const { t } = useTranslation('settings')
  const { status } = useGatewayStatus()
  const [info, setInfo] = useState<GatewayInfo | null>(null)
  const [failed, setFailed] = useState(false)
  const [loading, setLoading] = useState(true)

  const load = useCallback(async () => {
    setLoading(true)
    setFailed(false)
    try {
      const [url, port, dataDir] = await Promise.all([wails.getGatewayURL(), wails.getGatewayPort(), wails.getDataDir()])
      setInfo({ url, port, dataDir })
    } catch {
      setFailed(true)
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => { void load() }, [load])

  const tone: StatusTone = status === 'online' ? 'success' : status === 'connecting' ? 'info' : 'danger'

  if (failed) {
    return <ErrorState title={t('gateway.errorTitle')} description={t('gateway.errorBody')} onRetry={() => { void load() }} retrying={loading} />
  }

  return (
    <SettingsCard title={t('gateway.title')}>
      <SettingsRow label={t('gateway.status')} hint={t('gateway.statusHint')}>
        <StatusChip tone={tone} label={t(`gateway.${status}`)} />
      </SettingsRow>
      {loading || !info ? (
        <div role="status" aria-busy="true" className="flex flex-col gap-4 py-4">
          <Skeleton className="h-4 w-2/3" />
          <Skeleton className="h-4 w-1/2" />
          <Skeleton className="h-4 w-3/4" />
        </div>
      ) : (
        <>
          <SettingsRow label={t('gateway.url')} hint={t('gateway.urlHint')}>
            <span className="text-code text-ink-secondary">{info.url}</span>
          </SettingsRow>
          <SettingsRow label={t('gateway.port')} hint={t('gateway.portHint')}>
            <span className="text-code text-ink-secondary">{info.port}</span>
          </SettingsRow>
          <SettingsRow label={t('gateway.dataDir')} hint={t('gateway.dataDirHint')}>
            <span className="max-w-[200px] truncate text-code text-ink-secondary" title={info.dataDir}>{info.dataDir}</span>
            <Button onClick={() => { void wails.openFile(info.dataDir).catch(() => {}) }}>{t('gateway.openFolder')}</Button>
          </SettingsRow>
        </>
      )}
    </SettingsCard>
  )
}
