import { useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { wails, wailsRuntime, type UpdateInfo } from '../../lib/wails'
import { IconAlertTriangle, IconCheckCircle, IconDownload } from '../common/Icons'
import { Button, Spinner } from '../ui'

type BannerState = 'idle' | 'available' | 'downloading' | 'done' | 'error'

/** App update notice at the top of the main panel (driven by the backend "update:available" event). */
export function UpdateBanner() {
  const { t } = useTranslation('desktop')
  const [update, setUpdate] = useState<UpdateInfo | null>(null)
  const [state, setState] = useState<BannerState>('idle')
  const [dismissed, setDismissed] = useState(false)

  useEffect(() => wailsRuntime.eventsOn<UpdateInfo>('update:available', (info) => {
    if (info?.available) {
      setUpdate(info)
      setState('available')
      setDismissed(false)
    }
  }), [])

  const handleUpdate = async () => {
    if (!update) return
    setState('downloading')
    try {
      await wails.applyUpdate()
      setState('done')
    } catch (err) {
      console.error('[update] apply failed', err)
      setState('error')
    }
  }

  const handleRestart = () => {
    wails.restartApp().catch((err: unknown) => {
      console.error('[update] restart failed', err)
      setState('error')
    })
  }

  if (state === 'idle' || dismissed || !update) return null

  const later = (
    <Button size="sm" variant="secondary" onClick={() => setDismissed(true)}>
      {t('update.later')}
    </Button>
  )

  return (
    <div
      role={state === 'error' ? 'alert' : 'status'}
      className="flex shrink-0 items-center gap-3 border-b border-border-subtle bg-accent-subtle px-6 py-2.5 text-ui text-accent-subtle-ink"
    >
      {state === 'available' && (
        <>
          <IconDownload size={16} className="shrink-0" />
          <span className="min-w-0 flex-1 truncate">{t('update.available', { version: update.version })}</span>
          <Button size="sm" variant="primary" onClick={handleUpdate}>{t('update.now')}</Button>
          {later}
        </>
      )}
      {state === 'downloading' && (
        <>
          <Spinner size={14} />
          <span className="min-w-0 flex-1 truncate">{t('update.downloading')}</span>
        </>
      )}
      {state === 'done' && (
        <>
          <IconCheckCircle size={16} className="shrink-0" />
          <span className="min-w-0 flex-1 truncate">{t('update.ready')}</span>
          <Button size="sm" variant="primary" onClick={handleRestart}>{t('update.restart')}</Button>
          {later}
        </>
      )}
      {state === 'error' && (
        <>
          <IconAlertTriangle size={16} className="shrink-0 text-danger" />
          <span className="min-w-0 flex-1 truncate text-danger">{t('update.failed')}</span>
          <Button size="sm" variant="secondary" onClick={handleUpdate}>{t('update.retry')}</Button>
          {later}
        </>
      )}
    </div>
  )
}
