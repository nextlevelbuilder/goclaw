import { useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { useAgents } from '../../../hooks/use-agents'
import { wails } from '../../../lib/wails'
import { useUiStore } from '../../../stores/ui-store'
import { EditionCompareModal } from '../../common/EditionCompareModal'
import { IconBot, IconCheck, IconChevronsUpDown, IconInfo, IconPanelLeft } from '../../common/Icons'
import { IconButton, Menu, type MenuEntry } from '../../ui'
import { chatWithAgent } from '../shell-actions'

export interface SidebarHeaderProps {
  /** Collapse handler for the top-right button (to the rail, or close the overlay). */
  onCollapse: () => void
  collapseLabel: string
}

/**
 * Workspace row: app mark and the current chat agent. Opens a menu to switch
 * agent, manage agents or compare editions.
 */
export function SidebarHeader({ onCollapse, collapseLabel }: SidebarHeaderProps) {
  const { t } = useTranslation('desktop')
  const { agents, selectedAgent } = useAgents()
  const setActiveView = useUiStore((s) => s.setActiveView)
  const [editionOpen, setEditionOpen] = useState(false)
  const [version, setVersion] = useState('')

  useEffect(() => {
    wails.getVersion().then(setVersion).catch(() => { /* not running inside Wails */ })
  }, [])

  const items: MenuEntry[] = [
    ...agents.map((a) => ({
      label: a.name,
      icon: selectedAgent?.id === a.id
        ? <IconCheck size={14} />
        : <span aria-hidden="true" className="text-caption font-semibold">{a.emoji || a.name.charAt(0).toUpperCase()}</span>,
      onSelect: () => chatWithAgent(a.id),
    })),
    ...(agents.length > 0 ? ['separator' as const] : []),
    { label: t('nav.manageAgents'), icon: <IconBot size={16} />, onSelect: () => setActiveView('agents') },
    { label: `${t('nav.compareEditions')} · Lite${version ? ` v${version}` : ''}`, icon: <IconInfo size={16} />, onSelect: () => setEditionOpen(true) },
  ]

  return (
    <div className="flex items-center gap-1">
      <Menu
        align="start"
        items={items}
        trigger={(
          <button
            type="button"
            aria-label={t('nav.switchAgent')}
            className="flex h-control min-w-0 flex-1 items-center gap-2 rounded-sm px-1.5 text-left transition-colors hover:bg-surface-hover"
          >
            <span aria-hidden="true" className="flex size-5 shrink-0 items-center justify-center rounded-sm bg-accent text-[11px] font-bold text-on-accent">
              G
            </span>
            <span className="min-w-0 flex-1 truncate text-ui font-semibold text-ink">
              GoClaw
              <span className="font-normal text-ink-muted"> · {selectedAgent?.name ?? t('nav.noAgent')}</span>
            </span>
            <IconChevronsUpDown size={14} className="shrink-0 text-ink-muted" />
          </button>
        )}
      />
      <IconButton aria-label={collapseLabel} icon={<IconPanelLeft size={16} />} size="sm" onClick={onCollapse} tooltipSide="right" />
      <EditionCompareModal open={editionOpen} onClose={() => setEditionOpen(false)} />
    </div>
  )
}
