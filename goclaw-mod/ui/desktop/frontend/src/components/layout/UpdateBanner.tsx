import { useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { EventsOn } from '../../../wailsjs/runtime/runtime'
import { wails, type UpdateInfo } from '../../lib/wails'

type BannerState = 'idle' | 'available' | 'downloading' | 'done' | 'error'

export function UpdateBanner() {
  const { t } = useTranslation('desktop')
  const [update, setUpdate] = useState<UpdateInfo | null>(null)
  const [state, setState] = useState<BannerState>('idle')
  const [dismissed, setDismissed] = useState(false)

  useEffect(() => {
    const cancel = EventsOn('update:available', (info: UpdateInfo) => {
      if (info?.available) {
        setUpdate(info)
        setState('available')
        setDismissed(false)
      }
    })
    return cancel
  }, [])

  const handleUpdate = async () => {
    if (!update) return
    setState('downloading')
    try {
      await wails.applyUpdate()
      setState('done')
    } catch {
      setState('error')
    }
  }

  const handleRestart = () => {
    wails.restartApp()
  }

  if (state === 'idle' || dismissed || !update) return null

  return (
    <div className="flex items-center gap-3 px-4 py-2 bg-accent/10 border-b border-accent/20 text-xs shrink-0">
      {state === 'available' && (
        <>
          <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth={2} strokeLinecap="round" strokeLinejoin="round" className="text-accent shrink-0">
            <path d="M21 15v4a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2v-4" />
            <polyline points="7 10 12 15 17 10" />
            <line x1="12" y1="15" x2="12" y2="3" />
          </svg>
          <span className="text-text-primary">
            {t('update.available', 'GoClaw Lite v{{version}} is available', { version: update.version })}
          </span>
          <button
            onClick={handleUpdate}
            className="font-medium text-accent bg-accent/10 hover:bg-accent/20 px-3 py-1 rounded-md cursor-pointer transition-colors"
          >
            {t('update.now', 'Update Now')}
          </button>
          <div className="flex-1" />
          <button onClick={() => setDismissed(true)} className="text-text-muted hover:text-text-primary cursor-pointer">
            <svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth={2} strokeLinecap="round" strokeLinejoin="round">
              <line x1="18" y1="6" x2="6" y2="18" /><line x1="6" y1="6" x2="18" y2="18" />
            </svg>
          </button>
        </>
      )}

      {state === 'downloading' && (
        <>
          <div className="w-3.5 h-3.5 border-2 border-accent border-t-transparent rounded-full animate-spin shrink-0" />
          <span className="text-text-primary">{t('update.downloading', 'Downloading update...')}</span>
        </>
      )}

      {state === 'done' && (
        <>
          <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth={2} strokeLinecap="round" strokeLinejoin="round" className="text-emerald-500 shrink-0">
            <path d="M22 11.08V12a10 10 0 1 1-5.93-9.14" /><polyline points="22 4 12 14.01 9 11.01" />
          </svg>
          <span className="text-text-primary">{t('update.ready', 'Update installed! Restart to apply.')}</span>
          <button
            onClick={handleRestart}
            className="font-medium text-accent bg-accent/10 hover:bg-accent/20 px-3 py-1 rounded-md cursor-pointer transition-colors"
          >
            {t('update.restart', 'Restart Now')}
          </button>
        </>
      )}

      {state === 'error' && (
        <>
          <span className="text-error">{t('update.failed', 'Update failed.')}</span>
          <button onClick={handleUpdate} className="font-medium text-accent bg-accent/10 hover:bg-accent/20 px-3 py-1 rounded-md cursor-pointer transition-colors">
            {t('update.retry', 'Retry')}
          </button>
          <div className="flex-1" />
          <button onClick={() => setDismissed(true)} className="text-text-muted hover:text-text-primary cursor-pointer">
            <svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth={2} strokeLinecap="round" strokeLinejoin="round">
              <line x1="18" y1="6" x2="6" y2="18" /><line x1="6" y1="6" x2="18" y2="18" />
            </svg>
          </button>
        </>
      )}
    </div>
  )
}
