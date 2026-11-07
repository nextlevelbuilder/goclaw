import { Fragment } from 'react'
import { useTranslation } from 'react-i18next'
import { cn } from '../../../lib/cn'
import { useUiStore, type AppView } from '../../../stores/ui-store'
import { Badge, Tooltip } from '../../ui'
import { NAV_GROUPS, VIEW_REGISTRY, navItemFor, viewLabelKey } from '../view-registry'

export interface SidebarNavProps {
  /** `full` shows labels under group headings; `rail` shows icons with tooltips. */
  mode: 'full' | 'rail'
  /** Optional count per view (e.g. pending pairings on Channels). */
  badges?: Partial<Record<AppView, number>>
  /** Called after an item is chosen (closes the overlay). */
  onNavigate?: () => void
}

/** Grouped navigation. The active item gets accent-subtle, a 2px accent marker and aria-current="page". */
export function SidebarNav({ mode, badges, onNavigate }: SidebarNavProps) {
  const { t } = useTranslation('desktop')
  const activeView = useUiStore((s) => s.activeView)
  const setActiveView = useUiStore((s) => s.setActiveView)
  const current = navItemFor(activeView)
  const rail = mode === 'rail'

  const go = (view: AppView) => {
    setActiveView(view)
    onNavigate?.()
  }

  return (
    <nav aria-label={t('nav.label')} className={cn('flex flex-col', rail ? 'items-center gap-1' : 'gap-0.5')}>
      {NAV_GROUPS.map((group, gi) => (
        <Fragment key={group.id}>
          {rail ? (
            gi > 0 && <span aria-hidden="true" className="my-1.5 h-px w-6 bg-border-subtle" />
          ) : (
            <h2 className="px-2 pb-1 pt-2.5 text-overline text-ink-muted">{t(`nav.groups.${group.id}`)}</h2>
          )}
          <ul className={cn('flex flex-col', rail ? 'items-center gap-1' : 'gap-0.5')}>
            {group.views.map((view) => {
              const Icon = VIEW_REGISTRY[view].icon
              const label = t(viewLabelKey(view))
              const active = current === view
              const count = badges?.[view] ?? 0
              const item = (
                <button
                  type="button"
                  onClick={() => go(view)}
                  aria-current={active ? 'page' : undefined}
                  aria-label={rail ? label : undefined}
                  className={cn(
                    'relative flex items-center rounded-sm text-ui transition-colors',
                    rail ? 'size-9 justify-center' : 'h-8 w-full gap-2.5 px-2 text-left',
                    active
                      ? 'bg-accent-subtle text-accent-subtle-ink'
                      : 'text-ink-secondary hover:bg-surface-hover hover:text-ink',
                  )}
                >
                  {active && (
                    <span
                      aria-hidden="true"
                      data-testid="nav-active-marker"
                      className={cn('absolute w-0.5 rounded-pill bg-accent', rail ? '-left-2.5 top-2 h-5' : '-left-3 top-[7px] h-[18px]')}
                    />
                  )}
                  <Icon size={16} className="shrink-0" />
                  {!rail && <span className="min-w-0 flex-1 truncate">{label}</span>}
                  {count > 0 && (
                    rail
                      ? <span aria-hidden="true" className="absolute right-1 top-1 size-2 rounded-pill bg-warning" />
                      : <Badge tone="warning" aria-label={t('nav.pendingPairings', { count })}>{count}</Badge>
                  )}
                </button>
              )
              return (
                <li key={view} className={rail ? undefined : 'w-full'}>
                  {rail ? <Tooltip content={label} side="right">{item}</Tooltip> : item}
                </li>
              )
            })}
          </ul>
        </Fragment>
      ))}
    </nav>
  )
}
