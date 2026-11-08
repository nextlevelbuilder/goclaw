import { useEffect, useState, type MouseEvent } from 'react'
import { useTranslation } from 'react-i18next'
import { cn } from '../../lib/cn'
import { wailsRuntime } from '../../lib/wails'
import { useUiStore } from '../../stores/ui-store'
import { IconClose, IconMinus, IconRestore, IconSearch, IconSquare } from '../common/Icons'
import { viewLabelKey } from './view-registry'

/** Track the maximised state so the middle button can show "Restore". */
function useMaximised(): boolean {
  const [maximised, setMaximised] = useState(false)
  useEffect(() => {
    let alive = true
    const check = () => { void wailsRuntime.isMaximised().then((m) => { if (alive) setMaximised(m) }) }
    check()
    window.addEventListener('resize', check)
    return () => { alive = false; window.removeEventListener('resize', check) }
  }, [])
  return maximised
}

const windowButton = 'wails-no-drag flex h-full w-[46px] items-center justify-center text-ink-secondary transition-colors'

export interface TitlebarProps {
  /**
   * Minimal bar for screens outside the shell (splash, onboarding): app mark
   * and window controls only, no breadcrumb or palette trigger.
   */
  minimal?: boolean
  className?: string
}

/**
 * Frameless window title bar: app mark and breadcrumb, the command palette
 * trigger in the centre and the window controls on the right. Everything but
 * the controls is a drag region; double-click toggles maximise.
 */
export function Titlebar({ minimal = false, className }: TitlebarProps) {
  const { t } = useTranslation('desktop')
  const activeView = useUiStore((s) => s.activeView)
  const openPalette = useUiStore((s) => s.setCommandPaletteOpen)
  const maximised = useMaximised()

  const onDoubleClick = (e: MouseEvent<HTMLElement>) => {
    if ((e.target as HTMLElement).closest('.wails-no-drag')) return
    wailsRuntime.toggleMaximise()
  }

  return (
    <header
      onDoubleClick={onDoubleClick}
      className={cn('wails-drag flex h-titlebar shrink-0 select-none items-center bg-surface-window text-ink', className)}
    >
      <nav aria-label={t('titlebar.breadcrumb')} className="flex min-w-0 flex-1 items-center gap-2 pl-4 text-caption font-medium text-ink-secondary">
        <span aria-hidden="true" className="flex size-[18px] shrink-0 items-center justify-center rounded-[5px] bg-accent text-[10px] font-bold text-on-accent">
          G
        </span>
        <span>GoClaw</span>
        {!minimal && (
          <>
            <span aria-hidden="true" className="text-ink-muted">/</span>
            <span aria-current="page" className="truncate">{t(viewLabelKey(activeView))}</span>
          </>
        )}
      </nav>

      {!minimal && <button
        type="button"
        onClick={() => openPalette(true)}
        aria-keyshortcuts="Control+K"
        className="wails-no-drag flex h-[26px] w-[300px] max-w-[40vw] shrink items-center gap-2 rounded-sm border border-border-subtle bg-surface-base px-2.5 text-caption text-ink-muted transition-colors hover:border-border-control"
      >
        <IconSearch size={13} className="shrink-0" />
        <span className="truncate">{t('titlebar.search')}</span>
        <kbd className="ml-auto shrink-0 font-mono text-[11px] font-medium text-ink-secondary">{t('titlebar.shortcut')}</kbd>
      </button>}

      <div className="flex h-full flex-1 justify-end">
        <button type="button" aria-label={t('titlebar.minimize')} title={t('titlebar.minimize')} onClick={wailsRuntime.minimise} className={cn(windowButton, 'hover:bg-surface-hover hover:text-ink')}>
          <IconMinus size={14} />
        </button>
        <button
          type="button"
          aria-label={maximised ? t('titlebar.restore') : t('titlebar.maximize')}
          title={maximised ? t('titlebar.restore') : t('titlebar.maximize')}
          onClick={wailsRuntime.toggleMaximise}
          className={cn(windowButton, 'hover:bg-surface-hover hover:text-ink')}
        >
          {maximised ? <IconRestore size={13} /> : <IconSquare size={13} />}
        </button>
        <button type="button" aria-label={t('titlebar.close')} title={t('titlebar.close')} onClick={wailsRuntime.quit} className={cn(windowButton, 'hover:bg-danger hover:text-on-accent')}>
          <IconClose size={14} />
        </button>
      </div>
    </header>
  )
}
