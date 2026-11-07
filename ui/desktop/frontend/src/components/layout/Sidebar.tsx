import { useTranslation } from 'react-i18next'
import type { AppView } from '../../stores/ui-store'
import { IconPanelLeft, IconPlus } from '../common/Icons'
import { Button, IconButton } from '../ui'
import { startNewChat } from './shell-actions'
import { SessionList } from './sidebar/SessionList'
import { SidebarFooter } from './sidebar/SidebarFooter'
import { SidebarHeader } from './sidebar/SidebarHeader'
import { SidebarNav } from './sidebar/SidebarNav'

export interface SidebarProps {
  /** `full`: labels, recent sessions. `rail`: icons only with tooltips. */
  mode: 'full' | 'rail'
  /** full mode: collapse to the rail (wide window) or close the overlay (narrow window). */
  onCollapse?: () => void
  collapseLabel?: string
  /** rail mode: expand to the full sidebar (inline, or as an overlay when narrow). */
  onExpand?: () => void
  /** Called after navigation (the overlay closes itself). */
  onNavigate?: () => void
  /** Deletes a conversation (from the shell's useSessions() instance). */
  onDeleteSession: (key: string) => Promise<void> | void
  /** Counts shown on nav items (e.g. pending pairings on Channels). */
  badges?: Partial<Record<AppView, number>>
}

/** Sidebar contents: workspace, New chat, grouped navigation, recent sessions and the gateway footer. */
export function Sidebar({ mode, onCollapse, collapseLabel, onExpand, onNavigate, onDeleteSession, badges }: SidebarProps) {
  const { t } = useTranslation('desktop')

  const newChat = () => {
    startNewChat()
    onNavigate?.()
  }

  if (mode === 'rail') {
    return (
      <div className="flex h-full flex-col items-center gap-1 py-3">
        <IconButton aria-label={t('nav.expand')} icon={<IconPanelLeft size={16} />} tooltipSide="right" onClick={onExpand} />
        <IconButton
          aria-label={t('nav.newChat')}
          icon={<IconPlus size={16} />}
          variant="primary"
          tooltipSide="right"
          onClick={newChat}
          className="mb-1 mt-1 size-9"
        />
        <div className="min-h-0 flex-1 overflow-y-auto overscroll-contain px-2.5">
          <SidebarNav mode="rail" badges={badges} onNavigate={onNavigate} />
        </div>
        <SidebarFooter mode="rail" />
      </div>
    )
  }

  return (
    <div className="flex h-full flex-col gap-0.5 p-3">
      <SidebarHeader onCollapse={onCollapse ?? (() => {})} collapseLabel={collapseLabel ?? t('nav.collapse')} />
      <Button variant="primary" icon={<IconPlus size={16} />} onClick={newChat} className="mb-1 mt-1.5 w-full" aria-keyshortcuts="Control+N">
        {t('nav.newChat')}
      </Button>
      <div className="flex min-h-0 flex-1 flex-col overflow-y-auto overscroll-contain">
        <SidebarNav mode="full" badges={badges} onNavigate={onNavigate} />
        <h2 className="px-2 pb-1 pt-2.5 text-overline text-ink-muted">{t('nav.recent')}</h2>
        <SessionList onDelete={onDeleteSession} onNavigate={onNavigate} />
      </div>
      <SidebarFooter mode="full" />
    </div>
  )
}
