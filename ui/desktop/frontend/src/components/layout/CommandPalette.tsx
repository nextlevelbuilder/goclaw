import { useEffect, useId, useMemo, useRef, useState, type KeyboardEvent } from 'react'
import { useTranslation } from 'react-i18next'
import { Dialog as DialogPrimitive, VisuallyHidden } from 'radix-ui'
import { cn } from '../../lib/cn'
import { useAgentStore } from '../../stores/agent-store'
import { useSessionStore } from '../../stores/session-store'
import { useUiStore } from '../../stores/ui-store'
import { IconBot, IconChat, IconGear, IconMoon, IconPlus, IconSearch } from '../common/Icons'
import { buildPaletteItems, type PaletteGroup, type PaletteItem } from './command-palette-items'
import { chatWithAgent, openSession, startNewChat } from './shell-actions'
import { VIEW_REGISTRY } from './view-registry'

function ItemIcon({ item }: { item: PaletteItem }) {
  const target = item.target
  switch (target.kind) {
    case 'view': {
      const Icon = VIEW_REGISTRY[target.view].icon
      return <Icon size={16} />
    }
    case 'agent':
      return <IconBot size={16} />
    case 'session':
      return <IconChat size={16} />
    case 'action':
      if (target.action === 'new-chat') return <IconPlus size={16} />
      if (target.action === 'toggle-theme') return <IconMoon size={16} />
      return <IconGear size={16} />
  }
}

/** Ctrl+K palette: search views, agents, sessions and actions. ↑↓ move, Enter opens, Esc closes. */
export function CommandPalette() {
  const { t } = useTranslation('desktop')
  const open = useUiStore((s) => s.commandPaletteOpen)
  const setOpen = useUiStore((s) => s.setCommandPaletteOpen)
  const agents = useAgentStore((s) => s.agents)
  const sessions = useSessionStore((s) => s.sessions)
  const [query, setQuery] = useState('')
  const [activeIndex, setActiveIndex] = useState(0)
  const listRef = useRef<HTMLDivElement>(null)
  const listId = useId()

  const items = useMemo(
    () => buildPaletteItems({ query, agents, sessions, t: (k) => t(k) }),
    [query, agents, sessions, t],
  )

  useEffect(() => {
    if (!open) {
      setQuery('')
      setActiveIndex(0)
    }
  }, [open])

  useEffect(() => { setActiveIndex(0) }, [query])

  useEffect(() => {
    const el = listRef.current?.querySelector<HTMLElement>('[aria-selected="true"]')
    el?.scrollIntoView?.({ block: 'nearest' })
  }, [activeIndex])

  const run = (item: PaletteItem) => {
    setOpen(false)
    const target = item.target
    const ui = useUiStore.getState()
    switch (target.kind) {
      case 'view':
        ui.setActiveView(target.view)
        break
      case 'agent':
        chatWithAgent(target.agentId)
        break
      case 'session':
        openSession(target.sessionKey)
        break
      case 'action':
        if (target.action === 'new-chat') startNewChat()
        else if (target.action === 'toggle-theme') ui.toggleTheme()
        else ui.openSettings('appearance')
        break
    }
  }

  const onKeyDown = (e: KeyboardEvent<HTMLInputElement>) => {
    if (items.length === 0) return
    switch (e.key) {
      case 'ArrowDown':
        e.preventDefault()
        setActiveIndex((i) => (i + 1) % items.length)
        break
      case 'ArrowUp':
        e.preventDefault()
        setActiveIndex((i) => (i - 1 + items.length) % items.length)
        break
      case 'Home':
        e.preventDefault()
        setActiveIndex(0)
        break
      case 'End':
        e.preventDefault()
        setActiveIndex(items.length - 1)
        break
      case 'Enter': {
        e.preventDefault()
        const item = items[activeIndex]
        if (item) run(item)
        break
      }
    }
  }

  const optionId = (i: number) => `${listId}-opt-${i}`
  let lastGroup: PaletteGroup | null = null

  return (
    <DialogPrimitive.Root open={open} onOpenChange={setOpen}>
      <DialogPrimitive.Portal>
        <DialogPrimitive.Overlay className="fixed inset-0 z-[90] bg-surface-window/70" />
        <DialogPrimitive.Content
          aria-describedby={undefined}
          className="fixed left-1/2 top-[15vh] z-[100] flex max-h-[70vh] w-[640px] max-w-[calc(100vw-2rem)] -translate-x-1/2 flex-col overflow-hidden rounded-xl border border-border-subtle bg-surface-raised text-ink shadow-popover animate-pop-in"
        >
          <VisuallyHidden.Root>
            <DialogPrimitive.Title>{t('palette.label')}</DialogPrimitive.Title>
          </VisuallyHidden.Root>
          <div className="flex h-[52px] shrink-0 items-center gap-2.5 border-b border-border-subtle px-4">
            <IconSearch size={16} className="shrink-0 text-ink-muted" />
            <input
              autoFocus
              value={query}
              onChange={(e) => setQuery(e.target.value)}
              onKeyDown={onKeyDown}
              role="combobox"
              aria-expanded="true"
              aria-controls={listId}
              aria-autocomplete="list"
              aria-activedescendant={items[activeIndex] ? optionId(activeIndex) : undefined}
              aria-label={t('palette.label')}
              placeholder={t('palette.placeholder')}
              className="h-8 min-w-0 flex-1 rounded-sm bg-transparent px-1 text-body text-ink placeholder:text-ink-muted focus-visible:outline-offset-0"
            />
            <kbd className="shrink-0 font-mono text-[11px] font-medium text-ink-muted">Esc</kbd>
          </div>

          <div ref={listRef} id={listId} role="listbox" aria-label={t('palette.label')} className="min-h-0 flex-1 overflow-y-auto overscroll-contain p-2">
            {items.length === 0 && (
              <p className="px-2 py-6 text-center text-ui font-normal text-ink-muted">{t('palette.noResults', { query })}</p>
            )}
            {items.map((item, i) => {
              const header = item.group !== lastGroup ? item.group : null
              lastGroup = item.group
              const selected = i === activeIndex
              return (
                <div key={item.id} role="presentation">
                  {header && (
                    <div role="presentation" className="px-2 pb-1 pt-2 text-overline text-ink-muted">
                      {t(`palette.groups.${header}`)}
                    </div>
                  )}
                  <div
                    id={optionId(i)}
                    role="option"
                    aria-selected={selected}
                    onMouseMove={() => { if (!selected) setActiveIndex(i) }}
                    onClick={() => run(item)}
                    className={cn(
                      'flex h-9 cursor-pointer items-center gap-2.5 rounded-sm px-2 text-ui',
                      selected ? 'bg-accent-subtle text-accent-subtle-ink' : 'text-ink',
                    )}
                  >
                    <span className={cn('shrink-0', selected ? 'text-current' : 'text-ink-muted')}><ItemIcon item={item} /></span>
                    <span className="min-w-0 flex-1 truncate">{item.label}</span>
                    {item.hint && <span className="shrink-0 font-mono text-[11px] text-ink-muted">{item.hint}</span>}
                  </div>
                </div>
              )
            })}
          </div>

          <div className="flex shrink-0 gap-4 border-t border-border-subtle px-4 py-2.5 text-caption text-ink-muted">
            <span>{t('palette.hintMove')}</span>
            <span>{t('palette.hintOpen')}</span>
            <span>{t('palette.hintClose')}</span>
          </div>
        </DialogPrimitive.Content>
      </DialogPrimitive.Portal>
    </DialogPrimitive.Root>
  )
}
